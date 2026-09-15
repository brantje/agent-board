package opencode

import (
    "strings"
    "testing"

    "github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

func TestInitialTaskPromptIncludesPersistedStatusAndFinalComparison(t *testing.T) {
    prompt := initialTaskPrompt(executioncontext.SafeContext{Issue: executioncontext.IssueContext{Title: "Task", Status: "BLOCKED"}})
    for _, required := range []string{
        "Current persisted Issue Board status: BLOCKED.",
        "Before your final response, compare the final work outcome with the persisted Issue Board status",
        "set_issue_status(status)",
        "native Question capability",
        "project-relative paths",
    } {
        if !strings.Contains(prompt, required) { t.Fatalf("prompt missing %q: %s", required, prompt) }
    }
    for _, forbidden := range []string{"Run starts move", "Run success moves", "Run failure moves"} {
        if strings.Contains(prompt, forbidden) { t.Fatalf("prompt incorrectly couples Board status to Run lifecycle: %s", prompt) }
    }
}
