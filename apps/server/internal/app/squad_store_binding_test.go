package app

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type squadStoreHidingDecorator struct {
	store.ControlPlaneStore
}

func TestSquadProjectAccessUsesBaseStoreBehindControlPlaneDecorator(t *testing.T) {
	base := newSquadAccessTestStore()
	base.roles[squadProjectID+":admin"] = store.ProjectRoleAdmin
	decorated := &squadStoreHidingDecorator{ControlPlaneStore: base}
	controlPlane := New(decorated)
	access, err := NewProjectAccessService(controlPlane, base)
	if err != nil {
		t.Fatal(err)
	}

	created, err := access.CreateSquad(t.Context(), activeProjectActor("admin", store.DeploymentRoleMember), store.Squad{
		ProjectID:     squadProjectID,
		Name:          "Backend",
		LeaderAgentID: squadLeaderID,
	})
	if err != nil {
		t.Fatalf("CreateSquad through decorated control plane: %v", err)
	}
	if created.ID != squadID {
		t.Fatalf("created id=%q want %q", created.ID, squadID)
	}
}
