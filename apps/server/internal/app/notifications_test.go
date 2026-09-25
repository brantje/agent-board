package app

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type issueCommentNotificationTestStore struct {
	*issueCommentTestStore
	notificationErr   error
	notificationCalls int
	page              store.UserNotificationPage
	markedRead        []struct {
		userID, notificationID string
		read                   bool
	}
	markedAll       int
	subscribed      bool
	setSubscription []bool
}

func (s *issueCommentNotificationTestStore) CreateIssueCommentNotifications(context.Context, string, string) error {
	s.notificationCalls++
	return s.notificationErr
}

func (s *issueCommentNotificationTestStore) ListUserNotifications(context.Context, string) (store.UserNotificationPage, error) {
	return s.page, s.notificationErr
}

func (s *issueCommentNotificationTestStore) SetNotificationRead(_ context.Context, userID, notificationID string, read bool) error {
	s.markedRead = append(s.markedRead, struct {
		userID, notificationID string
		read                   bool
	}{userID, notificationID, read})
	return s.notificationErr
}

func (s *issueCommentNotificationTestStore) MarkAllNotificationsRead(context.Context, string) (int, error) {
	return s.markedAll, s.notificationErr
}

func (s *issueCommentNotificationTestStore) GetIssueSubscription(context.Context, string, string, string) (bool, error) {
	return s.subscribed, s.notificationErr
}

func (s *issueCommentNotificationTestStore) SetIssueSubscription(_ context.Context, _ string, _ string, _ string, subscribed bool) error {
	s.setSubscription = append(s.setSubscription, subscribed)
	return s.notificationErr
}

func TestNotificationServiceDelegatesReadsAndMutations(t *testing.T) {
	base := &projectWorkflowAuthorizationStore{project: store.Project{ID: "project-1"}, issues: map[string]store.Issue{}, runs: map[string]store.Run{}}
	fake := &issueCommentNotificationTestStore{
		issueCommentTestStore: &issueCommentTestStore{projectWorkflowAuthorizationStore: base},
		page:                  store.UserNotificationPage{Notifications: []store.UserNotification{{ID: "notification-1"}}, UnreadCount: 1},
		markedAll:             2,
		subscribed:            true,
	}
	service := New(fake)

	page, err := service.ListNotifications(t.Context(), "user-1")
	if err != nil || len(page.Notifications) != 1 || page.UnreadCount != 1 {
		t.Fatalf("ListNotifications() = %+v, %v", page, err)
	}
	if err := service.SetNotificationRead(t.Context(), "user-1", "notification-1", true); err != nil {
		t.Fatalf("SetNotificationRead() error = %v", err)
	}
	if len(fake.markedRead) != 1 || !fake.markedRead[0].read {
		t.Fatalf("notification read calls = %+v", fake.markedRead)
	}
	if count, err := service.MarkAllNotificationsRead(t.Context(), "user-1"); err != nil || count != 2 {
		t.Fatalf("MarkAllNotificationsRead() = %d, %v", count, err)
	}
	subscribed, err := service.GetIssueSubscription(t.Context(), "project-1", "issue-1", "user-1")
	if err != nil || !subscribed {
		t.Fatalf("GetIssueSubscription() = %v, %v", subscribed, err)
	}
	if err := service.SetIssueSubscription(t.Context(), "project-1", "issue-1", "user-1", false); err != nil {
		t.Fatalf("SetIssueSubscription() error = %v", err)
	}
	if len(fake.setSubscription) != 1 || fake.setSubscription[0] {
		t.Fatalf("subscription calls = %+v", fake.setSubscription)
	}
}

func TestProjectAccessSubscriptionCommandsUseViewerBoundary(t *testing.T) {
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: "project-1"},
		roles:   map[string]string{"user-1": store.ProjectRoleViewer},
		issues:  map[string]store.Issue{},
		runs:    map[string]store.Run{},
	}
	fake := &issueCommentNotificationTestStore{
		issueCommentTestStore: &issueCommentTestStore{projectWorkflowAuthorizationStore: base},
		subscribed:            true,
	}
	control := New(fake)
	access, err := NewProjectAccessService(control, fake)
	if err != nil {
		t.Fatal(err)
	}
	actor := activeProjectActor("user-1", store.DeploymentRoleMember)
	subscribed, err := access.GetIssueSubscription(t.Context(), actor, "project-1", "issue-1")
	if err != nil || !subscribed {
		t.Fatalf("GetIssueSubscription() = %v, %v", subscribed, err)
	}
	if err := access.SetIssueSubscription(t.Context(), actor, "project-1", "issue-1", false); err != nil {
		t.Fatalf("SetIssueSubscription() error = %v", err)
	}
}

func TestNotificationServiceReportsUnavailableAndStoreErrors(t *testing.T) {
	base := &projectWorkflowAuthorizationStore{project: store.Project{ID: "project-1"}, issues: map[string]store.Issue{}, runs: map[string]store.Run{}}
	unavailable := New(&issueCommentTestStore{projectWorkflowAuthorizationStore: base})
	if _, err := unavailable.ListNotifications(t.Context(), "user-1"); err == nil {
		t.Fatal("ListNotifications() unexpectedly succeeded without notification store")
	}
	if err := unavailable.SetNotificationRead(t.Context(), "user-1", "notification-1", true); err == nil {
		t.Fatal("SetNotificationRead() unexpectedly succeeded without notification store")
	}
	if _, err := unavailable.MarkAllNotificationsRead(t.Context(), "user-1"); err == nil {
		t.Fatal("MarkAllNotificationsRead() unexpectedly succeeded without notification store")
	}
	if _, err := unavailable.GetIssueSubscription(t.Context(), "project-1", "issue-1", "user-1"); err == nil {
		t.Fatal("GetIssueSubscription() unexpectedly succeeded without notification store")
	}
	if err := unavailable.SetIssueSubscription(t.Context(), "project-1", "issue-1", "user-1", true); err == nil {
		t.Fatal("SetIssueSubscription() unexpectedly succeeded without notification store")
	}

	failing := &issueCommentNotificationTestStore{
		issueCommentTestStore: &issueCommentTestStore{projectWorkflowAuthorizationStore: base},
		notificationErr:       errors.New("notification store unavailable"),
	}
	service := New(failing)
	if _, err := service.ListNotifications(t.Context(), "user-1"); err == nil {
		t.Fatal("ListNotifications() unexpectedly swallowed store error")
	}
	if err := service.SetNotificationRead(t.Context(), "user-1", "notification-1", true); err == nil {
		t.Fatal("SetNotificationRead() unexpectedly swallowed store error")
	}
	if _, err := service.MarkAllNotificationsRead(t.Context(), "user-1"); err == nil {
		t.Fatal("MarkAllNotificationsRead() unexpectedly swallowed store error")
	}
	if _, err := service.GetIssueSubscription(t.Context(), "project-1", "issue-1", "user-1"); err == nil {
		t.Fatal("GetIssueSubscription() unexpectedly swallowed store error")
	}
	if err := service.SetIssueSubscription(t.Context(), "project-1", "issue-1", "user-1", true); err == nil {
		t.Fatal("SetIssueSubscription() unexpectedly swallowed store error")
	}
}

func TestCommentNotificationFailureDoesNotRollBackComment(t *testing.T) {
	const projectID = "project-1"
	const issueID = "issue-1"
	base := &projectWorkflowAuthorizationStore{
		project: store.Project{ID: projectID, IssuePrefix: "AB"},
		issues: map[string]store.Issue{
			issueID: {ID: issueID, ProjectID: projectID, Number: 1, Title: "Issue", Status: "TODO"},
		},
		runs: map[string]store.Run{},
	}
	failure := errors.New("notification store unavailable")
	fake := &issueCommentNotificationTestStore{
		issueCommentTestStore: &issueCommentTestStore{projectWorkflowAuthorizationStore: base},
		notificationErr:       failure,
	}
	service := New(fake)

	comment, err := service.CreateHumanIssueComment(t.Context(), CreateIssueCommentInput{
		ProjectID: projectID, IssueID: issueID, Body: "The comment is durable", RequestKey: "request-1",
	}, "user-1")
	if err != nil {
		t.Fatalf("CreateHumanIssueComment() error = %v", err)
	}
	if comment.ID == "" || len(fake.comments) != 1 {
		t.Fatalf("comment=%+v persisted=%d", comment, len(fake.comments))
	}
	if fake.notificationCalls != 1 {
		t.Fatalf("notification calls=%d want 1", fake.notificationCalls)
	}
}
