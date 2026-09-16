package httpapi

import (
	"net/http"
	"testing"
)

func TestSquadHTTPUpdateAndDeleteErrorPaths(t *testing.T) {
	fake := newSquadHTTPStore()
	router := squadHTTPRouter(t, fake, "admin")
	validBody := map[string]any{
		"name":          "Core",
		"leaderAgentId": squadHTTPLeaderID,
		"members":       []any{},
	}

	missingMembers := squadHTTPRequest(t, router, http.MethodPut, "/projects/"+squadHTTPProjectID+"/squads/"+squadHTTPID, map[string]any{
		"name":          "Core",
		"leaderAgentId": squadHTTPLeaderID,
	})
	if missingMembers.Code != http.StatusBadRequest {
		t.Fatalf("missing members status=%d body=%s", missingMembers.Code, missingMembers.Body.String())
	}

	invalidLeader := squadHTTPRequest(t, router, http.MethodPut, "/projects/"+squadHTTPProjectID+"/squads/"+squadHTTPID, map[string]any{
		"name":          "Core",
		"leaderAgentId": "not-a-uuid",
		"members":       []any{},
	})
	if invalidLeader.Code != http.StatusBadRequest {
		t.Fatalf("invalid leader status=%d body=%s", invalidLeader.Code, invalidLeader.Body.String())
	}

	invalidUpdateID := squadHTTPRequest(t, router, http.MethodPut, "/projects/"+squadHTTPProjectID+"/squads/not-a-uuid", validBody)
	if invalidUpdateID.Code != http.StatusBadRequest {
		t.Fatalf("invalid update id status=%d body=%s", invalidUpdateID.Code, invalidUpdateID.Body.String())
	}

	missingUpdate := squadHTTPRequest(t, router, http.MethodPut, "/projects/"+squadHTTPProjectID+"/squads/"+squadHTTPID, validBody)
	if missingUpdate.Code != http.StatusNotFound {
		t.Fatalf("missing update status=%d body=%s", missingUpdate.Code, missingUpdate.Body.String())
	}

	invalidDeleteID := squadHTTPRequest(t, router, http.MethodDelete, "/projects/"+squadHTTPProjectID+"/squads/not-a-uuid", nil)
	if invalidDeleteID.Code != http.StatusBadRequest {
		t.Fatalf("invalid delete id status=%d body=%s", invalidDeleteID.Code, invalidDeleteID.Body.String())
	}

	missingDelete := squadHTTPRequest(t, router, http.MethodDelete, "/projects/"+squadHTTPProjectID+"/squads/"+squadHTTPID, nil)
	if missingDelete.Code != http.StatusNotFound {
		t.Fatalf("missing delete status=%d body=%s", missingDelete.Code, missingDelete.Body.String())
	}
}
