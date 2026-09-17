package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type delegationHandoffServiceStore struct {
	store.ControlPlaneStore

	projectID      string
	parentRunID    string
	delegationID   string
	delegatedRunID string
	err            error
}

func (s *delegationHandoffServiceStore) MarkDelegationWorkspaceHandoffReady(_ context.Context, projectID, parentRunID, delegationID, delegatedRunID string) error {
	s.projectID = projectID
	s.parentRunID = parentRunID
	s.delegationID = delegationID
	s.delegatedRunID = delegatedRunID
	return s.err
}

func TestMarkDelegationWorkspaceHandoffReadyForwardsAndTranslatesErrors(t *testing.T) {
	backend := &delegationHandoffServiceStore{}
	service := New(backend)

	if err := service.MarkDelegationWorkspaceHandoffReady(t.Context(), "project", "parent-run", "delegation", "child-run"); err != nil {
		t.Fatal(err)
	}
	if backend.projectID != "project" || backend.parentRunID != "parent-run" || backend.delegationID != "delegation" || backend.delegatedRunID != "child-run" {
		t.Fatalf("handoff forwarding project=%q parent=%q delegation=%q child=%q", backend.projectID, backend.parentRunID, backend.delegationID, backend.delegatedRunID)
	}

	backend.err = store.ErrConflict
	if err := service.MarkDelegationWorkspaceHandoffReady(t.Context(), "project", "parent-run", "delegation", "child-run"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("error=%v want translated conflict", err)
	}
}

func TestMarkDelegationWorkspaceHandoffReadyRejectsUnavailableStores(t *testing.T) {
	var nilService *Service
	if err := nilService.MarkDelegationWorkspaceHandoffReady(t.Context(), "project", "parent-run", "delegation", "child-run"); err == nil {
		t.Fatal("nil service accepted delegation workspace handoff")
	}

	if err := New(&unsupportedDelegationStore{}).MarkDelegationWorkspaceHandoffReady(t.Context(), "project", "parent-run", "delegation", "child-run"); err == nil {
		t.Fatal("store without handoff support accepted delegation workspace handoff")
	}
}
