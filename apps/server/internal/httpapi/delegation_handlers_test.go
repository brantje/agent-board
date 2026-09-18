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
	return store.Delegation{
		ID:             "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		ProjectID:      projectID,
		IssueID:        issueID,
		ParentRunID:    runID,
		ParentAgentID:  agentID,
		TargetAgentID:  otherID,
		Task:           "inspect scheduler ownership",
		DelegatedRunID: delegatedRunID,
		RequestKey:     "request-1",
		CreatedAt:      time.Unix(1, 0).UTC(),
		UpdatedAt:      time.Unix(1, 0).UTC(),
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
	if len(listed) != 1 || listed[0].ID != delegationFixture().ID || listed[0].WorkspaceAccess != store.DelegationWorkspaceAccessWrite {
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
	if child.ParentRunID != runID || child.DelegatedRunID != delegatedRunID || child.WorkspaceAccess != store.DelegationWorkspaceAccessWrite {
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
