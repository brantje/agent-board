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

func TestServerEnvironmentPinsSmallModelToConfiguredProviderModel(t *testing.T) {
	env, err := serverEnvironment(executioncontext.SafeContext{
		Model: executioncontext.ModelContext{Model: "nvidia/nemotron-3-ultra-550b-a55b:free"},
		Provider: executioncontext.ProviderContext{
			Name: "OpenRouter",
			Kind: "openrouter",
		},
	}, "openrouter")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &decoded); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	want := "openrouter/nvidia/nemotron-3-ultra-550b-a55b:free"
	if decoded["model"] != want || decoded["small_model"] != want {
		t.Fatalf("model=%v small_model=%v want %s", decoded["model"], decoded["small_model"], want)
	}
	agent, _ := decoded["agent"].(map[string]any)
	title, _ := agent["title"].(map[string]any)
	if title["disable"] != true {
		t.Fatalf("title agent disable=%v want true so title generation cannot steal the configured model slot", title["disable"])
	}
}

func TestServerEnvironmentUsesProcessLocalDatabase(t *testing.T) {
	env, err := serverEnvironment(executioncontext.SafeContext{
		Model:    executioncontext.ModelContext{Model: "deepseek/deepseek-v4-flash"},
		Provider: executioncontext.ProviderContext{Kind: "openrouter"},
	}, "openrouter")
	if err != nil {
		t.Fatal(err)
	}
	if got := env["OPENCODE_DB"]; got != ":memory:" {
		t.Fatalf("OPENCODE_DB=%q want :memory: so concurrent OpenCode processes do not share SQLite state", got)
	}
}
