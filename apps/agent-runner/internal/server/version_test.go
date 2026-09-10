package server

import (
	"testing"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
	"github.com/gorilla/websocket"
)

func TestHandshakeAdvertisesEmbeddedRunnerVersion(t *testing.T) {
	original := Version
	Version = "v0.1.23-test"
	defer func() { Version = original }()

	_, httpServer := newTestRunner(t)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(httpServer.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	send(t, conn, protocol.TypeServerHello, "", protocol.ServerHello{SupportedVersions: []int{protocol.Version2}})
	helloMsg := read(t, conn)
	hello, err := protocol.DecodePayload[protocol.RunnerHello](helloMsg)
	if err != nil {
		t.Fatal(err)
	}
	if hello.Capabilities.RunnerVersion != "v0.1.23-test" {
		t.Fatalf("runner_version=%q", hello.Capabilities.RunnerVersion)
	}
}
