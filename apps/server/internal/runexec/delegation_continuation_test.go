package runexec

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestLoadDelegationContinuationUsesExactResumeJobAndCurrentWorkspaceRevision(t *testing.T) {
	outcome := store.DelegationOutcomeSucceeded
	summary := "bounded delegated result"
	eventID := "event-1"
	accepted := true
	completedAt := time.Now().UTC()
	evidenceStore := &processTestStore{
		delegationContinuation: store.Delegation{
			ID:                       "delegation-1",
			ProjectID:                "project-1",
			IssueID:                  "issue-1",
			ParentRunID:              "parent-run-1",
			ParentAgentID:            "parent-agent-1",
			TargetAgentID:            "target-agent-1",
			Task:                     "Inspect the bounded component.",
			DelegatedRunID:           "child-run-1",
			Outcome:                  &outcome,
			ResultSummary:            &summary,
			ResultEventID:            &eventID,
			WorkspaceChangesAccepted: &accepted,
			CompletedAt:              &completedAt,
		},
		workspaceRevision: "revision-after-delegate",
	}
	processor := &Processor{store: evidenceStore}
	claim := &store.SchedulerAdmission{Job: store.SchedulerJob{ID: "resume-job-1", Kind: "RESUME"}}
	run := store.Run{ID: "parent-run-1", ProjectID: "project-1", IssueID: "issue-1", WorkspaceID: "workspace-1"}

	got, err := processor.loadDelegationContinuation(t.Context(), claim, run)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("delegation continuation is nil")
	}
	if got.DelegationID != "delegation-1" || got.TargetAgentID != "target-agent-1" || got.Task != "Inspect the bounded component." || got.Outcome != outcome || got.ResultSummary != summary || got.DelegatedRunID != "child-run-1" || got.ResultEventID != eventID || !got.WorkspaceChangesAccepted || got.WorkspaceRevision != "revision-after-delegate" {
		t.Fatalf("delegation continuation=%+v", got)
	}
}

func TestLoadDelegationContinuationIgnoresNonDelegationResume(t *testing.T) {
	processor := &Processor{store: &processTestStore{}}
	run := store.Run{ID: "parent-run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"}

	got, err := processor.loadDelegationContinuation(t.Context(), &store.SchedulerAdmission{Job: store.SchedulerJob{ID: "question-resume", Kind: "RESUME"}}, run)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("continuation=%+v want nil", got)
	}
}

func TestLoadDelegationContinuationFailsClosedOnIncompleteOrUnverifiableState(t *testing.T) {
	outcome := store.DelegationOutcomeSucceeded
	summary := "result"
	accepted := true
	completedAt := time.Now().UTC()
	base := store.Delegation{
		ID:                       "delegation-1",
		ProjectID:                "project-1",
		ParentRunID:              "parent-run-1",
		TargetAgentID:            "target-agent-1",
		Task:                     "task",
		DelegatedRunID:           "child-run-1",
		Outcome:                  &outcome,
		ResultSummary:            &summary,
		WorkspaceChangesAccepted: &accepted,
		CompletedAt:              &completedAt,
	}
	claim := &store.SchedulerAdmission{Job: store.SchedulerJob{ID: "resume-job-1", Kind: "RESUME"}}
	run := store.Run{ID: "parent-run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"}

	tests := []struct {
		name  string
		store *processTestStore
	}{
		{name: "missing workspace revision", store: &processTestStore{delegationContinuation: base}},
		{name: "workspace revision lookup failure", store: &processTestStore{delegationContinuation: base, workspaceRevisionErr: errors.New("revision unavailable")}},
		{name: "incomplete delegation", store: &processTestStore{delegationContinuation: func() store.Delegation { v := base; v.ResultSummary = nil; return v }(), workspaceRevision: "revision"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := &Processor{store: tt.store}
			if got, err := processor.loadDelegationContinuation(t.Context(), claim, run); err == nil || got != nil {
				t.Fatalf("continuation=%+v err=%v want fail-closed error", got, err)
			}
		})
	}
}

type continuationWithoutRevisionStore struct {
	ExecutionStore
	delegation store.Delegation
}

func (s *continuationWithoutRevisionStore) GetDelegationByContinuationJob(context.Context, string, string, string) (store.Delegation, error) {
	return s.delegation, nil
}

func TestLoadDelegationContinuationFailsClosedOnLookupAndCapabilityErrors(t *testing.T) {
	outcome := store.DelegationOutcomeSucceeded
	summary := "result"
	accepted := true
	completedAt := time.Now().UTC()
	base := store.Delegation{
		ID: "delegation-1", ProjectID: "project-1", ParentRunID: "parent-run-1", TargetAgentID: "target-agent-1",
		Task: "task", DelegatedRunID: "child-run-1", Outcome: &outcome, ResultSummary: &summary,
		WorkspaceChangesAccepted: &accepted, CompletedAt: &completedAt,
	}
	claim := &store.SchedulerAdmission{Job: store.SchedulerJob{ID: "resume-job-1", Kind: "RESUME"}}
	run := store.Run{ID: "parent-run-1", ProjectID: "project-1", WorkspaceID: "workspace-1"}

	t.Run("delegation lookup failure", func(t *testing.T) {
		processor := &Processor{store: &processTestStore{delegationErr: errors.New("lookup failed")}}
		if got, err := processor.loadDelegationContinuation(t.Context(), claim, run); err == nil || got != nil {
			t.Fatalf("continuation=%+v err=%v want lookup error", got, err)
		}
	})

	t.Run("invalid persisted outcome", func(t *testing.T) {
		invalid := "UNKNOWN"
		delegation := base
		delegation.Outcome = &invalid
		processor := &Processor{store: &processTestStore{delegationContinuation: delegation, workspaceRevision: "revision"}}
		if got, err := processor.loadDelegationContinuation(t.Context(), claim, run); err == nil || got != nil {
			t.Fatalf("continuation=%+v err=%v want invalid outcome error", got, err)
		}
	})

	t.Run("Workspace revision capability missing", func(t *testing.T) {
		processor := &Processor{store: &continuationWithoutRevisionStore{delegation: base}}
		if got, err := processor.loadDelegationContinuation(t.Context(), claim, run); err == nil || got != nil {
			t.Fatalf("continuation=%+v err=%v want missing revision capability error", got, err)
		}
	})
}

