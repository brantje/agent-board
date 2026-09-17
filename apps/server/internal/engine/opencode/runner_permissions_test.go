package opencode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

func TestServerEnvironmentAllowsHeadlessToolsExceptTrustedDelegationHandshake(t *testing.T) {
	env, err := serverEnvironment(executioncontext.SafeContext{
		Model:    executioncontext.ModelContext{Model: "test-model"},
		Provider: executioncontext.ProviderContext{Kind: "openrouter"},
	}, "openrouter")
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Permission map[string]string `json:"permission"`
	}
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &decoded); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if decoded.Permission["*"] != "allow" {
		t.Fatalf("default permission=%q want allow", decoded.Permission["*"])
	}
	if decoded.Permission[delegationPermissionName] != "ask" {
		t.Fatalf("delegation permission=%q want ask", decoded.Permission[delegationPermissionName])
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
