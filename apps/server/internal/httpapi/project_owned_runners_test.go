package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reservationFailureRunnerStore struct {
	*runnerAPIStore
}

func (s *reservationFailureRunnerStore) CountRunnerReservations(context.Context, []string) (map[string]int, error) {
	return nil, errors.New("reservation lookup failed")
}

type projectCapacityRunnerStore struct {
	*runnerAPIStore
	values       []store.Runner
	reservations map[string]int
}

func (s *projectCapacityRunnerStore) ListRunners(context.Context) ([]store.Runner, error) {
	return append([]store.Runner(nil), s.values...), nil
}

func (s *projectCapacityRunnerStore) CountRunnerReservations(_ context.Context, ids []string) (map[string]int, error) {
	counts := map[string]int{}
	for _, id := range ids {
		counts[id] = s.reservations[id]
	}
	return counts, nil
}

func TestProjectOwnedRunnerAPIScopeAndRegistration(t *testing.T) {
	sharedStore := &runnerAPIStore{}
	sharedRouter := NewRouter(app.New(sharedStore))
	sharedCreate := runnerAPIRequest(sharedRouter, http.MethodPost, "/api/runners", "")
	if sharedCreate.Code != http.StatusCreated {
		t.Fatalf("shared create=%d %s", sharedCreate.Code, sharedCreate.Body.String())
	}
	sharedList := runnerAPIRequest(sharedRouter, http.MethodGet, "/api/runners", "")
	if sharedList.Code != http.StatusOK || !strings.Contains(sharedList.Body.String(), `"projectId":null`) {
		t.Fatalf("shared list=%d %s", sharedList.Code, sharedList.Body.String())
	}

	ownedStore := &runnerAPIStore{}
	ownedRouter := NewRouter(app.New(ownedStore))
	created := runnerAPIRequest(ownedRouter, http.MethodPost, "/api/projects/"+projectID+"/runners", "")
	if created.Code != http.StatusCreated {
		t.Fatalf("owned create=%d %s", created.Code, created.Body.String())
	}
	if ownedStore.value.ProjectID == nil || *ownedStore.value.ProjectID != projectID {
		t.Fatalf("persisted owner=%v", ownedStore.value.ProjectID)
	}
	var response struct {
		Runner struct {
			ID string `json:"id"`
		} `json:"runner"`
		RegistrationToken string `json:"registrationToken"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil || response.RegistrationToken == "" {
		t.Fatalf("creation response=%#v err=%v", response, err)
	}

	projectList := runnerAPIRequest(ownedRouter, http.MethodGet, "/api/projects/"+projectID+"/runners", "")
	if projectList.Code != http.StatusOK || !strings.Contains(projectList.Body.String(), `"projectId":"`+projectID+`"`) || !strings.Contains(projectList.Body.String(), `"sharedRunners":[]`) {
		t.Fatalf("project list=%d %s", projectList.Code, projectList.Body.String())
	}
	uppercaseList := runnerAPIRequest(ownedRouter, http.MethodGet, "/api/projects/"+strings.ToUpper(projectID)+"/runners", "")
	if uppercaseList.Code != http.StatusOK || !strings.Contains(uppercaseList.Body.String(), `"projectId":"`+projectID+`"`) {
		t.Fatalf("uppercase project list=%d %s", uppercaseList.Code, uppercaseList.Body.String())
	}
	globalList := runnerAPIRequest(ownedRouter, http.MethodGet, "/api/runners", "")
	if globalList.Code != http.StatusOK || !strings.Contains(globalList.Body.String(), `"projectId":"`+projectID+`"`) {
		t.Fatalf("global list=%d %s", globalList.Code, globalList.Body.String())
	}

	registered := runnerAPIRequest(ownedRouter, http.MethodPost, "/api/runner/register", `{"registrationToken":"`+response.RegistrationToken+`","hostname":"project-host"}`)
	if registered.Code != http.StatusCreated || ownedStore.value.ProjectID == nil || *ownedStore.value.ProjectID != projectID {
		t.Fatalf("register=%d owner=%v body=%s", registered.Code, ownedStore.value.ProjectID, registered.Body.String())
	}

	uppercaseUpdate := runnerAPIRequest(ownedRouter, http.MethodPatch, "/api/projects/"+strings.ToUpper(projectID)+"/runners/"+response.Runner.ID, `{"name":"uppercase-project"}`)
	if uppercaseUpdate.Code != http.StatusOK || ownedStore.value.Name != "uppercase-project" {
		t.Fatalf("uppercase project update=%d %s", uppercaseUpdate.Code, uppercaseUpdate.Body.String())
	}
	foreignUpdate := runnerAPIRequest(ownedRouter, http.MethodPatch, "/api/projects/"+otherID+"/runners/"+response.Runner.ID, `{"name":"wrong-project"}`)
	if foreignUpdate.Code != http.StatusNotFound {
		t.Fatalf("foreign update=%d %s", foreignUpdate.Code, foreignUpdate.Body.String())
	}
}

func TestProjectRunnerSettingsExposeInternalRunnerCapacity(t *testing.T) {
	now := time.Now().UTC()
	internalID := "33333333-3333-4333-8333-333333333333"
	memory := &projectCapacityRunnerStore{
		runnerAPIStore: &runnerAPIStore{},
		values: []store.Runner{{
			ID: internalID, Name: "Internal runner", Internal: true, RegisteredAt: &now,
			Capabilities: json.RawMessage(`{"engines":["opencode"],"features":["stdin","health"],"max_active_sessions":3}`),
		}},
		reservations: map[string]int{internalID: 1},
	}
	router := NewRouter(app.New(memory))

	response := runnerAPIRequest(router, http.MethodGet, "/api/projects/"+projectID+"/runners", "")
	if response.Code != http.StatusOK {
		t.Fatalf("project settings=%d %s", response.Code, response.Body.String())
	}
	var body struct {
		InternalRunner *RunnerDTO `json:"internalRunner"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.InternalRunner == nil || !body.InternalRunner.Internal || body.InternalRunner.ID != internalID {
		t.Fatalf("internal runner=%#v", body.InternalRunner)
	}
	if body.InternalRunner.MaxActiveSessions != 3 || body.InternalRunner.ReservedSessions != 1 {
		t.Fatalf("internal capacity=%#v", body.InternalRunner)
	}
	if !strings.Contains(string(body.InternalRunner.Capabilities), `"features":["stdin","health"]`) {
		t.Fatalf("internal capabilities=%s", body.InternalRunner.Capabilities)
	}
}

func TestProjectRunnerRotationDoesNotMutateWhenResponseEnrichmentFails(t *testing.T) {
	owner := projectID
	now := time.Now().UTC()
	tokenHash := make([]byte, 32)
	tokenHash[0] = 7
	base := &runnerAPIStore{value: store.Runner{
		ID: otherID, ProjectID: &owner, Name: "owned-host", TokenHash: tokenHash, RegisteredAt: &now,
	}}
	router := NewRouter(app.New(&reservationFailureRunnerStore{runnerAPIStore: base}))

	response := runnerAPIRequest(router, http.MethodPost, "/api/projects/"+projectID+"/runners/"+otherID+"/rotate-token", "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("rotate=%d %s", response.Code, response.Body.String())
	}
	if !bytes.Equal(base.value.TokenHash, tokenHash) {
		t.Fatal("runner credential rotated before response enrichment succeeded")
	}
}
