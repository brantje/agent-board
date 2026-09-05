package app

import (
	"encoding/json"
	"testing"
)

func TestValidObjectRejectsJSONNull(t *testing.T) {
	if validObject(json.RawMessage(`null`)) {
		t.Fatal("JSON null must not be accepted as an object")
	}
	if !validObject(json.RawMessage(`{}`)) {
		t.Fatal("JSON object must be accepted")
	}
}
