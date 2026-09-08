package opencode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

func TestServerEnvironmentRegistersCustomProviderModel(t *testing.T) {
	baseURL := "https://openrouter.ai/api/v1/"
	env, err := serverEnvironment(executioncontext.SafeContext{
		Model: executioncontext.ModelContext{Model: "minimax/minimax-m3"},
		Provider: executioncontext.ProviderContext{
			Name:    "OpenRouter",
			Kind:    "openai-compatible",
			BaseURL: &baseURL,
		},
	}, "openai-compatible")
	if err != nil {
		t.Fatal(err)
	}

	config := env["OPENCODE_CONFIG_CONTENT"]
	var decoded map[string]any
	if err := json.Unmarshal([]byte(config), &decoded); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	provider := decoded["provider"].(map[string]any)["openai-compatible"].(map[string]any)
	if provider["npm"] != "@ai-sdk/openai-compatible" {
		t.Fatalf("npm=%v", provider["npm"])
	}
	if provider["name"] != "OpenRouter" {
		t.Fatalf("name=%v", provider["name"])
	}
	options := provider["options"].(map[string]any)
	if options["baseURL"] != baseURL || options["apiKey"] != "{env:"+providerCredentialEnv+"}" {
		t.Fatalf("options=%v", options)
	}
	models := provider["models"].(map[string]any)
	model, ok := models["minimax/minimax-m3"].(map[string]any)
	if !ok {
		t.Fatalf("models=%v", models)
	}
	if model["name"] != "minimax/minimax-m3" {
		t.Fatalf("model name=%v", model["name"])
	}
}

func TestServerEnvironmentBuiltInProviderOmitsCustomPackage(t *testing.T) {
	baseURL := "https://api.anthropic.com/v1"
	env, err := serverEnvironment(executioncontext.SafeContext{
		Model: executioncontext.ModelContext{Model: "claude-sonnet-4-20250514"},
		Provider: executioncontext.ProviderContext{
			Name:    "Anthropic",
			Kind:    "anthropic",
			BaseURL: &baseURL,
		},
	}, "anthropic")
	if err != nil {
		t.Fatal(err)
	}

	config := env["OPENCODE_CONFIG_CONTENT"]
	if strings.Contains(config, "@ai-sdk/openai-compatible") {
		t.Fatalf("built-in provider should not declare npm package: %q", config)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(config), &decoded); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	provider := decoded["provider"].(map[string]any)["anthropic"].(map[string]any)
	models := provider["models"].(map[string]any)
	if _, ok := models["claude-sonnet-4-20250514"]; !ok {
		t.Fatalf("models=%v", models)
	}
}
