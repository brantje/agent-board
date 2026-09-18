package evidence

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationHandoffCapabilityStore struct {
	store.ControlPlaneStore

	projectID      string
	parentRunID    string
	delegationID   string
	delegatedRunID string
	err            error
}

func (s *delegationHandoffCapabilityStore) MarkDelegationWorkspaceHandoffReady(_ context.Context, projectID, parentRunID, delegationID, delegatedRunID string) error {
	s.projectID = projectID
	s.parentRunID = parentRunID
	s.delegationID = delegationID
	s.delegatedRunID = delegatedRunID
	return s.err
}

type unsupportedDelegationHandoffCapabilityStore struct {
	store.ControlPlaneStore
}

func TestRedactingStorePreservesDelegationWorkspaceHandoffCapability(t *testing.T) {
	base := &delegationHandoffCapabilityStore{}
	wrapped := NewRedactingStore(base, redaction.NewRegistry())

	if err := wrapped.MarkDelegationWorkspaceHandoffReady(t.Context(), "project", "parent-run", "delegation", "child-run"); err != nil {
		t.Fatal(err)
	}
	if base.projectID != "project" || base.parentRunID != "parent-run" || base.delegationID != "delegation" || base.delegatedRunID != "child-run" {
		t.Fatalf("handoff forwarding project=%q parent=%q delegation=%q child=%q", base.projectID, base.parentRunID, base.delegationID, base.delegatedRunID)
	}

	base.err = store.ErrConflict
	if err := wrapped.MarkDelegationWorkspaceHandoffReady(t.Context(), "project", "parent-run", "delegation", "child-run"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("error=%v want conflict", err)
	}
}

func TestRedactingStoreReportsMissingDelegationWorkspaceHandoffCapability(t *testing.T) {
	wrapped := NewRedactingStore(&unsupportedDelegationHandoffCapabilityStore{}, redaction.NewRegistry())
	if err := wrapped.MarkDelegationWorkspaceHandoffReady(t.Context(), "project", "parent-run", "delegation", "child-run"); err == nil {
		t.Fatal("expected missing delegation workspace handoff capability error")
	}
}
