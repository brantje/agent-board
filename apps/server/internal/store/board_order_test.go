package store

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestProjectNewIssuePlacement(t *testing.T) {
	tests := []struct {
		name     string
		settings json.RawMessage
		want     string
		wantErr  bool
	}{
		{name: "empty", want: ProjectNewIssuePlacementBottom},
		{name: "object default", settings: json.RawMessage(`{}`), want: ProjectNewIssuePlacementBottom},
		{name: "bottom", settings: json.RawMessage(`{"newIssuePlacement":"bottom"}`), want: ProjectNewIssuePlacementBottom},
		{name: "top", settings: json.RawMessage(`{"newIssuePlacement":"top"}`), want: ProjectNewIssuePlacementTop},
		{name: "invalid value", settings: json.RawMessage(`{"newIssuePlacement":"middle"}`), wantErr: true},
		{name: "invalid json", settings: json.RawMessage(`[]`), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProjectNewIssuePlacement(tt.settings)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidArgument) {
					t.Fatalf("error = %v, want ErrInvalidArgument", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ProjectNewIssuePlacement: %v", err)
			}
			if got != tt.want {
				t.Fatalf("placement = %q, want %q", got, tt.want)
			}
		})
	}
}
