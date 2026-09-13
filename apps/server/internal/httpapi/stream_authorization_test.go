package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type streamProjectAccessStore struct {
	*projectAccessHTTPStore
	mu sync.RWMutex
}

func newStreamProjectAccessStore() *streamProjectAccessStore {
	return &streamProjectAccessStore{projectAccessHTTPStore: newProjectAccessHTTPStore()}
}

func (s *streamProjectAccessStore) EffectiveProjectRole(ctx context.Context, projectID, userID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.projectAccessHTTPStore.EffectiveProjectRole(ctx, projectID, userID)
}

func (s *streamProjectAccessStore) grant(projectID, userID, role string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roles[projectGrantKey(projectID, userID)] = role
}

func (s *streamProjectAccessStore) revoke(projectID, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.roles, projectGrantKey(projectID, userID))
}

func TestProjectEventStreamStopsAfterAuthorizationRevocation(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		revoke func(t *testing.T, fixture *projectAccessHTTPFixture, access *streamProjectAccessStore, user store.User)
	}{
		{
			name: "project access removed",
			revoke: func(_ *testing.T, _ *projectAccessHTTPFixture, access *streamProjectAccessStore, user store.User) {
				access.revoke(projectID, user.ID)
			},
		},
		{
			name: "user disabled",
			revoke: func(t *testing.T, fixture *projectAccessHTTPFixture, _ *streamProjectAccessStore, user store.User) {
				disableStreamUser(t, fixture, user.ID)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newProjectAccessHTTPFixture(t)
			user, token := fixture.createUser(t, "project-stream-user", store.DeploymentRoleMember)
			access := newStreamProjectAccessStore()
			access.grant(projectID, user.ID, store.ProjectRoleViewer)

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

			testCase.revoke(t, fixture, access, user)
			live := store.Event{
				ID: "88888888-aaaa-4aaa-8aaa-888888888888", SchemaVersion: 1, Type: "issue.updated",
				ProjectID: projectID, Actor: store.EmptyObject, Payload: store.EmptyObject,
			}
			if err := hub.Publish(context.Background(), live); err != nil {
				t.Fatal(err)
			}
			assertRevokedStreamEndsWithoutEvent(t, resp, cancel, live.ID)
		})
	}
}

func TestRunEventStreamStopsAfterAuthorizationRevocation(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		revoke func(t *testing.T, fixture *projectAccessHTTPFixture, access *streamProjectAccessStore, user store.User)
	}{
		{
			name: "project access removed",
			revoke: func(_ *testing.T, _ *projectAccessHTTPFixture, access *streamProjectAccessStore, user store.User) {
				access.revoke(projectID, user.ID)
			},
		},
		{
			name: "user disabled",
			revoke: func(t *testing.T, fixture *projectAccessHTTPFixture, _ *streamProjectAccessStore, user store.User) {
				disableStreamUser(t, fixture, user.ID)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newProjectAccessHTTPFixture(t)
			user, token := fixture.createUser(t, "run-stream-user", store.DeploymentRoleMember)
			access := newStreamProjectAccessStore()
			access.grant(projectID, user.ID, store.ProjectRoleViewer)

			hub := evidence.NewHub()
			blobs, err := evidence.NewFileBlobStore(t.TempDir(), 1024)
			if err != nil {
				t.Fatal(err)
			}
			evidenceService, err := app.NewRunEvidenceService(&sseEvidenceStore{}, blobs)
			if err != nil {
				t.Fatal(err)
			}
			controlPlane := app.New(&fakeControlPlaneStore{})
			accessService, err := app.NewProjectAccessService(controlPlane, access)
			if err != nil {
				t.Fatal(err)
			}
			router := NewRouterWithApplication(&app.Services{
				ControlPlane:  controlPlane,
				Auth:          fixture.auth,
				ProjectAccess: accessService,
				RunEvidence:   evidenceService,
				EventHub:      hub,
			})
			server := httptest.NewServer(router)
			t.Cleanup(server.Close)

			resp, cancel := openAuthorizedEventStream(t, server, "/api/projects/"+projectID+"/runs/"+runID+"/events", token)
			defer cancel()
			defer resp.Body.Close()

			testCase.revoke(t, fixture, access, user)
			runRef := runID
			sequence := int64(1)
			live := store.Event{
				ID: "99999999-aaaa-4aaa-8aaa-999999999999", SchemaVersion: 1, Type: "agent.message",
				ProjectID: projectID, RunID: &runRef, Sequence: &sequence, Actor: store.EmptyObject, Payload: store.EmptyObject,
			}
			if err := hub.Publish(context.Background(), live); err != nil {
				t.Fatal(err)
			}
			assertRevokedStreamEndsWithoutEvent(t, resp, cancel, live.ID)
		})
	}
}

func disableStreamUser(t *testing.T, fixture *projectAccessHTTPFixture, userID string) {
	t.Helper()
	_, adminToken := fixture.createUser(t, "stream-admin", store.DeploymentRoleAdmin)
	admin, err := fixture.auth.AuthenticateNormalAccess(t.Context(), adminToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.auth.AdminSetDisabled(t.Context(), admin, userID, true); err != nil {
		t.Fatal(err)
	}
}

func openAuthorizedEventStream(t *testing.T, server *httptest.Server, path, token string) (*http.Response, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+path, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		t.Fatalf("stream status=%d body=%s", resp.StatusCode, body)
	}
	return resp, cancel
}

func assertRevokedStreamEndsWithoutEvent(t *testing.T, resp *http.Response, cancel context.CancelFunc, eventID string) {
	t.Helper()
	result := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			errCh <- err
			return
		}
		result <- body
	}()
	select {
	case body := <-result:
		if strings.Contains(string(body), eventID) {
			t.Fatalf("revoked stream received protected event %s: %s", eventID, body)
		}
	case err := <-errCh:
		t.Fatalf("read revoked stream: %v", err)
	case <-time.After(time.Second):
		cancel()
		t.Fatal("revoked stream remained open after a later event")
	}
}
