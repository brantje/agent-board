package server

import (
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

func TestLargeOutputIsChunkedWithoutLosingChannelIdentity(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeStart, "large", protocol.StartRequest{Command: []string{"sh", "-c", "dd if=/dev/zero bs=1000 count=200 2>/dev/null; printf stderr-marker >&2"}})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted {
		t.Fatalf("unexpected start response %#v", msg)
	}

	stdoutBytes := 0
	var stderr strings.Builder
	stdoutMessages := 0
	for {
		msg := read(t, conn)
		switch msg.Type {
		case protocol.TypeStdout:
			stream, err := protocol.DecodePayload[protocol.StreamData](msg)
			if err != nil { t.Fatal(err) }
			stdoutBytes += len(stream.Data)
			stdoutMessages++
		case protocol.TypeStderr:
			stream, err := protocol.DecodePayload[protocol.StreamData](msg)
			if err != nil { t.Fatal(err) }
			stderr.Write(stream.Data)
		case protocol.TypeExit:
			if stdoutBytes != 200000 || stdoutMessages < 2 || stderr.String() != "stderr-marker" {
				t.Fatalf("unexpected streams bytes=%d chunks=%d stderr=%q", stdoutBytes, stdoutMessages, stderr.String())
			}
			return
		case protocol.TypeError:
			t.Fatalf("unexpected protocol error %#v", msg)
		}
	}
}

func TestStdinCloseIsIdempotent(t *testing.T) {
	_, httpServer := newTestRunner(t)
	conn := dialAndHandshake(t, httpServer.URL, 1)
	defer conn.Close()

	send(t, conn, protocol.TypeStart, "stdin-close", protocol.StartRequest{Command: []string{"sh", "-c", "cat >/dev/null; sleep 0.1; printf done"}})
	if msg := read(t, conn); msg.Type != protocol.TypeSessionStarted {
		t.Fatalf("unexpected start response %#v", msg)
	}
	send(t, conn, protocol.TypeStdinClose, "stdin-close", nil)
	send(t, conn, protocol.TypeStdinClose, "stdin-close", nil)

	var output strings.Builder
	for {
		msg := read(t, conn)
		switch msg.Type {
		case protocol.TypeStdout:
			stream, err := protocol.DecodePayload[protocol.StreamData](msg)
			if err != nil { t.Fatal(err) }
			output.Write(stream.Data)
		case protocol.TypeExit:
			if output.String() != "done" { t.Fatalf("unexpected stdout %q", output.String()) }
			return
		case protocol.TypeError:
			t.Fatalf("duplicate stdin close returned error %#v", msg)
		}
	}
}
