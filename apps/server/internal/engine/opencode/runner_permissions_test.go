package opencode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

func TestServerEnvironmentAllowsAllHeadlessToolCalls(t *testing.T) {
	env, err := serverEnvironment(executioncontext.SafeContext{
		Model:    executioncontext.ModelContext{Model: "test-model"},
		Provider: executioncontext.ProviderContext{Kind: "openrouter"},
	}, "openrouter")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &decoded); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if decoded["permission"] != "allow" {
		t.Fatalf("permission=%v want allow", decoded["permission"])
	}
}

func TestInitialTaskPromptKeepsWorkspacePathsPortable(t *testing.T) {
	prompt := initialTaskPrompt(executioncontext.SafeContext{
		Issue: executioncontext.IssueContext{
			Title:       "Write result",
			Description: "Create /workspace/result.txt.",
		},
	})
	if !strings.Contains(prompt, "Treat /workspace as the logical workspace root") || !strings.Contains(prompt, "project-relative paths") {
		t.Fatalf("prompt does not explain portable runner workspace paths: %q", prompt)
	}
}
