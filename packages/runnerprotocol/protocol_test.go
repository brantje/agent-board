package runnerprotocol

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestMessageRoundTrip(t *testing.T) {
	want, err := NewMessage(Version1, TypeStdout, "session-1", StreamData{Data: []byte("hello\n")})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch: got %#v want %#v", got, want)
	}
	payload, err := DecodePayload[StreamData](got)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload.Data) != "hello\n" {
		t.Fatalf("unexpected stream payload %q", payload.Data)
	}
}

func TestProtocolValidation(t *testing.T) {
	if _, err := Decode([]byte(`{"version":2,"type":"health"}`)); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("expected unsupported-version error, got %v", err)
	}
	if _, err := Decode([]byte(`{"version":1,"type":"health","surprise":true}`)); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("expected invalid-message error, got %v", err)
	}
	if err := (Message{Version: Version1, Type: TypeStart}).Validate(); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("expected session scoping error, got %v", err)
	}
}

func TestRunnerHelloPayloadShape(t *testing.T) {
	msg, err := NewMessage(Version1, TypeRunnerHello, "", RunnerHello{Version: Version1, Capabilities: Capabilities{MaxActiveSessions: 1, Features: []string{"stdin", "stdout"}}})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["version"] != float64(Version1) {
		t.Fatalf("unexpected version payload: %#v", payload)
	}
}
