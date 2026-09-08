package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	openRouterEnvKey       = "OPENROUTER_API_KEY"
	openRouterModelsEnvKey = "OPENROUTER_MODELS"
	openRouterProviderName = "OpenRouter"
	openRouterProviderKind = "openrouter"
)

func EnsureOpenRouterFromEnv(ctx context.Context, control *Service, secretStore SecretStore, getenv func(string) string) error {
	if control == nil {
		return fmt.Errorf("control plane service is required")
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	apiKey := strings.TrimSpace(getenv(openRouterEnvKey))
	models := parseCommaSeparatedEnv(getenv(openRouterModelsEnvKey))
	if apiKey == "" && len(models) == 0 {
		return nil
	}

	providers, err := control.ListProviders(ctx)
	if err != nil {
		return fmt.Errorf("list providers for env bootstrap: %w", err)
	}

	provider, err := ensureOpenRouterProviderFromEnv(ctx, control, secretStore, apiKey, providers)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return nil
	}
	if provider.ID == "" {
		return fmt.Errorf("%s is set but OpenRouter provider does not exist", openRouterModelsEnvKey)
	}
	return ensureOpenRouterModelProfilesFromEnv(ctx, control, provider, models)
}

func ensureOpenRouterProviderFromEnv(ctx context.Context, control *Service, secretStore SecretStore, apiKey string, providers []store.Provider) (store.Provider, error) {
	if provider, found, err := findOpenRouterProvider(providers); err != nil {
		return store.Provider{}, err
	} else if found {
		return reconcileOpenRouterProviderCredential(ctx, control, secretStore, apiKey, provider)
	}
	if apiKey == "" {
		return store.Provider{}, nil
	}
	if secretStore == nil {
		return store.Provider{}, fmt.Errorf("%s is set but secret storage is not configured", openRouterEnvKey)
	}

	provider, err := control.CreateProvider(ctx, store.Provider{
		Name:         openRouterProviderName,
		Kind:         openRouterProviderKind,
		Enabled:      true,
		SafeMetadata: store.EmptyObject,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			reloaded, listErr := control.ListProviders(ctx)
			if listErr != nil {
				return store.Provider{}, fmt.Errorf("list providers after OpenRouter create conflict: %w", listErr)
			}
			provider, found, findErr := findOpenRouterProvider(reloaded)
			if findErr != nil {
				return store.Provider{}, findErr
			}
			if !found {
				return store.Provider{}, fmt.Errorf("create OpenRouter provider: %w", err)
			}
			return reconcileOpenRouterProviderCredential(ctx, control, secretStore, apiKey, provider)
		}
		return store.Provider{}, fmt.Errorf("create OpenRouter provider: %w", err)
	}

	provider, err = completeOpenRouterProviderCredential(ctx, control, secretStore, apiKey, provider)
	if err != nil {
		return store.Provider{}, err
	}
	slog.Info("created OpenRouter provider from env bootstrap")
	return provider, nil
}

func reconcileOpenRouterProviderCredential(ctx context.Context, control *Service, secretStore SecretStore, apiKey string, provider store.Provider) (store.Provider, error) {
	if apiKey == "" {
		return provider, nil
	}
	configured, err := openRouterProviderCredentialConfigured(ctx, secretStore, provider)
	if err != nil {
		return store.Provider{}, err
	}
	if configured {
		slog.Info("OpenRouter provider already exists; skipping env bootstrap")
		return provider, nil
	}
	if secretStore == nil {
		return store.Provider{}, fmt.Errorf("%s is set but secret storage is not configured", openRouterEnvKey)
	}
	provider, err = completeOpenRouterProviderCredential(ctx, control, secretStore, apiKey, provider)
	if err != nil {
		return store.Provider{}, err
	}
	slog.Info("completed OpenRouter provider credential from env bootstrap")
	return provider, nil
}

func completeOpenRouterProviderCredential(ctx context.Context, control *Service, secretStore SecretStore, apiKey string, provider store.Provider) (store.Provider, error) {
	ref := "provider:" + provider.ID
	if _, err := secretStore.Put(ctx, secrets.Scope{}, ref, []byte(apiKey)); err != nil {
		return store.Provider{}, fmt.Errorf("store OpenRouter credential: %w", err)
	}
	refValue := ref
	provider.CredentialRef = &refValue
	provider, err := control.UpdateProvider(ctx, provider)
	if err != nil {
		return store.Provider{}, fmt.Errorf("update OpenRouter credential ref: %w", err)
	}
	return provider, nil
}

func openRouterProviderCredentialConfigured(ctx context.Context, secretStore SecretStore, provider store.Provider) (bool, error) {
	if provider.CredentialRef == nil || strings.TrimSpace(*provider.CredentialRef) == "" {
		return false, nil
	}
	if secretStore == nil {
		return false, nil
	}
	_, err := secretStore.Resolve(ctx, secrets.Scope{}, strings.TrimSpace(*provider.CredentialRef))
	if errors.Is(err, secrets.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("resolve OpenRouter credential: %w", err)
	}
	return true, nil
}

func ensureOpenRouterModelProfilesFromEnv(ctx context.Context, control *Service, provider store.Provider, modelIDs []string) error {
	existing, err := control.ListModelProfiles(ctx, nil)
	if err != nil {
		return fmt.Errorf("list model profiles for env bootstrap: %w", err)
	}

	for _, modelID := range modelIDs {
		name := deriveOpenRouterModelProfileName(modelID)
		if name == "" {
			continue
		}
		if globalOpenRouterModelProfileExists(existing, provider.ID, name, modelID) {
			continue
		}

		created, err := control.CreateModelProfile(ctx, store.ModelProfile{
			ProviderID:         provider.ID,
			Name:               name,
			Model:              modelID,
			GenerationSettings: store.EmptyObject,
			Enabled:            true,
		})
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				continue
			}
			return fmt.Errorf("create OpenRouter model profile %q: %w", modelID, err)
		}
		existing = append(existing, created)
		slog.Info("created OpenRouter model profile from env bootstrap", "name", name, "model", modelID)
	}
	return nil
}

func parseCommaSeparatedEnv(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func deriveOpenRouterModelProfileName(modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ""
	}
	if idx := strings.LastIndex(modelID, "/"); idx >= 0 && idx < len(modelID)-1 {
		return modelID[idx+1:]
	}
	return modelID
}

func findOpenRouterProvider(providers []store.Provider) (store.Provider, bool, error) {
	var wrongKind store.Provider
	var wrongKindFound bool
	for _, provider := range providers {
		if !strings.EqualFold(provider.Name, openRouterProviderName) {
			continue
		}
		if strings.EqualFold(provider.Kind, openRouterProviderKind) {
			return provider, true, nil
		}
		wrongKind = provider
		wrongKindFound = true
	}
	if wrongKindFound {
		return store.Provider{}, false, fmt.Errorf(
			"provider named %q exists with kind %q; env bootstrap requires kind %q",
			openRouterProviderName,
			wrongKind.Kind,
			openRouterProviderKind,
		)
	}
	return store.Provider{}, false, nil
}

func globalOpenRouterModelProfileExists(existing []store.ModelProfile, providerID, name, modelID string) bool {
	for _, profile := range existing {
		if profile.ProjectID != nil {
			continue
		}
		if strings.EqualFold(profile.Name, name) {
			return true
		}
		if profile.ProviderID == providerID && profile.Model == modelID {
			return true
		}
	}
	return false
}
