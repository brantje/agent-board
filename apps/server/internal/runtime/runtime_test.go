package runtime

import (
	"errors"
	"testing"
)

func validSpec() RuntimeSpec {
	return RuntimeSpec{
		RuntimeInstanceID: "instance-1",
		ProjectID:         "project-1",
		IssueID:           "issue-1",
		WorkspaceID:       "workspace-1",
		RuntimeID:         "runtime-1",
		Image:             "agent-board-runtime:test",
		WorkingDirectory:  "/workspace",
		Workspace: WorkspaceMount{
			WorkspaceID: "workspace-1",
			Source:      "/var/lib/agent-board/workspaces/workspace-1",
			Target:      WorkspaceTarget,
		},
		Network: NetworkNone,
		Labels:  map[string]string{"agent-board.test": "true"},
	}
}

func TestValidateSpecAcceptsAuthorizedWorkspaceShape(t *testing.T) {
	spec := validSpec()
	cpu, pids, timeout := 500, 128, 60
	memory := int64(512 << 20)
	spec.Resources = ResourcePolicy{CPULimitMillis: &cpu, MemoryLimitBytes: &memory, PIDLimit: &pids, TimeoutSeconds: &timeout}
	if err := ValidateSpec(spec); err != nil {
		t.Fatalf("ValidateSpec() error = %v", err)
	}
	spec.WorkingDirectory = "/workspace/subdir"
	if err := ValidateSpec(spec); err != nil {
		t.Fatalf("ValidateSpec(subdir) error = %v", err)
	}
}

func TestValidateSpecRejectsInvalidOrEscapingState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RuntimeSpec)
	}{
		{"missing identity", func(v *RuntimeSpec) { v.RuntimeInstanceID = "" }},
		{"workspace mismatch", func(v *RuntimeSpec) { v.Workspace.WorkspaceID = "other" }},
		{"missing source", func(v *RuntimeSpec) { v.Workspace.Source = "" }},
		{"wrong mount target", func(v *RuntimeSpec) { v.Workspace.Target = "/tmp/workspace" }},
		{"escaping cwd", func(v *RuntimeSpec) { v.WorkingDirectory = "/workspace/../etc" }},
		{"unknown network", func(v *RuntimeSpec) { v.Network = "host" }},
		{"blank label", func(v *RuntimeSpec) { v.Labels = map[string]string{"": "value"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := validSpec()
			tt.mutate(&spec)
			if err := ValidateSpec(spec); !errors.Is(err, ErrInvalidSpec) {
				t.Fatalf("ValidateSpec() error = %v, want ErrInvalidSpec", err)
			}
		})
	}
}

func TestValidateSpecRejectsNonPositiveResources(t *testing.T) {
	zero := 0
	zero64 := int64(0)
	tests := []ResourcePolicy{
		{CPULimitMillis: &zero},
		{MemoryLimitBytes: &zero64},
		{PIDLimit: &zero},
		{TimeoutSeconds: &zero},
	}
	for i, resources := range tests {
		spec := validSpec()
		spec.Resources = resources
		if err := ValidateSpec(spec); !errors.Is(err, ErrInvalidSpec) {
			t.Fatalf("case %d error = %v, want ErrInvalidSpec", i, err)
		}
	}
}

func TestValidateTransition(t *testing.T) {
	valid := [][2]State{
		{StateProvisioning, StateStarting},
		{StateProvisioning, StateFailed},
		{StateStarting, StateRunning},
		{StateRunning, StateStopping},
		{StateStopping, StateStopped},
		{StateStopped, StateStarting},
		{StateStopped, StateDestroyed},
		{StateFailed, StateDestroyed},
		{StateRunning, StateRunning},
	}
	for _, transition := range valid {
		if err := ValidateTransition(transition[0], transition[1]); err != nil {
			t.Errorf("%s -> %s error = %v", transition[0], transition[1], err)
		}
	}
	invalid := [][2]State{
		{StateProvisioning, StateRunning},
		{StateRunning, StateProvisioning},
		{StateDestroyed, StateStarting},
		{State("UNKNOWN"), State("UNKNOWN")},
		{StateRunning, State("UNKNOWN")},
		{State("UNKNOWN"), StateRunning},
	}
	for _, transition := range invalid {
		if err := ValidateTransition(transition[0], transition[1]); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("%s -> %s error = %v, want ErrInvalidTransition", transition[0], transition[1], err)
		}
	}
}
