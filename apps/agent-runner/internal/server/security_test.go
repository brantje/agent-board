package server

import (
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

func TestExecutionSecretsAreRedactedFromStdoutAndStderr(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	secret := "sensitive-token-value"
	send(t, conn, protocol.TypeStart, "redact", protocol.StartRequest{
		Command: []string{"sh", "-c", "printf 'out:%s' \"$TOKEN\"; printf 'err:%s' \"$TOKEN\" >&2"},
		Secrets: map[string]string{"TOKEN": secret},
	})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted {
		t.Fatalf("unexpected start response %#v", msg)
	}

	var stdout, stderr strings.Builder
	for {
		msg := read(t, conn)
		switch msg.Type {
		case protocol.TypeStdout:
			stream, err := protocol.DecodePayload[protocol.StreamData](msg)
			if err != nil { t.Fatal(err) }
			stdout.Write(stream.Data)
		case protocol.TypeStderr:
			stream, err := protocol.DecodePayload[protocol.StreamData](msg)
			if err != nil { t.Fatal(err) }
			stderr.Write(stream.Data)
		case protocol.TypeExit:
			if stdout.String() != "out:***" || stderr.String() != "err:***" {
				t.Fatalf("secret redaction failed stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), secret) {
				t.Fatal("secret appeared in protocol output")
			}
			return
		}
	}
}
