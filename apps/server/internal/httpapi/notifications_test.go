package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type notificationHTTPStore struct {
	*fakeControlPlaneStore
	page             store.UserNotificationPage
	read             bool
	readNotification string
	markAllCalls     int
	subscribed       bool
	subscriptionCall []bool
	err              error
}

func (s *notificationHTTPStore) CreateIssueCommentNotifications(context.Context, string, string) error {
	return nil
}

func (s *notificationHTTPStore) ListUserNotifications(context.Context, string) (store.UserNotificationPage, error) {
	return s.page, s.err
}

func (s *notificationHTTPStore) SetNotificationRead(_ context.Context, _, notificationID string, read bool) error {
	s.readNotification = notificationID
	s.read = read
	return s.err
}

func (s *notificationHTTPStore) MarkAllNotificationsRead(context.Context, string) (int, error) {
	s.markAllCalls++
	return 3, s.err
}

func (s *notificationHTTPStore) GetIssueSubscription(context.Context, string, string, string) (bool, error) {
	return s.subscribed, s.err
}

func (s *notificationHTTPStore) SetIssueSubscription(_ context.Context, _ string, _ string, _ string, subscribed bool) error {
	s.subscribed = subscribed
	s.subscriptionCall = append(s.subscriptionCall, subscribed)
	return s.err
}

type notificationSubscriptionHTTPStore struct {
	*notificationHTTPStore
	*projectAccessHTTPStore
}

func TestNotificationHTTPHandlersUseAuthenticatedUserAndDTOs(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	authStore := newAuthHTTPStore()
	authService, err := app.NewAuthService(authStore, app.AuthServiceConfig{
		Now:        func() time.Time { return now },
		Random:     &authHTTPRandom{},
		SigningKey: bytes.Repeat([]byte{19}, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	notificationID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	notifications := &notificationHTTPStore{
		fakeControlPlaneStore: &fakeControlPlaneStore{},
		page: store.UserNotificationPage{
			Notifications: []store.UserNotification{{
				ID: notificationID, Kind: store.NotificationKindCommentReply, ProjectID: projectID,
				ProjectName: "Project", IssueID: issueID, IssueKey: issueKey, IssueTitle: "Issue",
				SourceCommentID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", CommentAuthorType: "HUMAN",
				CommentAuthorID: otherID, CommentAuthorName: "Sam", Preview: "Please review", CreatedAt: now,
			}}, UnreadCount: 1,
		},
	}
	handler := NewRouterWithApplication(&app.Services{ControlPlane: app.New(notifications), Auth: authService})
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/api/notifications"},
		{http.MethodPatch, "/api/notifications/" + notificationID},
		{http.MethodPost, "/api/notifications/read-all"},
	} {
		response := authHTTPRequest(t, handler, request.method, request.path, `{}`, nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s %s status=%d body=%s", request.method, request.path, response.Code, response.Body.String())
		}
	}
	registerAuthHTTPUser(t, handler)
	tokens := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + tokens.AccessToken}

	response := authHTTPRequest(t, handler, http.MethodGet, "/api/notifications", "", headers)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Please review") {
		t.Fatalf("list notifications status=%d body=%s", response.Code, response.Body.String())
	}
	var page UserNotificationsDTO
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || len(page.Notifications) != 1 || page.UnreadCount != 1 {
		t.Fatalf("notification DTO = %+v, error=%v", page, err)
	}

	response = authHTTPRequest(t, handler, http.MethodPatch, "/api/notifications/"+notificationID, `{"read":true}`, headers)
	if response.Code != http.StatusNoContent || notifications.readNotification != notificationID || !notifications.read {
		t.Fatalf("set read status=%d body=%s store=%+v", response.Code, response.Body.String(), notifications)
	}
	response = authHTTPRequest(t, handler, http.MethodPatch, "/api/notifications/"+notificationID, `{}`, headers)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing read status=%d body=%s", response.Code, response.Body.String())
	}
	response = authHTTPRequest(t, handler, http.MethodPost, "/api/notifications/read-all", "", headers)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"updated":3`) || notifications.markAllCalls != 1 {
		t.Fatalf("mark all status=%d body=%s calls=%d", response.Code, response.Body.String(), notifications.markAllCalls)
	}
	if response = authHTTPRequest(t, handler, http.MethodPatch, "/api/notifications/not-a-uuid", `{"read":true}`, headers); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid notification ID status=%d body=%s", response.Code, response.Body.String())
	}
	if response = authHTTPRequest(t, handler, http.MethodPatch, "/api/notifications/"+notificationID, "{", headers); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid notification JSON status=%d body=%s", response.Code, response.Body.String())
	}
	notifications.err = errors.New("notification store unavailable")
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/api/notifications"},
		{http.MethodPatch, "/api/notifications/" + notificationID},
		{http.MethodPost, "/api/notifications/read-all"},
	} {
		body := ""
		if request.method == http.MethodPatch {
			body = `{"read":true}`
		}
		if response = authHTTPRequest(t, handler, request.method, request.path, body, headers); response.Code != http.StatusInternalServerError {
			t.Fatalf("store error %s %s status=%d body=%s", request.method, request.path, response.Code, response.Body.String())
		}
	}
}

func TestIssueSubscriptionHTTPHandlersUseViewerProjectAccess(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	authStore := newAuthHTTPStore()
	authService, err := app.NewAuthService(authStore, app.AuthServiceConfig{
		Now:        func() time.Time { return now },
		Random:     &authHTTPRandom{offset: 7},
		SigningKey: bytes.Repeat([]byte{23}, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	notifications := &notificationHTTPStore{fakeControlPlaneStore: &fakeControlPlaneStore{}}
	access := newProjectAccessHTTPStore()
	combined := &notificationSubscriptionHTTPStore{notificationHTTPStore: notifications, projectAccessHTTPStore: access}
	control := app.New(combined)
	projectAccess, err := app.NewProjectAccessService(control, combined)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewRouterWithApplication(&app.Services{ControlPlane: control, Auth: authService, ProjectAccess: projectAccess})
	user := registerAuthHTTPUser(t, handler)
	access.users[user.ID] = store.User{ID: user.ID, DeploymentRole: store.DeploymentRoleMember, Status: store.UserStatusActive}
	access.roles[projectGrantKey(projectID, user.ID)] = store.ProjectRoleViewer
	tokens := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + tokens.AccessToken}
	path := "/api/projects/" + projectID + "/issues/" + issueKey + "/subscription"
	missingPath := "/api/projects/" + projectID + "/issues/" + missingIssueKey + "/subscription"
	response := authHTTPRequest(t, handler, http.MethodGet, missingPath, "", headers)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing issue subscription status=%d body=%s", response.Code, response.Body.String())
	}

	response = authHTTPRequest(t, handler, http.MethodGet, path, "", headers)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"subscribed":false`) {
		t.Fatalf("get subscription status=%d body=%s", response.Code, response.Body.String())
	}
	response = authHTTPRequest(t, handler, http.MethodPut, path, "", headers)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"subscribed":true`) {
		t.Fatalf("put subscription status=%d body=%s", response.Code, response.Body.String())
	}
	response = authHTTPRequest(t, handler, http.MethodDelete, path, "", headers)
	if response.Code != http.StatusNoContent || len(notifications.subscriptionCall) != 2 || notifications.subscriptionCall[0] != true || notifications.subscriptionCall[1] != false {
		t.Fatalf("delete subscription status=%d body=%s calls=%v", response.Code, response.Body.String(), notifications.subscriptionCall)
	}
	notifications.err = errors.New("subscription store unavailable")
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		response = authHTTPRequest(t, handler, method, path, "", headers)
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("subscription store error method=%s status=%d body=%s", method, response.Code, response.Body.String())
		}
	}
	direct := &api{service: control, projectAccess: projectAccess}
	for _, handler := range []func(http.ResponseWriter, *http.Request){direct.getIssueSubscription, direct.setIssueSubscription} {
		routeContext := chi.NewRouteContext()
		routeContext.URLParams.Add("projectID", projectID)
		routeContext.URLParams.Add("issueID", issueKey)
		request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, routeContext))
		response := httptest.NewRecorder()
		handler(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("direct subscription handler status=%d body=%s", response.Code, response.Body.String())
		}
	}
}
