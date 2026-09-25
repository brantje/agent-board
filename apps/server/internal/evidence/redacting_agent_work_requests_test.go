package evidence

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type agentWorkRequestCapabilityStore struct {
	store.ControlPlaneStore
	contextProjectID string
	contextRunID string
	contextResult *store.AgentWorkRequestExecutionContext
	contextErr error
	reconcileCalls int
	reconcileResult store.AgentWorkRequestReconciliationResult
	reconcileErr error
}

func (s *agentWorkRequestCapabilityStore) GetAgentWorkRequestExecutionContext(_ context.Context, projectID, runID string) (*store.AgentWorkRequestExecutionContext, error) {
	s.contextProjectID = projectID
	s.contextRunID = runID
	return s.contextResult, s.contextErr
}

func (s *agentWorkRequestCapabilityStore) ReconcilePendingAgentWorkRequest(context.Context) (store.AgentWorkRequestReconciliationResult, error) {
	s.reconcileCalls++
	return s.reconcileResult, s.reconcileErr
}

type unsupportedAgentWorkRequestCapabilityStore struct{ store.ControlPlaneStore }

func TestRedactingStorePreservesAgentWorkRequestCapabilities(t *testing.T) {
	reason := "mention"
	base := &agentWorkRequestCapabilityStore{
		contextResult: &store.AgentWorkRequestExecutionContext{
			WorkRequestID: "work-1",
			Comments: []store.AgentWorkRequestComment{{
				CommentID: "comment-1", AuthorType: "HUMAN", AuthorID: "user-1",
				AuthorName: "User", Body: "Please handle this",
				TriggerKind: store.AgentWorkRequestTriggerMention, RoutingReason: &reason,
			}},
		},
		reconcileResult: store.AgentWorkRequestReconciliationResult{
			Handled: true, Events: []store.Event{{ID: "event-1"}},
		},
	}
	wrapped := NewRedactingStore(base, redaction.NewRegistry())

	var contextStore store.AgentWorkRequestExecutionContextStore = wrapped
	gotContext, err := contextStore.GetAgentWorkRequestExecutionContext(t.Context(), "project-1", "run-1")
	if err != nil { t.Fatal(err) }
	if base.contextProjectID != "project-1" || base.contextRunID != "run-1" {
		t.Fatalf("execution-context forwarding project=%q run=%q", base.contextProjectID, base.contextRunID)
	}
	if gotContext != base.contextResult { t.Fatal("execution context was not forwarded unchanged") }
	if len(gotContext.Comments) != 1 || gotContext.Comments[0].CommentID != "comment-1" || gotContext.Comments[0].Body != "Please handle this" {
		t.Fatalf("execution context provenance=%+v", gotContext.Comments)
	}

	var reconciliationStore store.AgentWorkRequestReconciliationStore = wrapped
	gotReconciliation, err := reconciliationStore.ReconcilePendingAgentWorkRequest(t.Context())
	if err != nil { t.Fatal(err) }
	if base.reconcileCalls != 1 { t.Fatalf("reconciliation calls=%d want 1", base.reconcileCalls) }
	if !gotReconciliation.Handled || len(gotReconciliation.Events) != 1 || gotReconciliation.Events[0].ID != "event-1" {
		t.Fatalf("reconciliation result=%+v", gotReconciliation)
	}
}

func TestRedactingStoreAgentWorkRequestCapabilityErrorsAreForwarded(t *testing.T) {
	contextErr := errors.New("context failed")
	reconcileErr := errors.New("reconcile failed")
	base := &agentWorkRequestCapabilityStore{contextErr: contextErr, reconcileErr: reconcileErr}
	wrapped := NewRedactingStore(base, redaction.NewRegistry())

	if _, err := wrapped.GetAgentWorkRequestExecutionContext(t.Context(), "project-1", "run-1"); !errors.Is(err, contextErr) {
		t.Fatalf("execution-context error=%v want %v", err, contextErr)
	}
	if _, err := wrapped.ReconcilePendingAgentWorkRequest(t.Context()); !errors.Is(err, reconcileErr) {
		t.Fatalf("reconciliation error=%v want %v", err, reconcileErr)
	}
}

func TestRedactingStoreReportsMissingAgentWorkRequestCapabilities(t *testing.T) {
	wrapped := NewRedactingStore(&unsupportedAgentWorkRequestCapabilityStore{}, redaction.NewRegistry())
	if _, err := wrapped.GetAgentWorkRequestExecutionContext(t.Context(), "project-1", "run-1"); err == nil {
		t.Fatal("expected missing Agent work request execution-context capability error")
	}
	if _, err := wrapped.ReconcilePendingAgentWorkRequest(t.Context()); err == nil {
		t.Fatal("expected missing Agent work request reconciliation capability error")
	}
}
