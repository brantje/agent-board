package evidence

import (
	"context"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type failureReasonCaptureStore struct {
	store.ControlPlaneStore
	transition store.SchedulerTransition
}

func (s *failureReasonCaptureStore) TransitionAdmittedJob(_ context.Context, input store.SchedulerTransition) (store.Run, error) {
	s.transition = input
	return store.Run{}, nil
}

func TestRedactingStoreSanitizesRunFailureReason(t *testing.T) {
	const runID = "run-redaction"
	const secret = "provider-secret"

	registry := redaction.NewRegistry()
	registry.Register(runID, []string{secret})
	base := &failureReasonCaptureStore{}
	secured := NewRedactingStore(base, registry)
	reason := "provider failed with " + secret

	if _, err := secured.TransitionAdmittedJob(t.Context(), store.SchedulerTransition{
		ProjectID:     "project",
		JobID:         "job",
		RunID:         runID,
		LeaseToken:    "lease",
		RunStatus:     "FAILED",
		FailureReason: &reason,
	}); err != nil {
		t.Fatal(err)
	}
	if base.transition.FailureReason == nil {
		t.Fatal("failure reason was dropped")
	}
	if strings.Contains(*base.transition.FailureReason, secret) {
		t.Fatalf("failure reason leaked secret: %q", *base.transition.FailureReason)
	}
}
