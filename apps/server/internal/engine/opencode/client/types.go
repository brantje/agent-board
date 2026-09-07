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

type Event struct {
	ID       string                     `json:"id"`
	Type     string                     `json:"type"`
	Data     json.RawMessage            `json:"data"`
	Metadata map[string]json.RawMessage `json:"metadata,omitempty"`
	Durable  *struct {
		AggregateID string `json:"aggregateID"`
		Seq         int64  `json:"seq"`
		Version     int    `json:"version"`
	} `json:"durable,omitempty"`
}
