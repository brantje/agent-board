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

	liteLLMEndpointEnvKey = "LITELLM_ENDPOINT"
	liteLLMAPIKeyEnvKey   = "LITELLM_API_KEY"
	liteLLMModelsEnvKey   = "LITELLM_MODELS"
	liteLLMProviderName   = "LiteLLM"
	liteLLMProviderKind   = "openai-compatible"
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

	providers, err := control.ListProviders(ctx, nil)
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
	return ensureModelProfilesFromEnv(ctx, control, provider, models, "OpenRouter")
}

func EnsureLiteLLMFromEnv(ctx context.Context, control *Service, secretStore SecretStore, getenv func(string) string) error {
	if control == nil {
		return fmt.Errorf("control plane service is required")
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	endpoint := strings.TrimSpace(getenv(liteLLMEndpointEnvKey))
	apiKey := strings.TrimSpace(getenv(liteLLMAPIKeyEnvKey))
	models := parseCommaSeparatedEnv(getenv(liteLLMModelsEnvKey))
	if endpoint == "" && apiKey == "" && len(models) == 0 {
		return nil
	}

	providers, err := control.ListProviders(ctx, nil)
	if err != nil {
		return fmt.Errorf("list providers for env bootstrap: %w", err)
	}
	provider, err := ensureLiteLLMProviderFromEnv(ctx, control, secretStore, endpoint, apiKey, providers)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return nil
	}
	if provider.ID == "" {
		return fmt.Errorf("%s is set but LiteLLM provider does not exist", liteLLMModelsEnvKey)
	}
	return ensureModelProfilesFromEnv(ctx, control, provider, models, "LiteLLM")
}

func ensureLiteLLMProviderFromEnv(ctx context.Context, control *Service, secretStore SecretStore, endpoint, apiKey string, providers []store.Provider) (store.Provider, error) {
	if provider, found, err := findNamedProvider(providers, liteLLMProviderName, liteLLMProviderKind); err != nil {
		return store.Provider{}, err
	} else if found {
		return reconcileLiteLLMProviderFromEnv(ctx, control, secretStore, endpoint, apiKey, provider)
	}
	if endpoint == "" && apiKey == "" {
		return store.Provider{}, nil
	}
	if endpoint == "" || apiKey == "" {
		return store.Provider{}, fmt.Errorf("%s and %s must both be set to create the LiteLLM provider", liteLLMEndpointEnvKey, liteLLMAPIKeyEnvKey)
	}
	if secretStore == nil {
		return store.Provider{}, fmt.Errorf("%s is set but secret storage is not configured", liteLLMAPIKeyEnvKey)
	}

	baseURL := endpoint
	provider, err := control.CreateProvider(ctx, store.Provider{
		Name:         liteLLMProviderName,
		Kind:         liteLLMProviderKind,
		BaseURL:      &baseURL,
		Enabled:      true,
		SafeMetadata: store.EmptyObject,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			reloaded, listErr := control.ListProviders(ctx, nil)
			if listErr != nil {
				return store.Provider{}, fmt.Errorf("list providers after LiteLLM create conflict: %w", listErr)
			}
			provider, found, findErr := findNamedProvider(reloaded, liteLLMProviderName, liteLLMProviderKind)
			if findErr != nil {
				return store.Provider{}, findErr
			}
			if !found {
				return store.Provider{}, fmt.Errorf("create LiteLLM provider: %w", err)
			}
			return reconcileLiteLLMProviderFromEnv(ctx, control, secretStore, endpoint, apiKey, provider)
		}
		return store.Provider{}, fmt.Errorf("create LiteLLM provider: %w", err)
	}

	provider, err = completeProviderCredential(ctx, control, secretStore, apiKey, provider, "LiteLLM")
	if err != nil {
		return store.Provider{}, err
	}
	slog.Info("created LiteLLM provider from env bootstrap")
	return provider, nil
}

func reconcileLiteLLMProviderFromEnv(ctx context.Context, control *Service, secretStore SecretStore, endpoint, apiKey string, provider store.Provider) (store.Provider, error) {
	next := provider
	baseURLMissing := endpoint != "" && !providerBaseURLConfigured(next)
	if baseURLMissing {
		baseURL := endpoint
		next.BaseURL = &baseURL
	}

	if apiKey != "" {
		configured, err := providerCredentialConfigured(ctx, secretStore, provider, "LiteLLM")
		if err != nil {
			return store.Provider{}, err
		}
		if !configured {
			if secretStore == nil {
				return store.Provider{}, fmt.Errorf("%s is set but secret storage is not configured", liteLLMAPIKeyEnvKey)
			}
			provider, err = completeProviderCredential(ctx, control, secretStore, apiKey, next, "LiteLLM")
			if err != nil {
				return store.Provider{}, err
			}
			slog.Info("completed LiteLLM provider credential from env bootstrap")
			return provider, nil
		}
		slog.Info("LiteLLM provider already exists; skipping credential env bootstrap")
	}

	if !baseURLMissing {
		return provider, nil
	}
	provider, err := control.UpdateProvider(ctx, nil, next)
	if err != nil {
		return store.Provider{}, fmt.Errorf("update LiteLLM base URL: %w", err)
	}
	slog.Info("completed LiteLLM provider base URL from env bootstrap")
	return provider, nil
}

func ensureOpenRouterProviderFromEnv(ctx context.Context, control *Service, secretStore SecretStore, apiKey string, providers []store.Provider) (store.Provider, error) {
	if provider, found, err := findNamedProvider(providers, openRouterProviderName, openRouterProviderKind); err != nil {
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
			reloaded, listErr := control.ListProviders(ctx, nil)
			if listErr != nil {
				return store.Provider{}, fmt.Errorf("list providers after OpenRouter create conflict: %w", listErr)
			}
			provider, found, findErr := findNamedProvider(reloaded, openRouterProviderName, openRouterProviderKind)
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

	provider, err = completeProviderCredential(ctx, control, secretStore, apiKey, provider, "OpenRouter")
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
	configured, err := providerCredentialConfigured(ctx, secretStore, provider, "OpenRouter")
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
	provider, err = completeProviderCredential(ctx, control, secretStore, apiKey, provider, "OpenRouter")
	if err != nil {
		return store.Provider{}, err
	}
	slog.Info("completed OpenRouter provider credential from env bootstrap")
	return provider, nil
}

func completeProviderCredential(ctx context.Context, control *Service, secretStore SecretStore, apiKey string, provider store.Provider, label string) (store.Provider, error) {
	ref := "provider:" + provider.ID
	if _, err := secretStore.Put(ctx, secrets.Scope{}, ref, []byte(apiKey)); err != nil {
		return store.Provider{}, fmt.Errorf("store %s credential: %w", label, err)
	}
	refValue := ref
	provider.CredentialRef = &refValue
	provider, err := control.UpdateProvider(ctx, nil, provider)
	if err != nil {
		return store.Provider{}, fmt.Errorf("update %s credential ref: %w", label, err)
	}
	return provider, nil
}

func providerCredentialConfigured(ctx context.Context, secretStore SecretStore, provider store.Provider, label string) (bool, error) {
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
		return false, fmt.Errorf("resolve %s credential: %w", label, err)
	}
	return true, nil
}

func providerBaseURLConfigured(provider store.Provider) bool {
	return provider.BaseURL != nil && strings.TrimSpace(*provider.BaseURL) != ""
}

func ensureModelProfilesFromEnv(ctx context.Context, control *Service, provider store.Provider, modelIDs []string, label string) error {
	existing, err := control.ListModelProfiles(ctx, nil)
	if err != nil {
		return fmt.Errorf("list model profiles for env bootstrap: %w", err)
	}

	for _, modelID := range modelIDs {
		name := deriveEnvModelProfileName(modelID)
		if name == "" {
			continue
		}
		if globalEnvModelProfileExists(existing, provider.ID, name, modelID) {
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
			return fmt.Errorf("create %s model profile %q: %w", label, modelID, err)
		}
		existing = append(existing, created)
		slog.Info("created "+label+" model profile from env bootstrap", "name", name, "model", modelID)
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
	return deriveEnvModelProfileName(modelID)
}

func deriveEnvModelProfileName(modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ""
	}
	if idx := strings.LastIndex(modelID, "/"); idx >= 0 && idx < len(modelID)-1 {
		return modelID[idx+1:]
	}
	return modelID
}

func findNamedProvider(providers []store.Provider, name, kind string) (store.Provider, bool, error) {
	var wrongKind store.Provider
	var wrongKindFound bool
	for _, provider := range providers {
		if !strings.EqualFold(provider.Name, name) {
			continue
		}
		if provider.Kind == kind {
			return provider, true, nil
		}
		wrongKind = provider
		wrongKindFound = true
	}
	if wrongKindFound {
		return store.Provider{}, false, fmt.Errorf(
			"provider named %q exists with kind %q; env bootstrap requires kind %q",
			name,
			wrongKind.Kind,
			kind,
		)
	}
	return store.Provider{}, false, nil
}

func globalEnvModelProfileExists(existing []store.ModelProfile, providerID, name, modelID string) bool {
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
