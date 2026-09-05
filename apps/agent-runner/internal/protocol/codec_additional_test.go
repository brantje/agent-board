package protocol

import (
	"errors"
	"testing"
)

func TestDecodeRejectsMultipleJSONValues(t *testing.T) {
	_, err := Decode([]byte(`{"version":1,"type":"health"} {"version":1,"type":"health"}`))
	if !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("expected invalid-message error, got %v", err)
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

func TestErrorMessageMayBeSessionScoped(t *testing.T) {
	msg, err := NewMessage(Version1, TypeError, "session-1", ErrorPayload{Code: "example", Message: "example"})
	if err != nil {
		t.Fatal(err)
	}
	if msg.SessionID != "session-1" {
		t.Fatalf("unexpected session id %q", msg.SessionID)
	}
}
