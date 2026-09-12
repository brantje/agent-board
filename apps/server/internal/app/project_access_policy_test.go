package app

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestProjectAccessSharedAuthorizationPolicy(t *testing.T) {
	project := store.Project{ID: "project-1", Name: "Project"}
	fake := &projectAccessServiceStore{
		projects: []store.Project{project},
		roles: map[string]string{
			project.ID + ":viewer": store.ProjectRoleViewer,
			project.ID + ":member": store.ProjectRoleMember,
			project.ID + ":admin":  store.ProjectRoleAdmin,
		},
	}
	service := newProjectAccessServiceForTest(t, fake)

	viewer := activeProjectActor("viewer", store.DeploymentRoleMember)
	member := activeProjectActor("member", store.DeploymentRoleMember)
	admin := activeProjectActor("admin", store.DeploymentRoleMember)

	if err := service.AuthorizeRead(t.Context(), viewer, project.ID); err != nil {
		t.Fatalf("viewer read denied: %v", err)
	}
	if err := service.AuthorizeWorkflowMutation(t.Context(), viewer, project.ID); appErrorCode(err) != "forbidden" {
		t.Fatalf("viewer workflow mutation error=%v", err)
	}
	if err := service.AuthorizeAdministration(t.Context(), viewer, project.ID); appErrorCode(err) != "forbidden" {
		t.Fatalf("viewer administration error=%v", err)
	}

	if err := service.AuthorizeRead(t.Context(), member, project.ID); err != nil {
		t.Fatalf("member read denied: %v", err)
	}
	if err := service.AuthorizeWorkflowMutation(t.Context(), member, project.ID); err != nil {
		t.Fatalf("member workflow mutation denied: %v", err)
	}
	if err := service.AuthorizeAdministration(t.Context(), member, project.ID); appErrorCode(err) != "forbidden" {
		t.Fatalf("member administration error=%v", err)
	}

	if err := service.AuthorizeRead(t.Context(), admin, project.ID); err != nil {
		t.Fatalf("admin read denied: %v", err)
	}
	if err := service.AuthorizeWorkflowMutation(t.Context(), admin, project.ID); err != nil {
		t.Fatalf("admin workflow mutation denied: %v", err)
	}
	if err := service.AuthorizeAdministration(t.Context(), admin, project.ID); err != nil {
		t.Fatalf("admin administration denied: %v", err)
	}

	deploymentAdmin := activeProjectActor("deployment-admin", store.DeploymentRoleAdmin)
	if err := service.AuthorizeAdministration(t.Context(), deploymentAdmin, project.ID); err != nil {
		t.Fatalf("deployment admin implicit Project admin denied: %v", err)
	}

	unrelated := activeProjectActor("unrelated", store.DeploymentRoleMember)
	if err := service.AuthorizeRead(t.Context(), unrelated, project.ID); appErrorCode(err) != "project_not_found" {
		t.Fatalf("unrelated read error=%v", err)
	}
}

func appErrorCode(err error) string {
	apiErr, ok := AsError(err)
	if !ok {
		return ""
	}
	return apiErr.Code
}
