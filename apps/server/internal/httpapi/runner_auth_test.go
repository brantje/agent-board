package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

func TestRunnerWebSocketAcceptsOnlyPermanentRegisteredCredential(t *testing.T) {
	memory := &runnerAPIStore{}
	server := httptest.NewServer(NewRouter(app.New(memory)))
	defer server.Close()

	creation := runnerAPIRequest(NewRouter(app.New(memory)), http.MethodPost, "/api/runners", "")
	if creation.Code != http.StatusCreated {
		t.Fatalf("create runner: %d %s", creation.Code, creation.Body.String())
	}
	var created struct {
		Runner struct {
			ID string `json:"id"`
		} `json:"runner"`
		RegistrationToken string `json:"registrationToken"`
	}
	if err := json.Unmarshal(creation.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/runner/ws"
	dial := func(token string, wantStatus int) *websocket.Conn {
		t.Helper()
		conn, response, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
			"Authorization":            []string{"Bearer " + token},
			protocol.RunnerIDHeader: []string{created.Runner.ID},
		})
		if wantStatus == http.StatusSwitchingProtocols {
			if err != nil || conn == nil {
				t.Fatalf("registered websocket credential rejected: response=%v err=%v", response, err)
			}
			return conn
		}
		if conn != nil {
			conn.Close()
		}
		if err == nil || response == nil || response.StatusCode != wantStatus {
			t.Fatalf("websocket status=%v err=%v, want %d", response, err, wantStatus)
		}
		response.Body.Close()
		return nil
	}

	dial(created.RegistrationToken, http.StatusUnauthorized)

	registration := runnerAPIRequest(NewRouter(app.New(memory)), http.MethodPost, "/api/runner/register", `{"registrationToken":"`+created.RegistrationToken+`","hostname":"auth-host"}`)
	if registration.Code != http.StatusCreated {
		t.Fatalf("register runner: %d %s", registration.Code, registration.Body.String())
	}
	var enrolled struct {
		RunnerToken string `json:"runnerToken"`
	}
	if err := json.Unmarshal(registration.Body.Bytes(), &enrolled); err != nil || enrolled.RunnerToken == "" {
		t.Fatalf("enrollment response=%#v err=%v", enrolled, err)
	}

	first := dial(enrolled.RunnerToken, http.StatusSwitchingProtocols)
	first.Close()
	second := dial(enrolled.RunnerToken, http.StatusSwitchingProtocols)
	second.Close()
	dial(created.RegistrationToken, http.StatusUnauthorized)

	rotation := runnerAPIRequest(NewRouter(app.New(memory)), http.MethodPost, "/api/runners/"+created.Runner.ID+"/rotate-token", "")
	if rotation.Code != http.StatusOK {
		t.Fatalf("rotate runner: %d %s", rotation.Code, rotation.Body.String())
	}
	var rotated struct {
		RunnerToken string `json:"runnerToken"`
	}
	if err := json.Unmarshal(rotation.Body.Bytes(), &rotated); err != nil || rotated.RunnerToken == "" {
		t.Fatalf("rotation response=%#v err=%v", rotated, err)
	}
	dial(enrolled.RunnerToken, http.StatusUnauthorized)
	third := dial(rotated.RunnerToken, http.StatusSwitchingProtocols)
	third.Close()

	revoke := runnerAPIRequest(NewRouter(app.New(memory)), http.MethodPost, "/api/runners/"+created.Runner.ID+"/revoke", "")
	if revoke.Code != http.StatusOK {
		t.Fatalf("revoke runner: %d %s", revoke.Code, revoke.Body.String())
	}
	dial(rotated.RunnerToken, http.StatusUnauthorized)
}
