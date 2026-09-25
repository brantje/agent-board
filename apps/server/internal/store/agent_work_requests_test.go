package store

import "testing"

func TestSameAgentWorkRequestCompatibility(t *testing.T) {
	parent := "parent-run"
	base := AgentWorkRequest{
		ProjectID: "project", IssueID: "issue", WorkspaceID: "workspace", TargetAgentID: "agent",
		AuthorityKind: AgentWorkRequestAuthorityIssue,
	}

	tests := []struct {
		name  string
		left  AgentWorkRequest
		right AgentWorkRequest
		want  bool
	}{
		{name: "same issue authority", left: base, right: base, want: true},
		{name: "different project", left: base, right: withAgentWorkProject(base, "other"), want: false},
		{name: "different issue", left: base, right: withAgentWorkIssue(base, "other"), want: false},
		{name: "different workspace", left: base, right: withAgentWorkWorkspace(base, "other"), want: false},
		{name: "different target", left: base, right: withAgentWorkTarget(base, "other"), want: false},
		{
			name:  "parent authority requires exact parent",
			left:  withAgentWorkParent(base, parent),
			right: withAgentWorkParent(base, parent),
			want:  true,
		},
		{
			name:  "different parent",
			left:  withAgentWorkParent(base, parent),
			right: withAgentWorkParent(base, "other-parent"),
			want:  false,
		},
		{
			name:  "issue and parent authority never mix",
			left:  base,
			right: withAgentWorkParent(base, parent),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SameAgentWorkRequestCompatibility(tt.left, tt.right); got != tt.want {
				t.Fatalf("SameAgentWorkRequestCompatibility()=%v want %v", got, tt.want)
			}
		})
	}
}

func withAgentWorkProject(value AgentWorkRequest, project string) AgentWorkRequest {
	value.ProjectID = project
	return value
}
func withAgentWorkIssue(value AgentWorkRequest, issue string) AgentWorkRequest {
	value.IssueID = issue
	return value
}
func withAgentWorkWorkspace(value AgentWorkRequest, workspace string) AgentWorkRequest {
	value.WorkspaceID = workspace
	return value
}
func withAgentWorkTarget(value AgentWorkRequest, target string) AgentWorkRequest {
	value.TargetAgentID = target
	return value
}
func withAgentWorkParent(value AgentWorkRequest, parent string) AgentWorkRequest {
	value.AuthorityKind = AgentWorkRequestAuthorityParentRun
	value.ParentRunID = &parent
	return value
}

func TestSameAgentWorkRequestCompatibilityRejectsUnknownAuthority(t *testing.T) {
	value := AgentWorkRequest{ProjectID: "project", IssueID: "issue", WorkspaceID: "workspace", TargetAgentID: "agent", AuthorityKind: "UNKNOWN"}
	if SameAgentWorkRequestCompatibility(value, value) {
		t.Fatal("unknown authority was treated as compatible")
	}
}
