package providerdiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const requestTimeout = 10 * time.Second

var builtInOpenCodeProviders = map[string]struct{}{
	"amazon-bedrock": {},
	"anthropic":      {},
	"azure":          {},
	"cerebras":       {},
	"deepseek":       {},
	"fireworks":      {},
	"github-copilot": {},
	"gitlab":         {},
	"google":         {},
	"google-vertex":  {},
	"groq":           {},
	"mistral":        {},
	"openai":         {},
	"openrouter":     {},
	"opencode":       {},
	"perplexity":     {},
	"together":       {},
	"xai":            {},
}

var defaultBaseURLs = map[string]string{
	"cerebras":   "https://api.cerebras.ai/v1",
	"deepseek":   "https://api.deepseek.com/v1",
	"fireworks":  "https://api.fireworks.ai/inference/v1",
	"groq":       "https://api.groq.com/openai/v1",
	"mistral":    "https://api.mistral.ai/v1",
	"openai":     "https://api.openai.com/v1",
	"openrouter": "https://openrouter.ai/api/v1",
	"opencode":   "https://opencode.ai/zen/v1",
	"perplexity": "https://api.perplexity.ai",
	"together":   "https://api.together.xyz/v1",
	"xai":        "https://api.x.ai/v1",
}

type Model struct {
	ID   string
	Name *string
}

func ResolveBaseURL(provider store.Provider) (string, error) {
	if provider.BaseURL != nil {
		base := strings.TrimSpace(*provider.BaseURL)
		if base != "" {
			return normalizeBaseURL(base), nil
		}
	}
	kind := strings.TrimSpace(provider.Kind)
	if defaultURL, ok := defaultBaseURLs[kind]; ok {
		return defaultURL, nil
	}
	if isBuiltInOpenCodeProvider(kind) {
		return "", fmt.Errorf("provider model discovery: built-in provider %q has no default models endpoint; configure baseUrl", kind)
	}
	return "", fmt.Errorf("provider model discovery: baseUrl is required for custom providers")
}

func isBuiltInOpenCodeProvider(kind string) bool {
	_, ok := builtInOpenCodeProviders[kind]
	return ok
}

func normalizeBaseURL(base string) string {
	return strings.TrimRight(strings.TrimSpace(base), "/")
}

func modelsEndpoint(baseURL string) string {
	return normalizeBaseURL(baseURL) + "/models"
}

func ListModels(ctx context.Context, client *http.Client, baseURL string, apiKey []byte) ([]Model, error) {
	if client == nil {
		return nil, fmt.Errorf("provider model discovery: HTTP client is required")
	}
	endpoint := modelsEndpoint(baseURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("provider model discovery: build request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if len(apiKey) > 0 {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(apiKey)))
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("provider model discovery: request failed: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("provider model discovery: read response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("provider model discovery: upstream returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID   string  `json:"id"`
			Name *string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("provider model discovery: decode response: %w", err)
	}
	seen := make(map[string]struct{}, len(payload.Data))
	models := make([]Model, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		model := Model{ID: id}
		if item.Name != nil {
			name := strings.TrimSpace(*item.Name)
			if name != "" {
				model.Name = &name
			}
		}
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func Discover(ctx context.Context, client *http.Client, provider store.Provider, apiKey []byte) ([]Model, error) {
	baseURL, err := ResolveBaseURL(provider)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	return ListModels(ctx, client, baseURL, apiKey)
}
