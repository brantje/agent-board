package opencode

import (
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

func TestInitialTaskPromptUsesTrustedDelegationTargetAndSquadContext(t *testing.T) {
	role := "implementation"
	request := engine.Request{
		Context: executioncontext.SafeContext{
			Issue: executioncontext.IssueContext{Title: "Delegate work", Status: "TODO"},
			Agent: executioncontext.AgentContext{AllowDelegation: true},
		},
		Delegation: &recordingDelegationRequester{},
		DelegationContext: &engine.DelegationToolContext{
			Targets: []engine.DelegationTargetContext{
				{ID: "agent-2", Name: "Implementer"},
				{ID: "agent-3", Name: "Verifier"},
			},
			Squad: &engine.SquadDelegationContext{
				ID: "squad-1", Name: "Backend", LeaderAgentID: "agent-1", LeaderAgentName: "Leader",
				Members: []engine.SquadDelegationMemberContext{{ID: "agent-2", Name: "Implementer", Role: &role}},
			},
		},
	}
	prompt := initialTaskPromptForRequest(request)
	for _, want := range []string{
		"Choose targetAgentId only from the server-provided available delegation targets",
		"Available delegation targets:\n- agent-2 — Implementer\n- agent-3 — Verifier",
		"Current Issue Squad: Backend (squad-1)",
		"Authoritative Squad leader: Leader (agent-1)",
		"agent-2 — Implementer — role: implementation",
		"Squad membership is collaboration context only",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestInitialTaskPromptDoesNotExposeDelegationGuidanceWithoutEffectiveCapability(t *testing.T) {
	prompt := initialTaskPromptForRequest(engine.Request{
		Context: executioncontext.SafeContext{
			Issue: executioncontext.IssueContext{Title: "No delegation", Status: "TODO"},
			Agent: executioncontext.AgentContext{AllowDelegation: true},
		},
	})
	if strings.Contains(prompt, "delegate_task(targetAgentId, task)") || strings.Contains(prompt, "Available delegation targets") {
		t.Fatalf("prompt exposed delegation without effective capability:\n%s", prompt)
	}
}

func TestDelegationToolReturnsStructuredCanonicalAcceptance(t *testing.T) {
	for _, want := range []string{
		`JSON.stringify({ status: "accepted", targetAgentId })`,
		`metadata: {`,
		`targetAgentId,`,
		`task,`,
	} {
		if !strings.Contains(delegationToolSource, want) {
			t.Fatalf("delegation tool source missing %q", want)
		}
	}
	if strings.Contains(delegationToolSource, "parentRunId") || strings.Contains(delegationToolSource, "issueId") {
		t.Fatal("delegation tool exposes server-owned parent identity")
	}
}

func TestInitialTaskPromptDescribesCommentOriginDelegationWithoutInventingParentRun(t *testing.T) {
	sourceCommentID := "comment-1"
	prompt := initialTaskPromptForRequest(engine.Request{
		Context: executioncontext.SafeContext{
			Issue: executioncontext.IssueContext{Title: "Mentioned work", Status: "TODO"},
			Delegation: &executioncontext.DelegationContext{
				ID: "delegation-1", SourceCommentID: &sourceCommentID, TargetAgentID: "agent-2",
				Task: "Inspect this bounded request.", RequestKey: "mention-1",
			},
		},
		IssueComments: &recordingIssueCommentPublisher{},
	})
	for _, want := range []string{
		"structured Agent mention in the Issue discussion",
		"There is no parent Run to resume",
		"Do not attempt to change Issue status or delegate further work",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "The parent Run remains authoritative") {
		t.Fatalf("comment-origin delegated prompt invented a parent Run:\n%s", prompt)
	}
}

func TestIssueCommentPromptRequiresStructuredMentionIDs(t *testing.T) {
	for _, want := range []string{
		"publish_issue_comment(body, mentionAgentIds?)",
		"Plain @name text has no routing semantics",
		"pass at most one stable Agent ID structurally in mentionAgentIds",
		"never guess Agent identifiers",
	} {
		if !strings.Contains(issueCommentPromptGuidance, want) {
			t.Fatalf("Issue comment guidance missing %q: %s", want, issueCommentPromptGuidance)
		}
	}
}
