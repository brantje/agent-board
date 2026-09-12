package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type phase3GroupHTTPStore struct {
	*authHTTPStore
	groups  map[string]store.Group
	members map[string]map[string]struct{}
	next    int
}

func newPhase3GroupHTTPStore() *phase3GroupHTTPStore {
	return &phase3GroupHTTPStore{
		authHTTPStore: newAuthHTTPStore(),
		groups:        map[string]store.Group{},
		members:       map[string]map[string]struct{}{},
	}
}

func (s *phase3GroupHTTPStore) ListGroups(context.Context) ([]store.Group, error) {
	result := make([]store.Group, 0, len(s.groups))
	for _, group := range s.groups {
		result = append(result, group)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (s *phase3GroupHTTPStore) CreateGroup(_ context.Context, input store.Group) (store.Group, error) {
	for _, group := range s.groups {
		if group.Name == input.Name {
			return store.Group{}, store.ErrConflict
		}
	}
	s.next++
	input.ID = "00000000-0000-0000-0001-" + leftPad12(s.next)
	input.CreatedAt = time.Unix(int64(s.next), 0).UTC()
	input.UpdatedAt = input.CreatedAt
	s.groups[input.ID] = input
	return input, nil
}

func leftPad12(value int) string {
	result := "000000000000" + string(rune('0'+value))
	return result[len(result)-12:]
}

func (s *phase3GroupHTTPStore) UpdateGroup(_ context.Context, id, name string) (store.Group, error) {
	group, ok := s.groups[id]
	if !ok {
		return store.Group{}, store.ErrNotFound
	}
	for otherID, existing := range s.groups {
		if otherID != id && existing.Name == name {
			return store.Group{}, store.ErrConflict
		}
	}
	group.Name = name
	s.groups[id] = group
	return group, nil
}

func (s *phase3GroupHTTPStore) DeleteGroup(_ context.Context, id string) error {
	if _, ok := s.groups[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.groups, id)
	delete(s.members, id)
	return nil
}

func (s *phase3GroupHTTPStore) ListGroupMembers(_ context.Context, groupID string) ([]store.User, error) {
	if _, ok := s.groups[groupID]; !ok {
		return nil, store.ErrNotFound
	}
	result := make([]store.User, 0, len(s.members[groupID]))
	for userID := range s.members[groupID] {
		result = append(result, s.users[userID])
	}
	return result, nil
}

func (s *phase3GroupHTTPStore) AddGroupMember(_ context.Context, groupID, userID string) error {
	if _, ok := s.groups[groupID]; !ok {
		return store.ErrNotFound
	}
	if s.members[groupID] == nil {
		s.members[groupID] = map[string]struct{}{}
	}
	if _, exists := s.members[groupID][userID]; exists {
		return store.ErrConflict
	}
	s.members[groupID][userID] = struct{}{}
	return nil
}

func (s *phase3GroupHTTPStore) RemoveGroupMember(_ context.Context, groupID, userID string) error {
	if _, ok := s.groups[groupID]; !ok {
		return store.ErrNotFound
	}
	if _, exists := s.members[groupID][userID]; !exists {
		return store.ErrGroupMemberNotFound
	}
	delete(s.members[groupID], userID)
	return nil
}

func newPhase3GroupHTTPHandler(t *testing.T, now *time.Time) (http.Handler, *phase3GroupHTTPStore) {
	t.Helper()
	authStore := newPhase3GroupHTTPStore()
	authService, err := app.NewAuthService(authStore, app.AuthServiceConfig{
		Now:        func() time.Time { return *now },
		Random:     &authHTTPRandom{},
		SigningKey: bytes.Repeat([]byte{23}, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	services := &app.Services{ControlPlane: app.New(&fakeControlPlaneStore{}), Auth: authService}
	return NewRouterWithApplication(services), authStore
}

func TestPhase3GroupHTTPAdminCRUDAndMembership(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, authStore := newPhase3GroupHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + admin.AccessToken}

	created := authHTTPRequest(t, handler, http.MethodPost, "/api/groups", `{"name":" Engineering "}`, headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("create group status=%d body=%s", created.Code, created.Body.String())
	}
	var group groupResponse
	if err := json.Unmarshal(created.Body.Bytes(), &group); err != nil {
		t.Fatal(err)
	}
	if group.Name != "engineering" {
		t.Fatalf("group was not normalized: %+v", group)
	}
	duplicate := authHTTPRequest(t, handler, http.MethodPost, "/api/groups", `{"name":"ENGINEERING"}`, headers)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate group status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}

	updated := authHTTPRequest(t, handler, http.MethodPatch, "/api/groups/"+group.ID, `{"name":"Platform"}`, headers)
	if updated.Code != http.StatusOK {
		t.Fatalf("update group status=%d body=%s", updated.Code, updated.Body.String())
	}
	listed := authHTTPRequest(t, handler, http.MethodGet, "/api/groups", "", headers)
	if listed.Code != http.StatusOK || listed.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("list groups status=%d cache=%q body=%s", listed.Code, listed.Header().Get("Cache-Control"), listed.Body.String())
	}

	disabledID := "00000000-0000-0000-0000-000000000099"
	authStore.users[disabledID] = store.User{ID: disabledID, Username: "disabled", Email: "disabled@example.com", DisplayName: "Disabled", DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusDisabled, AuthVersion: 1}
	added := authHTTPRequest(t, handler, http.MethodPost, "/api/groups/"+group.ID+"/members", `{"userID":"`+disabledID+`"}`, headers)
	if added.Code != http.StatusNoContent {
		t.Fatalf("add disabled member status=%d body=%s", added.Code, added.Body.String())
	}
	duplicateMember := authHTTPRequest(t, handler, http.MethodPost, "/api/groups/"+group.ID+"/members", `{"userID":"`+disabledID+`"}`, headers)
	if duplicateMember.Code != http.StatusConflict {
		t.Fatalf("duplicate member status=%d body=%s", duplicateMember.Code, duplicateMember.Body.String())
	}
	members := authHTTPRequest(t, handler, http.MethodGet, "/api/groups/"+group.ID+"/members", "", headers)
	if members.Code != http.StatusOK || members.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("members status=%d cache=%q body=%s", members.Code, members.Header().Get("Cache-Control"), members.Body.String())
	}
	var memberRows []authUserResponse
	if err := json.Unmarshal(members.Body.Bytes(), &memberRows); err != nil || len(memberRows) != 1 || memberRows[0].Status != store.UserStatusDisabled {
		t.Fatalf("members=%+v err=%v", memberRows, err)
	}

	removed := authHTTPRequest(t, handler, http.MethodDelete, "/api/groups/"+group.ID+"/members/"+disabledID, "", headers)
	if removed.Code != http.StatusNoContent {
		t.Fatalf("remove member status=%d body=%s", removed.Code, removed.Body.String())
	}
	deleted := authHTTPRequest(t, handler, http.MethodDelete, "/api/groups/"+group.ID, "", headers)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete group status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	if _, ok := authStore.users[disabledID]; !ok {
		t.Fatal("group deletion removed user")
	}
}

func TestPhase3GroupHTTPRequiresAuthenticatedDeploymentAdmin(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _ := newPhase3GroupHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)
	adminHeaders := map[string]string{"Authorization": "Bearer " + admin.AccessToken}

	createUser := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/users", `{"username":"member","email":"member@example.com","displayName":"Member"}`, adminHeaders)
	var pending pendingUserResponse
	if err := json.Unmarshal(createUser.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	setup := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/setup/complete", `{"token":"`+pending.SetupToken+`","password":"member-long-password"}`, nil)
	if setup.Code != http.StatusOK {
		t.Fatalf("setup status=%d body=%s", setup.Code, setup.Body.String())
	}
	login := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"member","password":"member-long-password"}`, nil)
	var member authTokensResponse
	if err := json.Unmarshal(login.Body.Bytes(), &member); err != nil {
		t.Fatal(err)
	}

	spoofed := authHTTPRequest(t, handler, http.MethodGet, "/api/groups", "", map[string]string{"X-User-ID": admin.User.ID, "X-Admin": "true"})
	if spoofed.Code != http.StatusUnauthorized {
		t.Fatalf("spoofed identity headers status=%d body=%s", spoofed.Code, spoofed.Body.String())
	}
	memberResponse := authHTTPRequest(t, handler, http.MethodGet, "/api/groups", "", map[string]string{"Authorization": "Bearer " + member.AccessToken})
	if memberResponse.Code != http.StatusForbidden {
		t.Fatalf("deployment member group admin status=%d body=%s", memberResponse.Code, memberResponse.Body.String())
	}

	unknownShape := authHTTPRequest(t, handler, http.MethodPost, "/api/groups", `{"name":"team","role":"admin"}`, adminHeaders)
	if unknownShape.Code != http.StatusBadRequest {
		t.Fatalf("group role shape accepted status=%d body=%s", unknownShape.Code, unknownShape.Body.String())
	}
}
