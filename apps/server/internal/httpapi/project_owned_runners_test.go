package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
)

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
	globalList := runnerAPIRequest(ownedRouter, http.MethodGet, "/api/runners", "")
	if globalList.Code != http.StatusOK || !strings.Contains(globalList.Body.String(), `"projectId":"`+projectID+`"`) {
		t.Fatalf("global list=%d %s", globalList.Code, globalList.Body.String())
	}

	registered := runnerAPIRequest(ownedRouter, http.MethodPost, "/api/runner/register", `{"registrationToken":"`+response.RegistrationToken+`","hostname":"project-host"}`)
	if registered.Code != http.StatusCreated || ownedStore.value.ProjectID == nil || *ownedStore.value.ProjectID != projectID {
		t.Fatalf("register=%d owner=%v body=%s", registered.Code, ownedStore.value.ProjectID, registered.Body.String())
	}

	foreignUpdate := runnerAPIRequest(ownedRouter, http.MethodPatch, "/api/projects/"+otherID+"/runners/"+response.Runner.ID, `{"name":"wrong-project"}`)
	if foreignUpdate.Code != http.StatusNotFound {
		t.Fatalf("foreign update=%d %s", foreignUpdate.Code, foreignUpdate.Body.String())
	}
}
