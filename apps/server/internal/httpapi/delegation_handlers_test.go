package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

const delegatedRunID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

func delegationFixture() store.Delegation {
	outcome := store.DelegationOutcomeSucceeded
	summary := "bounded result"
	eventID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	accepted := true
	continuationJobID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	completedAt := time.Unix(2, 0).UTC()
	return store.Delegation{
		ID:                       "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		ProjectID:                projectID,
		IssueID:                  issueID,
		ParentRunID:              runID,
		ParentAgentID:            agentID,
		TargetAgentID:            otherID,
		Task:                     "inspect scheduler ownership",
		DelegatedRunID:           delegatedRunID,
		RequestKey:               "request-1",
		Outcome:                  &outcome,
		ResultSummary:            &summary,
		ResultEventID:            &eventID,
		WorkspaceChangesAccepted: &accepted,
		ContinuationJobID:        &continuationJobID,
		CompletedAt:              &completedAt,
		CreatedAt:                time.Unix(1, 0).UTC(),
		UpdatedAt:                time.Unix(2, 0).UTC(),
	}
}

func (f *fakeControlPlaneStore) RequestDelegation(context.Context, store.RequestDelegationCommand) (store.RequestDelegationResult, error) {
	return store.RequestDelegationResult{}, store.ErrInvalidArgument
}

func (f *fakeControlPlaneStore) GetDelegationByRun(_ context.Context, pid, childRunID string) (store.Delegation, error) {
	if pid != projectID || childRunID != delegatedRunID {
		return store.Delegation{}, store.ErrNotFound
	}
	return delegationFixture(), nil
}

func (f *fakeControlPlaneStore) ListDelegationsByParentRun(_ context.Context, pid, parentRunID string) ([]store.Delegation, error) {
	if pid != projectID || parentRunID != runID {
		return nil, store.ErrNotFound
	}
	return []store.Delegation{delegationFixture()}, nil
}

func TestDelegationInspectionRoutesAreReadOnly(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))

	request := httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/runs/"+runID+"/delegations", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var listed []DelegationDTO
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != delegationFixture().ID || listed[0].WorkspaceAccess != store.DelegationWorkspaceAccessWrite || listed[0].ParentRunStatus != "PAUSED" || listed[0].DelegatedRunStatus != "COMPLETED" || listed[0].Outcome == nil || *listed[0].Outcome != store.DelegationOutcomeSucceeded || listed[0].ResultSummary == nil || *listed[0].ResultSummary != "bounded result" || listed[0].ResultEventID == nil || listed[0].WorkspaceChangesAccepted == nil || !*listed[0].WorkspaceChangesAccepted || listed[0].WorkspaceRevision != "revision-1" || listed[0].ContinuationJobID == nil || listed[0].CompletedAt == nil {
		t.Fatalf("listed=%+v", listed)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/runs/"+delegatedRunID+"/delegation", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", response.Code, response.Body.String())
	}
	var child DelegationDTO
	if err := json.Unmarshal(response.Body.Bytes(), &child); err != nil {
		t.Fatal(err)
	}
	if child.ParentRunID != runID || child.DelegatedRunID != delegatedRunID || child.WorkspaceAccess != store.DelegationWorkspaceAccessWrite || child.ParentRunStatus != "PAUSED" || child.DelegatedRunStatus != "COMPLETED" || child.Outcome == nil || *child.Outcome != store.DelegationOutcomeSucceeded || child.WorkspaceRevision != "revision-1" {
		t.Fatalf("child=%+v", child)
	}

	body := `{"targetAgentId":"` + otherID + `","task":"must not be public","requestKey":"forged-parent"}`
	request = httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/runs/"+runID+"/delegations", strings.NewReader(body))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d want=%d body=%s", response.Code, http.StatusMethodNotAllowed, response.Body.String())
	}
}

func TestAgentCreateRoundTripsAllowDelegation(t *testing.T) {
	router := NewRouter(app.New(&fakeControlPlaneStore{}))
	body := `{"name":"Delegator","engine":"test","modelProfileId":"` + modelID + `","engineSettings":{},"concurrencyLimit":1,"allowDelegation":true,"state":"ENABLED"}`
	request := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/agents", strings.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var agent AgentDTO
	if err := json.Unmarshal(response.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}
	if !agent.AllowDelegation {
		t.Fatalf("agent=%+v", agent)
	}
}
