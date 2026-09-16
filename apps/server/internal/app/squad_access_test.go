package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type squadAccessTestStore struct {
	*squadTestStore
	store.ProjectAccessStore
	roles map[string]string
}

func newSquadAccessTestStore() *squadAccessTestStore {
	return &squadAccessTestStore{
		squadTestStore: newSquadTestStore(),
		roles:           map[string]string{},
	}
}

func (s *squadAccessTestStore) EffectiveProjectRole(_ context.Context, projectID, userID string) (string, error) {
	role, ok := s.roles[projectID+":"+userID]
	if !ok {
		return "", store.ErrNotFound
	}
	return role, nil
}

func newSquadProjectAccessService(t *testing.T, data *squadAccessTestStore) *ProjectAccessService {
	t.Helper()
	service, err := NewProjectAccessService(New(data), data)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestSquadProjectAccessAllowsViewerReads(t *testing.T) {
	data := newSquadAccessTestStore()
	data.roles[squadProjectID+":viewer"] = store.ProjectRoleViewer
	data.squads[squadID] = store.Squad{ID: squadID, ProjectID: squadProjectID, Name: "Backend", LeaderAgentID: squadLeaderID}
	service := newSquadProjectAccessService(t, data)
	actor := activeProjectActor("viewer", store.DeploymentRoleMember)

	listed, err := service.ListSquads(t.Context(), actor, squadProjectID)
	if err != nil || len(listed) != 1 || listed[0].ID != squadID {
		t.Fatalf("ListSquads = %+v, %v", listed, err)
	}
	got, err := service.GetSquad(t.Context(), actor, squadProjectID, squadID)
	if err != nil || got.ID != squadID {
		t.Fatalf("GetSquad = %+v, %v", got, err)
	}
}

func TestSquadProjectAccessRequiresAdminForMutations(t *testing.T) {
	data := newSquadAccessTestStore()
	data.roles[squadProjectID+":member"] = store.ProjectRoleMember
	data.roles[squadProjectID+":admin"] = store.ProjectRoleAdmin
	service := newSquadProjectAccessService(t, data)
	member := activeProjectActor("member", store.DeploymentRoleMember)
	admin := activeProjectActor("admin", store.DeploymentRoleMember)
	input := store.Squad{ProjectID: squadProjectID, Name: "Backend", LeaderAgentID: squadLeaderID}

	if _, err := service.CreateSquad(t.Context(), member, input); !isAppCode(err, "forbidden") {
		t.Fatalf("member create error = %v", err)
	}
	if data.createCalls != 0 {
		t.Fatalf("member mutation reached store %d times", data.createCalls)
	}

	created, err := service.CreateSquad(t.Context(), admin, input)
	if err != nil {
		t.Fatalf("admin CreateSquad: %v", err)
	}
	created.Name = "Platform"
	if _, err := service.UpdateSquad(t.Context(), member, created); !isAppCode(err, "forbidden") {
		t.Fatalf("member update error = %v", err)
	}
	if _, err := service.UpdateSquad(t.Context(), admin, created); err != nil {
		t.Fatalf("admin UpdateSquad: %v", err)
	}
	if err := service.DeleteSquad(t.Context(), member, squadProjectID, created.ID); !isAppCode(err, "forbidden") {
		t.Fatalf("member delete error = %v", err)
	}
	if err := service.DeleteSquad(t.Context(), admin, squadProjectID, created.ID); err != nil {
		t.Fatalf("admin DeleteSquad: %v", err)
	}
}

func TestSquadProjectAccessPreservesProjectIsolation(t *testing.T) {
	data := newSquadAccessTestStore()
	data.roles[squadProjectID+":viewer"] = store.ProjectRoleViewer
	service := newSquadProjectAccessService(t, data)
	actor := activeProjectActor("viewer", store.DeploymentRoleMember)

	if _, err := service.ListSquads(t.Context(), actor, squadOtherProjectID); !isAppCode(err, "project_not_found") {
		t.Fatalf("inaccessible project list error = %v", err)
	}
	if _, err := service.GetSquad(t.Context(), actor, squadOtherProjectID, squadID); !isAppCode(err, "project_not_found") {
		t.Fatalf("inaccessible project get error = %v", err)
	}
}
