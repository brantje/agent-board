package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type recordingIssuePatchStore struct {
	*fakeStore
	patch  store.IssuePatch
	actor  json.RawMessage
	events []store.Event
	err    error
}

func (s *recordingIssuePatchStore) UpdateIssuePatchMutation(_ context.Context, patch store.IssuePatch, actor json.RawMessage) (store.IssueMutationResult, error) {
	s.patch = patch
	s.actor = append(json.RawMessage(nil), actor...)
	if s.err != nil {
		return store.IssueMutationResult{}, s.err
	}
	issue := coverageIssue()
	if patch.Title != nil {
		issue.Title = *patch.Title
	}
	if patch.Description != nil {
		issue.Description = *patch.Description
	}
	if patch.Status != nil {
		issue.Status = *patch.Status
	}
	if patch.Priority != nil {
		issue.Priority = *patch.Priority
	}
	return store.IssueMutationResult{Issue: issue, Events: append([]store.Event(nil), s.events...)}, nil
}

func TestIssuePatchApplicationBoundaryPreservesFieldPresence(t *testing.T) {
	project := store.Project{ID: coverageProjectID()}
	base := &fakeStore{project: project}
	patchStore := &recordingIssuePatchStore{fakeStore: base}
	service := New(patchStore)
	title := "renamed"

	issue, err := service.PatchIssue(t.Context(), store.IssuePatch{ProjectID: project.ID, ID: "issue", Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if issue.Title != title || patchStore.patch.Title == nil || *patchStore.patch.Title != title {
		t.Fatalf("issue=%+v patch=%+v", issue, patchStore.patch)
	}
	if patchStore.patch.Status != nil || patchStore.patch.Description != nil || patchStore.patch.Priority != nil {
		t.Fatalf("field presence was not preserved: %+v", patchStore.patch)
	}

	actor := json.RawMessage(`{"type":"HUMAN","id":"user-1"}`)
	status := "REVIEW"
	if _, err := service.PatchIssueWithActor(t.Context(), store.IssuePatch{ProjectID: project.ID, ID: "issue", Status: &status}, actor); err != nil {
		t.Fatal(err)
	}
	if string(patchStore.actor) != string(actor) {
		t.Fatalf("actor=%s want=%s", patchStore.actor, actor)
	}
}

func TestIssuePatchApplicationBoundaryValidatesAndTranslates(t *testing.T) {
	project := store.Project{ID: coverageProjectID()}
	service := New(&recordingIssuePatchStore{fakeStore: &fakeStore{project: project}, err: store.ErrConflict})
	empty := " "
	badStatus := "NOPE"
	badPriority := 5
	for _, patch := range []store.IssuePatch{
		{ProjectID: project.ID, ID: "issue", Title: &empty},
		{ProjectID: project.ID, ID: "issue", Status: &badStatus},
		{ProjectID: project.ID, ID: "issue", Priority: &badPriority},
	} {
		if _, err := service.PatchIssue(t.Context(), patch); err == nil {
			t.Fatalf("PatchIssue(%+v) unexpectedly succeeded", patch)
		} else if appErr, ok := AsError(err); !ok || appErr.Code != "invalid_argument" {
			t.Fatalf("PatchIssue(%+v) error=%v", patch, err)
		}
	}

	title := "valid"
	if _, err := service.PatchIssue(t.Context(), store.IssuePatch{ProjectID: project.ID, ID: "issue", Title: &title}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("translated error=%v", err)
	}

	withoutCapability := New(&fakeStore{project: project})
	if _, err := withoutCapability.PatchIssue(t.Context(), store.IssuePatch{ProjectID: project.ID, ID: "issue", Title: &title}); err == nil {
		t.Fatal("expected missing partial-mutation capability error")
	} else if appErr, ok := AsError(err); !ok || appErr.Code != "issue_mutation_unavailable" {
		t.Fatalf("missing capability error=%v", err)
	}
}

func (s *projectAccessServiceStore) UpdateIssuePatchMutation(_ context.Context, patch store.IssuePatch, _ json.RawMessage) (store.IssueMutationResult, error) {
	issue := store.Issue{ID: patch.ID, ProjectID: patch.ProjectID, Title: "existing", Status: "TODO"}
	if patch.Title != nil {
		issue.Title = *patch.Title
	}
	if patch.Status != nil {
		issue.Status = *patch.Status
	}
	return store.IssueMutationResult{Issue: issue}, nil
}

func TestProjectAccessPatchIssueUsesAuthorizedPartialMutation(t *testing.T) {
	project := store.Project{ID: "project-1", Name: "Project"}
	fake := &projectAccessServiceStore{
		projects: []store.Project{project},
		roles:    map[string]string{project.ID + ":user-1": store.ProjectRoleMember},
	}
	service := newProjectAccessServiceForTest(t, fake)
	title := "updated"
	issue, err := service.PatchIssue(t.Context(), activeProjectActor("user-1", store.DeploymentRoleMember), store.IssuePatch{ProjectID: project.ID, ID: "issue-1", Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if issue.Title != title || issue.Status != "TODO" {
		t.Fatalf("issue=%+v", issue)
	}
}
