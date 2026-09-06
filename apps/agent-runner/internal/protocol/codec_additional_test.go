package protocol

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeRejectsMultipleJSONValues(t *testing.T) {
	_, err := Decode([]byte(`{"version":1,"type":"health"} {"version":1,"type":"health"}`))
	if !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("expected invalid-message error, got %v", err)
	}
}

func TestDecodeRejectsNullEnvelope(t *testing.T) {
	_, err := Decode([]byte(`null`))
	if !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("expected invalid-message error for null envelope, got %v", err)
	}
}

func TestEncodeRejectsInvalidMessage(t *testing.T) {
	_, err := Encode(Message{Version: Version1, Type: TypeStart})
	if !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("expected invalid-message error, got %v", err)
	}
}

func TestDecodePayloadRejectsWrongShape(t *testing.T) {
	msg := Message{Version: Version1, Type: TypeStart, SessionID: "session", Payload: []byte(`{"command":"wrong"}`)}
	if _, err := DecodePayload[StartRequest](msg); err == nil {
		t.Fatal("expected payload shape error")
	}
}

func TestDecodePayloadRejectsNullObject(t *testing.T) {
	msg := Message{Version: Version1, Type: TypeStart, SessionID: "session", Payload: []byte(`null`)}
	_, err := DecodePayload[StartRequest](msg)
	if !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("expected invalid-message error for null payload, got %v", err)
	}
}

func TestDecodePayloadRejectsUnknownFields(t *testing.T) {
	msg := Message{Version: Version1, Type: TypeStart, SessionID: "session", Payload: []byte(`{"command":["true"],"commnad":["false"]}`)}
	_, err := DecodePayload[StartRequest](msg)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

func TestErrorMessageMayBeSessionScoped(t *testing.T) {
	msg, err := NewMessage(Version1, TypeError, "session-1", ErrorPayload{Code: "example", Message: "example"})
	if err != nil {
		t.Fatal(err)
	}
	if msg.SessionID != "session-1" {
		t.Fatalf("unexpected session id %q", msg.SessionID)
	}
}
