package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPhase3GroupHTTPAllRoutesRequireAdmin(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _ := newPhase3GroupHTTPHandler(t, &now)
	groupID := "00000000-0000-0000-0000-000000000001"
	userID := "00000000-0000-0000-0000-000000000002"

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list groups", method: http.MethodGet, path: "/api/groups"},
		{name: "create group", method: http.MethodPost, path: "/api/groups", body: `{"name":"team"}`},
		{name: "update group", method: http.MethodPatch, path: "/api/groups/" + groupID, body: `{"name":"team"}`},
		{name: "delete group", method: http.MethodDelete, path: "/api/groups/" + groupID},
		{name: "list members", method: http.MethodGet, path: "/api/groups/" + groupID + "/members"},
		{name: "add member", method: http.MethodPost, path: "/api/groups/" + groupID + "/members", body: `{"userID":"` + userID + `"}`},
		{name: "remove member", method: http.MethodDelete, path: "/api/groups/" + groupID + "/members/" + userID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := authHTTPRequest(t, handler, tt.method, tt.path, tt.body, nil)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestPhase3GroupHTTPValidationAndNotFoundContracts(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _ := newPhase3GroupHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + admin.AccessToken}

	created := authHTTPRequest(t, handler, http.MethodPost, "/api/groups", `{"name":"team"}`, headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("create group status=%d body=%s", created.Code, created.Body.String())
	}
	var group groupResponse
	if err := json.Unmarshal(created.Body.Bytes(), &group); err != nil {
		t.Fatal(err)
	}

	invalidPaths := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "update group", method: http.MethodPatch, path: "/api/groups/not-a-uuid", body: `{"name":"renamed"}`},
		{name: "delete group", method: http.MethodDelete, path: "/api/groups/not-a-uuid"},
		{name: "list members", method: http.MethodGet, path: "/api/groups/not-a-uuid/members"},
		{name: "add member", method: http.MethodPost, path: "/api/groups/not-a-uuid/members", body: `{"userID":"00000000-0000-0000-0000-000000000002"}`},
		{name: "remove member group", method: http.MethodDelete, path: "/api/groups/not-a-uuid/members/00000000-0000-0000-0000-000000000002"},
		{name: "remove member user", method: http.MethodDelete, path: "/api/groups/" + group.ID + "/members/not-a-uuid"},
	}
	for _, tt := range invalidPaths {
		t.Run("invalid path "+tt.name, func(t *testing.T) {
			response := authHTTPRequest(t, handler, tt.method, tt.path, tt.body, headers)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}

	badUpdate := authHTTPRequest(t, handler, http.MethodPatch, "/api/groups/"+group.ID, `{"name":"renamed","role":"admin"}`, headers)
	if badUpdate.Code != http.StatusBadRequest {
		t.Fatalf("unknown update field status=%d body=%s", badUpdate.Code, badUpdate.Body.String())
	}
	malformedUpdate := authHTTPRequest(t, handler, http.MethodPatch, "/api/groups/"+group.ID, `{"name":`, headers)
	if malformedUpdate.Code != http.StatusBadRequest {
		t.Fatalf("malformed update status=%d body=%s", malformedUpdate.Code, malformedUpdate.Body.String())
	}
	blankUpdate := authHTTPRequest(t, handler, http.MethodPatch, "/api/groups/"+group.ID, `{"name":"   "}`, headers)
	if blankUpdate.Code != http.StatusBadRequest {
		t.Fatalf("blank update status=%d body=%s", blankUpdate.Code, blankUpdate.Body.String())
	}

	invalidUser := authHTTPRequest(t, handler, http.MethodPost, "/api/groups/"+group.ID+"/members", `{"userID":"not-a-uuid"}`, headers)
	if invalidUser.Code != http.StatusBadRequest {
		t.Fatalf("invalid member user status=%d body=%s", invalidUser.Code, invalidUser.Body.String())
	}
	badMemberShape := authHTTPRequest(t, handler, http.MethodPost, "/api/groups/"+group.ID+"/members", `{"userID":"00000000-0000-0000-0000-000000000002","memberType":"group"}`, headers)
	if badMemberShape.Code != http.StatusBadRequest {
		t.Fatalf("generic member shape status=%d body=%s", badMemberShape.Code, badMemberShape.Body.String())
	}

	missingGroup := "00000000-0000-0000-0000-000000000099"
	missingUser := "00000000-0000-0000-0000-000000000098"
	notFoundRequests := []struct {
		name   string
		method string
		path   string
		body   string
		code   string
	}{
		{name: "update group", method: http.MethodPatch, path: "/api/groups/" + missingGroup, body: `{"name":"missing"}`, code: "group_not_found"},
		{name: "delete group", method: http.MethodDelete, path: "/api/groups/" + missingGroup, code: "group_not_found"},
		{name: "list members", method: http.MethodGet, path: "/api/groups/" + missingGroup + "/members", code: "group_not_found"},
		{name: "add missing user", method: http.MethodPost, path: "/api/groups/" + group.ID + "/members", body: `{"userID":"` + missingUser + `"}`, code: "user_not_found"},
		{name: "remove missing group", method: http.MethodDelete, path: "/api/groups/" + missingGroup + "/members/" + missingUser, code: "group_not_found"},
		{name: "remove absent membership", method: http.MethodDelete, path: "/api/groups/" + group.ID + "/members/" + missingUser, code: "group_member_not_found"},
	}
	for _, tt := range notFoundRequests {
		t.Run("not found "+tt.name, func(t *testing.T) {
			response := authHTTPRequest(t, handler, tt.method, tt.path, tt.body, headers)
			if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"`+tt.code+`"`) {
				t.Fatalf("status=%d code=%q body=%s", response.Code, tt.code, response.Body.String())
			}
		})
	}
}
