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
	openRouterEnvKey         = "OPENROUTER_API_KEY"
	openRouterModelsEnvKey   = "OPENROUTER_MODELS"
	openRouterProviderName   = "OpenRouter"
	openRouterProviderKind   = "openrouter"
)

func EnsureOpenRouterFromEnv(ctx context.Context, control *Service, secretWriter SecretWriter, getenv func(string) string) error {
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

	provider, err := ensureOpenRouterProviderFromEnv(ctx, control, secretWriter, apiKey, providers)
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

func ensureOpenRouterProviderFromEnv(ctx context.Context, control *Service, secretWriter SecretWriter, apiKey string, providers []store.Provider) (store.Provider, error) {
	if provider, found := findOpenRouterProvider(providers); found {
		if apiKey != "" {
			slog.Info("OpenRouter provider already exists; skipping env bootstrap")
		}
		return provider, nil
	}
	if apiKey == "" {
		return store.Provider{}, nil
	}
	if secretWriter == nil {
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
			slog.Info("OpenRouter provider already exists; skipping env bootstrap")
			return store.Provider{}, nil
		}
		return store.Provider{}, fmt.Errorf("create OpenRouter provider: %w", err)
	}

	ref := "provider:" + provider.ID
	if _, err := secretWriter.Put(ctx, secrets.Scope{}, ref, []byte(apiKey)); err != nil {
		return store.Provider{}, fmt.Errorf("store OpenRouter credential: %w", err)
	}
	refValue := ref
	provider.CredentialRef = &refValue
	provider, err = control.UpdateProvider(ctx, provider)
	if err != nil {
		return store.Provider{}, fmt.Errorf("update OpenRouter credential ref: %w", err)
	}

	slog.Info("created OpenRouter provider from env bootstrap")
	return provider, nil
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

func findOpenRouterProvider(providers []store.Provider) (store.Provider, bool) {
	for _, provider := range providers {
		if strings.EqualFold(provider.Name, openRouterProviderName) {
			return provider, true
		}
	}
	return store.Provider{}, false
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
