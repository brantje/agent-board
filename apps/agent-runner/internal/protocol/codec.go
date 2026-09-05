package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func Encode(msg Message) ([]byte, error) {
	if err := msg.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("encode protocol message: %w", err)
	}
	return encoded, nil
}

func Decode(data []byte) (Message, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var msg Message
	if err := decoder.Decode(&msg); err != nil {
		return Message{}, fmt.Errorf("%w: decode envelope: %v", ErrInvalidMessage, err)
	}
	if decoder.More() {
		return Message{}, fmt.Errorf("%w: multiple JSON values", ErrInvalidMessage)
	}
	if err := msg.Validate(); err != nil {
		return Message{}, err
	}
	return msg, nil
}
