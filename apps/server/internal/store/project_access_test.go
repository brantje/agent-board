package store

import "testing"

func TestProjectRoleOrdering(t *testing.T) {
	for _, role := range []string{ProjectRoleViewer, ProjectRoleMember, ProjectRoleAdmin} {
		if !ValidProjectRole(role) {
			t.Fatalf("expected %q to be valid", role)
		}
	}
	for _, role := range []string{"", "owner", "ADMIN"} {
		if ValidProjectRole(role) {
			t.Fatalf("expected %q to be invalid", role)
		}
	}
	if !ProjectRoleAtLeast(ProjectRoleAdmin, ProjectRoleMember) {
		t.Fatal("admin should include member")
	}
	if !ProjectRoleAtLeast(ProjectRoleMember, ProjectRoleViewer) {
		t.Fatal("member should include viewer")
	}
	if ProjectRoleAtLeast(ProjectRoleViewer, ProjectRoleMember) {
		t.Fatal("viewer must not include member")
	}
	if ProjectRoleAtLeast("owner", ProjectRoleViewer) {
		t.Fatal("unknown role must not authorize")
	}
}
