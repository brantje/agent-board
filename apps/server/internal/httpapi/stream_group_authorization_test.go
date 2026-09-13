package httpapi

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type groupStreamProjectAccessStore struct {
	*projectAccessHTTPStore
	mu      sync.RWMutex
	members map[string]map[string]struct{}
}

func newGroupStreamProjectAccessStore() *groupStreamProjectAccessStore {
	return &groupStreamProjectAccessStore{
		projectAccessHTTPStore: newProjectAccessHTTPStore(),
		members:                map[string]map[string]struct{}{},
	}
}

func (s *groupStreamProjectAccessStore) EffectiveProjectRole(_ context.Context, requestedProjectID, userID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if role, ok := s.roles[projectGrantKey(requestedProjectID, userID)]; ok {
		return role, nil
	}
	for _, grant := range s.groupGrants {
		if grant.ProjectID != requestedProjectID {
			continue
		}
		if _, ok := s.members[grant.GroupID][userID]; ok {
			return grant.Role, nil
		}
	}
	return "", store.ErrNotFound
}

func (s *groupStreamProjectAccessStore) grantGroup(projectID, groupID, userID, role string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.groups[groupID] = store.Group{ID: groupID, Name: "stream-group"}
	s.groupGrants[projectGrantKey(projectID, groupID)] = store.ProjectGroupAccess{
		ProjectID: projectID,
		GroupID:   groupID,
		Role:      role,
	}
	if s.members[groupID] == nil {
		s.members[groupID] = map[string]struct{}{}
	}
	s.members[groupID][userID] = struct{}{}
}

func (s *groupStreamProjectAccessStore) removeGroupMember(groupID, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.members[groupID], userID)
}

func (s *groupStreamProjectAccessStore) removeGroupGrant(projectID, groupID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.groupGrants, projectGrantKey(projectID, groupID))
}

func TestProjectEventStreamStopsAfterGroupDerivedAuthorizationRevocation(t *testing.T) {
	const groupID = "00000000-0000-0000-0000-000000000777"

	for _, testCase := range []struct {
		name   string
		revoke func(*groupStreamProjectAccessStore, string)
	}{
		{
			name: "group member removed",
			revoke: func(access *groupStreamProjectAccessStore, userID string) {
				access.removeGroupMember(groupID, userID)
			},
		},
		{
			name: "group project grant removed",
			revoke: func(access *groupStreamProjectAccessStore, _ string) {
				access.removeGroupGrant(projectID, groupID)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newProjectAccessHTTPFixture(t)
			user, token := fixture.createUser(t, "group-stream-user", store.DeploymentRoleMember)
			access := newGroupStreamProjectAccessStore()
			access.grantGroup(projectID, groupID, user.ID, store.ProjectRoleViewer)

			hub := evidence.NewHub()
			controlPlane := app.New(&projectEventStore{hub: hub})
			accessService, err := app.NewProjectAccessService(controlPlane, access)
			if err != nil {
				t.Fatal(err)
			}
			router := NewRouterWithApplication(&app.Services{
				ControlPlane:  controlPlane,
				Auth:          fixture.auth,
				ProjectAccess: accessService,
				EventHub:      hub,
			})
			server := httptest.NewServer(router)
			t.Cleanup(server.Close)

			resp, cancel := openAuthorizedEventStream(t, server, "/api/projects/"+projectID+"/events", token)
			defer cancel()
			defer resp.Body.Close()

			testCase.revoke(access, user.ID)
			live := store.Event{
				ID:            "77777777-aaaa-4aaa-8aaa-777777777777",
				SchemaVersion: 1,
				Type:          "issue.updated",
				ProjectID:     projectID,
				Actor:         store.EmptyObject,
				Payload:       store.EmptyObject,
			}
			if err := hub.Publish(context.Background(), live); err != nil {
				t.Fatal(err)
			}
			assertRevokedStreamEndsWithoutEvent(t, resp, cancel, live.ID)
		})
	}
}
