package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSquadStoreUsesCanonicalProjectWorkflowUserEligibility(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	project, agents := createSquadProjectWithAgents(t, s, "squad-user-eligibility", 1)

	directAdmin := createSquadWorkflowUser(t, s, project.ID, "squad-direct-admin", store.ProjectRoleAdmin, store.DeploymentRoleMember, store.UserStatusActive)
	directMember := createSquadWorkflowUser(t, s, project.ID, "squad-direct-member", store.ProjectRoleMember, store.DeploymentRoleMember, store.UserStatusActive)
	viewer := createSquadWorkflowUser(t, s, project.ID, "squad-viewer", store.ProjectRoleViewer, store.DeploymentRoleMember, store.UserStatusActive)
	pending := createSquadWorkflowUser(t, s, project.ID, "squad-pending", store.ProjectRoleMember, store.DeploymentRoleMember, store.UserStatusPending)
	disabled := createSquadWorkflowUser(t, s, project.ID, "squad-disabled", store.ProjectRoleMember, store.DeploymentRoleMember, store.UserStatusDisabled)

	noAccess, err := s.CreateUser(t.Context(), authUser("squad-no-access", "squad-no-access@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	deploymentAdminInput := authUser("squad-deployment-admin", "squad-deployment-admin@example.com", store.UserStatusActive)
	deploymentAdminInput.DeploymentRole = store.DeploymentRoleAdmin
	deploymentAdmin, err := s.CreateUser(t.Context(), deploymentAdminInput)
	if err != nil {
		t.Fatal(err)
	}
	groupMember, err := s.CreateUser(t.Context(), authUser("squad-group-member", "squad-group-member@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	groupAdmin, err := s.CreateUser(t.Context(), authUser("squad-group-admin", "squad-group-admin@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	groupViewer, err := s.CreateUser(t.Context(), authUser("squad-group-viewer", "squad-group-viewer@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	addSquadGroupAccess(t, pool, project.ID, groupMember.ID, "squad-workflow-members", store.ProjectRoleMember)
	addSquadGroupAccess(t, pool, project.ID, groupAdmin.ID, "squad-workflow-admins", store.ProjectRoleAdmin)
	addSquadGroupAccess(t, pool, project.ID, groupViewer.ID, "squad-workflow-viewers", store.ProjectRoleViewer)

	tests := []struct {
		name string
		user store.User
		want error
	}{
		{name: "direct admin", user: directAdmin},
		{name: "direct member", user: directMember},
		{name: "group admin", user: groupAdmin},
		{name: "group member", user: groupMember},
		{name: "deployment admin", user: deploymentAdmin},
		{name: "viewer", user: viewer, want: store.ErrInvalidArgument},
		{name: "group viewer", user: groupViewer, want: store.ErrInvalidArgument},
		{name: "pending", user: pending, want: store.ErrInvalidArgument},
		{name: "disabled", user: disabled, want: store.ErrInvalidArgument},
		{name: "no access", user: noAccess, want: store.ErrInvalidArgument},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.ValidateProjectWorkflowUser(t.Context(), project.ID, tt.user.ID)
			if tt.want == nil && err != nil {
				t.Fatalf("ValidateProjectWorkflowUser: %v", err)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("ValidateProjectWorkflowUser error = %v, want %v", err, tt.want)
			}

			_, err = s.CreateSquad(t.Context(), store.Squad{
				ProjectID: project.ID,
				Name: "User case " + string(rune('a'+i)),
				LeaderAgentID: agents[0].ID,
				Members: []store.SquadMember{{Type: store.SquadMemberTypeUser, ID: tt.user.ID}},
			})
			if tt.want == nil && err != nil {
				t.Fatalf("CreateSquad: %v", err)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("CreateSquad error = %v, want %v", err, tt.want)
			}
		})
	}

	role, err := s.EffectiveProjectRole(t.Context(), project.ID, directMember.ID)
	if err != nil || role != store.ProjectRoleMember {
		t.Fatalf("direct member role after Squad membership = %q, %v", role, err)
	}
	var directGroupMemberGrantCount int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM project_user_access WHERE project_id=$1 AND user_id=$2`, project.ID, groupMember.ID).Scan(&directGroupMemberGrantCount); err != nil {
		t.Fatal(err)
	}
	if directGroupMemberGrantCount != 0 {
		t.Fatalf("Squad membership created %d direct Project grants for Group-derived User", directGroupMemberGrantCount)
	}
}

func createSquadWorkflowUser(t *testing.T, s *Store, projectID, name, role, deploymentRole, status string) store.User {
	t.Helper()
	input := authUser(name, name+"@example.com", status)
	input.DeploymentRole = deploymentRole
	user, err := s.CreateUser(t.Context(), input)
	if err != nil {
		t.Fatalf("create User %s: %v", name, err)
	}
	if _, err := s.UpsertProjectUserAccess(t.Context(), store.ProjectUserAccess{ProjectID: projectID, UserID: user.ID, Role: role}); err != nil {
		t.Fatalf("grant Project access to %s: %v", name, err)
	}
	return user
}

func addSquadGroupAccess(t *testing.T, pool *pgxpool.Pool, projectID, userID, name, role string) {
	t.Helper()
	var groupID string
	if err := pool.QueryRow(t.Context(), `INSERT INTO groups (name) VALUES ($1) RETURNING id::text`, name).Scan(&groupID); err != nil {
		t.Fatalf("create Group %s: %v", name, err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO group_members (group_id,user_id) VALUES ($1,$2)`, groupID, userID); err != nil {
		t.Fatalf("add Group member: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO project_group_access (project_id,group_id,role) VALUES ($1,$2,$3)`, projectID, groupID, role); err != nil {
		t.Fatalf("grant Group Project access: %v", err)
	}
}
