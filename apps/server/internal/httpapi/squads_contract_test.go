package httpapi

import (
	"net/http"
	"testing"
)

func TestSquadHTTPRequiresMembersField(t *testing.T) {
	for name, body := range map[string]map[string]any{
		"missing": {
			"name":          "Core",
			"leaderAgentId": squadHTTPLeaderID,
		},
		"null": {
			"name":          "Core",
			"leaderAgentId": squadHTTPLeaderID,
			"members":       nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			res := squadHTTPRequest(t, squadHTTPRouter(t, newSquadHTTPStore(), "admin"), http.MethodPost, "/projects/"+squadHTTPProjectID+"/squads", body)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
		})
	}
}
