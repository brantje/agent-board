package client

import (
	"bytes"
	"encoding/json"
)

type Health struct {
	Healthy bool   `json:"healthy"`
	Version string `json:"version"`
	PID     int    `json:"pid,omitempty"`
}

type ModelRef struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Variant    string `json:"variant,omitempty"`
}

type CreateSessionRequest struct {
	Directory string
	Model     ModelRef
}

type Session struct {
	ID string `json:"id"`
}

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type QuestionInfo struct {
	Question string           `json:"question"`
	Header   string           `json:"header"`
	Options  []QuestionOption `json:"options"`
	Multiple *bool            `json:"multiple,omitempty"`
	Custom   *bool            `json:"custom,omitempty"`
}

type QuestionRequest struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionID"`
	Questions []QuestionInfo `json:"questions"`
	Tool      *struct {
		MessageID string `json:"messageID"`
		CallID    string `json:"callID"`
	} `json:"tool,omitempty"`
}

// Event is the normalized native OpenCode event envelope used by the adapter.
// OpenCode v1.18.29's /api/event SSE surface carries payloads in `data`, while
// older/fake event fixtures may use `properties`. UnmarshalJSON accepts both so
// the rest of the adapter has one stable payload field.
type Event struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"-"`
}

func (e *Event) UnmarshalJSON(input []byte) error {
	var wire struct {
		ID         string          `json:"id"`
		Type       string          `json:"type"`
		Data       json.RawMessage `json:"data"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(input, &wire); err != nil {
		return err
	}

	e.ID = wire.ID
	e.Type = wire.Type
	e.Properties = nil
	if hasJSONValue(wire.Data) {
		e.Properties = append(e.Properties, wire.Data...)
	} else if hasJSONValue(wire.Properties) {
		e.Properties = append(e.Properties, wire.Properties...)
	}
	return nil
}

func hasJSONValue(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) != 0 && !bytes.Equal(trimmed, []byte("null"))
}
