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
