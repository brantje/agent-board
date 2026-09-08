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

func (s *Service) ListProviderModels(ctx context.Context, providerID string, resolver executioncontext.SecretResolver, client *http.Client) ([]ProviderModel, error) {
	provider, err := s.GetProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	var apiKey []byte
	if provider.CredentialRef != nil && strings.TrimSpace(*provider.CredentialRef) != "" {
		if !secretResolverConfigured(resolver) {
			return nil, NewError("provider_credential_unavailable", "Provider credential is unavailable for model discovery.", nil)
		}
		apiKey, err = resolver.Resolve(ctx, secrets.Scope{}, strings.TrimSpace(*provider.CredentialRef))
		if err != nil {
			return nil, NewError("provider_credential_unavailable", "Provider credential is unavailable for model discovery.", err)
		}
	}
	discovered, err := providerdiscovery.Discover(ctx, client, provider, apiKey)
	if err != nil {
		if strings.Contains(err.Error(), "baseUrl") || strings.Contains(err.Error(), "default models endpoint") {
			return nil, NewError("provider_model_discovery_unconfigured", "Configure a baseUrl on this Provider before discovering models.", err)
		}
		return nil, NewError("provider_model_discovery_failed", "Unable to discover models from the Provider API.", err)
	}
	out := make([]ProviderModel, 0, len(discovered))
	for _, model := range discovered {
		out = append(out, ProviderModel{ID: model.ID, Name: model.Name})
	}
	return out, nil
}

func secretResolverConfigured(resolver executioncontext.SecretResolver) bool {
	if resolver == nil {
		return false
	}
	value := reflect.ValueOf(resolver)
	return value.Kind() != reflect.Ptr || !value.IsNil()
}
