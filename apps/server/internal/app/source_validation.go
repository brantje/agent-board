package app

import (
	"net/url"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func normalizeSourceConnection(input store.SourceConnection) (store.SourceConnection, error) {
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return store.SourceConnection{}, invalid("source connection name is required")
	}
	switch input.Kind {
	case store.SourceProviderGitHub, store.SourceProviderGitLab, store.SourceProviderForgejo:
	default:
		return store.SourceConnection{}, invalid("source connection kind must be github, gitlab or forgejo")
	}
	if input.ExternalAccountID != nil {
		externalID := strings.TrimSpace(*input.ExternalAccountID)
		if externalID == "" {
			return store.SourceConnection{}, invalid("externalAccountId must not be blank")
		}
		input.ExternalAccountID = &externalID
	}
	baseURL, err := normalizeSourceBaseURL(input.Kind, input.BaseURL)
	if err != nil {
		return store.SourceConnection{}, err
	}
	input.BaseURL = baseURL
	if input.HealthStatus == "" {
		input.HealthStatus = "UNKNOWN"
	}
	switch input.HealthStatus {
	case "UNKNOWN", "HEALTHY", "UNHEALTHY":
	default:
		return store.SourceConnection{}, invalid("invalid source connection health status")
	}
	return input, nil
}

func normalizeSourceBaseURL(kind string, value *string) (*string, error) {
	raw := ""
	if value != nil {
		raw = strings.TrimSpace(*value)
	}
	if raw == "" {
		switch kind {
		case store.SourceProviderGitHub:
			raw = "https://" + "github.com"
		case store.SourceProviderGitLab:
			raw = "https://" + "gitlab.com"
		case store.SourceProviderForgejo:
			return nil, invalid("baseUrl is required for forgejo source connections")
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, invalid("baseUrl must be an absolute instance URL without user info, query or fragment")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, invalid("baseUrl must use http or https")
	}
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	normalized := parsed.String()
	return &normalized, nil
}

func validateSourceRepository(input store.SourceRepository) error {
	if strings.TrimSpace(input.SourceConnectionID) == "" ||
		strings.TrimSpace(input.ExternalID) == "" ||
		strings.TrimSpace(input.Name) == "" ||
		strings.TrimSpace(input.Path) == "" ||
		strings.TrimSpace(input.WebURL) == "" {
		return invalid("source repository requires sourceConnectionId, externalId, name, path and webUrl")
	}
	return nil
}
