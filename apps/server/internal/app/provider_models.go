package app

import (
	"context"
	"net/http"
	"reflect"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/providerdiscovery"
	"github.com/brantje/agent-board/apps/server/internal/secrets"
)

type ProviderModel struct {
	ID   string  `json:"id"`
	Name *string `json:"name"`
}

type ProviderModelListResult struct {
	Models []ProviderModel
	Total  int
}

func (s *Service) ListProviderModels(ctx context.Context, scope *string, providerID string, resolver executioncontext.SecretResolver, client *http.Client) (ProviderModelListResult, error) {
	provider, err := s.GetProvider(ctx, scope, providerID)
	if err != nil {
		return ProviderModelListResult{}, err
	}
	var apiKey []byte
	if provider.CredentialRef != nil && strings.TrimSpace(*provider.CredentialRef) != "" {
		if !secretResolverConfigured(resolver) {
			s.persistProviderHealth(ctx, providerID, false, nil, nil)
			return ProviderModelListResult{}, NewError("provider_credential_unavailable", "Provider credential is unavailable for model discovery.", nil)
		}
		apiKey, err = resolver.Resolve(ctx, secrets.Scope{ProjectID: provider.ProjectID}, strings.TrimSpace(*provider.CredentialRef))
		if err != nil {
			s.persistProviderHealth(ctx, providerID, false, nil, nil)
			return ProviderModelListResult{}, NewError("provider_credential_unavailable", "Provider credential is unavailable for model discovery.", err)
		}
	}
	discovered, upstreamTotal, err := providerdiscovery.Discover(ctx, client, provider, apiKey)
	if err != nil {
		s.persistProviderHealth(ctx, providerID, false, nil, nil)
		if strings.Contains(err.Error(), "baseUrl") || strings.Contains(err.Error(), "default models endpoint") {
			return ProviderModelListResult{}, NewError("provider_model_discovery_unconfigured", "Configure a baseUrl on this Provider before discovering models.", err)
		}
		return ProviderModelListResult{}, NewError("provider_model_discovery_failed", "Unable to discover models from the Provider API.", err)
	}
	filtered := len(discovered)
	s.persistProviderHealth(ctx, providerID, true, &filtered, &upstreamTotal)
	out := make([]ProviderModel, 0, len(discovered))
	for _, model := range discovered {
		out = append(out, ProviderModel{ID: model.ID, Name: model.Name})
	}
	return ProviderModelListResult{Models: out, Total: upstreamTotal}, nil
}

func secretResolverConfigured(resolver executioncontext.SecretResolver) bool {
	if resolver == nil {
		return false
	}
	value := reflect.ValueOf(resolver)
	return value.Kind() != reflect.Ptr || !value.IsNil()
}
