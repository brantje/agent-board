package protocol

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
		t.Fatalf("round-trip mismatch:\n got: %#v\nwant: %#v", got, want)
	}

	payload, err := DecodePayload[StreamData](got)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload.Data) != "hello\n" {
		t.Fatalf("unexpected stream payload %q", payload.Data)
	}
}

func TestDecodeRejectsUnsupportedVersion(t *testing.T) {
	_, err := Decode([]byte(`{"version":2,"type":"health"}`))
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("expected unsupported-version error, got %v", err)
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	_, err := Decode([]byte(`{"version":1,"type":"health","surprise":true}`))
	if !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("expected invalid-message error, got %v", err)
	}
}

func TestValidateSessionScoping(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
	}{
		{name: "start requires session", msg: Message{Version: Version1, Type: TypeStart}},
		{name: "hello forbids session", msg: Message{Version: Version1, Type: TypeServerHello, SessionID: "session-1"}},
		{name: "unknown type", msg: Message{Version: Version1, Type: MessageType("future")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.msg.Validate(); !errors.Is(err, ErrInvalidMessage) {
				t.Fatalf("expected invalid-message error, got %v", err)
			}
		})
	}
}

func TestNewMessageRejectsUnencodablePayload(t *testing.T) {
	_, err := NewMessage(Version1, TypeError, "", func() {})
	if err == nil {
		t.Fatal("expected payload marshal error")
	}
}

func TestDecodePayloadRequiresPayload(t *testing.T) {
	_, err := DecodePayload[Health](Message{Version: Version1, Type: TypeHealth})
	if !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("expected invalid-message error, got %v", err)
	}
}

func TestPayloadShape(t *testing.T) {
	msg, err := NewMessage(Version1, TypeRunnerHello, "", RunnerHello{
		Version: Version1,
		Capabilities: Capabilities{
			MaxActiveSessions: 1,
			Features:          []string{"stdin", "stdout", "stderr", "terminate", "kill"},
		},
	})
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
