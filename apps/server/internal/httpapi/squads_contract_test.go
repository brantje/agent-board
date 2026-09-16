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

func TestSquadHTTPAcceptsExplicitNullMemberRole(t *testing.T) {
	fake := newSquadHTTPStore()
	res := squadHTTPRequest(t, squadHTTPRouter(t, fake, "admin"), http.MethodPost, "/projects/"+squadHTTPProjectID+"/squads", map[string]any{
		"name":          "Core",
		"leaderAgentId": squadHTTPLeaderID,
		"members": []map[string]any{{
			"type": "AGENT",
			"id":   squadHTTPMemberID,
			"role": nil,
		}},
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	created := fake.squads[squadHTTPID]
	if len(created.Members) != 1 || created.Members[0].Role != nil {
		t.Fatalf("created members=%+v, want one member with nil role", created.Members)
	}
}
