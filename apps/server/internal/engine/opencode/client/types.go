package client

import "encoding/json"

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

// Event is the native OpenCode v2 event envelope used by v1.18.29. Event
// payloads are carried in properties; unknown future top-level fields are
// intentionally ignored by encoding/json.
type Event struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}
