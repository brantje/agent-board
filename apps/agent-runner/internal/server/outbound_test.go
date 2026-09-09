package server

import (
	"context"
	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOutboundConnectionAuthenticatesAndExecutes(t *testing.T) {
	done := make(chan error, 1)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get(protocol.RunnerIDHeader) != "runner" || r.URL.RawQuery != "" {
			http.Error(w, "auth", 401)
			return
		}
		u := websocket.Upgrader{}
		c, err := u.Upgrade(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		send := func(typ protocol.MessageType, id string, payload any) {
			m, _ := protocol.NewMessage(protocol.Version2, typ, id, payload)
			if err = c.WriteJSON(m); err != nil {
				t.Error(err)
			}
		}
		send(protocol.TypeServerHello, "", protocol.ServerHello{SupportedVersions: []int{protocol.Version2}})
		var hello, health protocol.Message
		if err = c.ReadJSON(&hello); err != nil {
			done <- err
			return
		}
		caps, e := protocol.DecodePayload[protocol.RunnerHello](hello)
		if e != nil || caps.Capabilities.Validate() != nil {
			t.Error("invalid capabilities")
		}
		if err = c.ReadJSON(&health); err != nil {
			done <- err
			return
		}
		send(protocol.TypeStart, "session", protocol.StartRequest{Command: []string{"sh", "-c", "printf hello"}})
		output := ""
		for {
			var m protocol.Message
			if err = c.ReadJSON(&m); err != nil {
				done <- err
				return
			}
			if m.Type == protocol.TypeStdout {
				v, _ := protocol.DecodePayload[protocol.StreamData](m)
				output += string(v.Data)
			}
			if m.Type == protocol.TypeError {
				t.Errorf("runner error: %s", m.Payload)
				done <- nil
				return
			}
			if m.Type == protocol.TypeExit {
				if output != "hello" {
					t.Errorf("output %q", output)
				}
				done <- nil
				return
			}
		}
	}))
	defer host.Close()
	runner := New(Config{WorkspaceRoot: t.TempDir(), MaxActiveSessions: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Connect(ctx, host.URL, "runner", "token") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("outbound execution timed out")
	}
	cancel()
	if err := runner.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
