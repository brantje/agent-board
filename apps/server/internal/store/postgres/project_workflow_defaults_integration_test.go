package postgres

import (
	"encoding/json"
	"testing"
)

func TestNewProjectStrictOrderDefaultAndExplicitOverride(t *testing.T) {
	s := New(testPool(t))

	defaulted, err := s.CreateProject(t.Context(), testProjectInput("strict-order-default", "/repo/strict-order-default", "SOD"))
	if err != nil {
		t.Fatal(err)
	}
	var defaultSettings map[string]any
	if err := json.Unmarshal(defaulted.WorkflowSettings, &defaultSettings); err != nil {
		t.Fatal(err)
	}
	if strict, ok := defaultSettings["strictOrder"].(bool); !ok || !strict {
		t.Fatalf("default strictOrder=%v want true", defaultSettings["strictOrder"])
	}

	explicit := testProjectInput("strict-order-disabled", "/repo/strict-order-disabled", "SOF")
	explicit.WorkflowSettings = json.RawMessage(`{"strictOrder":false,"other":"kept"}`)
	disabled, err := s.CreateProject(t.Context(), explicit)
	if err != nil {
		t.Fatal(err)
	}
	var explicitSettings map[string]any
	if err := json.Unmarshal(disabled.WorkflowSettings, &explicitSettings); err != nil {
		t.Fatal(err)
	}
	if strict, ok := explicitSettings["strictOrder"].(bool); !ok || strict {
		t.Fatalf("explicit strictOrder=%v want false", explicitSettings["strictOrder"])
	}
	if explicitSettings["other"] != "kept" {
		t.Fatalf("unrelated workflow setting lost: %v", explicitSettings)
	}
}
