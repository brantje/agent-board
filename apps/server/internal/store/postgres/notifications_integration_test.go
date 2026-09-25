package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestIssueSubscriptionsAndNotificationsAreScopedIdempotentAndDurable(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	author, err := s.CreateUser(ctx, authUser("notification-author", "notification-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	subscriber, err := s.CreateUser(ctx, authUser("notification-subscriber", "notification-subscriber@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateUser(ctx, authUser("notification-other", "notification-other@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProjectWithAdmin(ctx, testProjectInput("Notifications", "/repo/notifications", "NTF"), author.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []store.User{subscriber, other} {
		if _, err := s.UpsertProjectUserAccess(ctx, store.ProjectUserAccess{ProjectID: project.ID, UserID: user.ID, Role: store.ProjectRoleViewer}); err != nil {
			t.Fatal(err)
		}
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Discuss this", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := s.CreateProvider(ctx, store.Provider{ProjectID: &project.ID, Name: "Notification Provider", Kind: "test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	model, err := s.CreateModelProfile(ctx, store.ModelProfile{ProjectID: &project.ID, ProviderID: provider.ID, Name: "Notification Model", Model: "test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := s.CreateAgent(ctx, store.Agent{ProjectID: &project.ID, Name: "Notification Agent", Engine: "test", ModelProfileID: model.ID, EngineSettings: store.EmptyObject, State: "ENABLED"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := s.CreateWorkspace(ctx, store.Workspace{ProjectID: project.ID, IssueID: issue.ID, Path: "/workspace/notifications", WorkingBranch: "issue/notifications"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateRun(ctx, store.Run{ProjectID: project.ID, IssueID: issue.ID, WorkspaceID: workspace.ID, AgentID: &agent.ID, Attempt: 1})
	if err != nil {
		t.Fatal(err)
	}

	following, err := s.GetIssueSubscription(ctx, project.ID, issue.ID, subscriber.ID)
	if err != nil || following {
		t.Fatalf("initial subscription=%v err=%v", following, err)
	}
	if err := s.SetIssueSubscription(ctx, project.ID, issue.ID, subscriber.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetIssueSubscription(ctx, project.ID, issue.ID, subscriber.ID, true); err != nil {
		t.Fatal(err)
	}
	following, err = s.GetIssueSubscription(ctx, project.ID, issue.ID, subscriber.ID)
	if err != nil || !following {
		t.Fatalf("following=%v err=%v", following, err)
	}

	rootKey := "root-request"
	root, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
		IssueID: issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID, Body: "Root discussion", SourceActionKey: &rootKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	replyKey := "reply-request"
	reply, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
		IssueID: issue.ID, ParentCommentID: &root.Comment.ID, AuthorType: store.ActorTypeHuman,
		AuthorID: other.ID, Body: "A follow-up reply", SourceActionKey: &replyKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateIssueCommentNotifications(ctx, project.ID, reply.Comment.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateIssueCommentNotifications(ctx, project.ID, reply.Comment.ID); err != nil {
		t.Fatal(err)
	}

	authorPage, err := s.ListUserNotifications(ctx, author.ID)
	if err != nil || len(authorPage.Notifications) != 1 || authorPage.UnreadCount != 1 {
		t.Fatalf("author page=%+v err=%v", authorPage, err)
	}
	if authorPage.Notifications[0].Kind != store.NotificationKindCommentReply || authorPage.Notifications[0].SourceCommentID != reply.Comment.ID {
		t.Fatalf("author notification=%+v", authorPage.Notifications[0])
	}
	subscriberPage, err := s.ListUserNotifications(ctx, subscriber.ID)
	if err != nil || len(subscriberPage.Notifications) != 1 || subscriberPage.Notifications[0].Kind != store.NotificationKindIssueComment {
		t.Fatalf("subscriber page=%+v err=%v", subscriberPage, err)
	}
	otherPage, err := s.ListUserNotifications(ctx, other.ID)
	if err != nil || len(otherPage.Notifications) != 0 {
		t.Fatalf("actor page=%+v err=%v", otherPage, err)
	}

	agentKey := "agent-reply-request"
	agentReply, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
		IssueID: issue.ID, ParentCommentID: &root.Comment.ID, AuthorType: store.ActorTypeAgent,
		AuthorID: agent.ID, SourceRunID: &run.ID, Body: "Agent follow-up", SourceActionKey: &agentKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateIssueCommentNotifications(ctx, project.ID, agentReply.Comment.ID); err != nil {
		t.Fatal(err)
	}
	authorPage, err = s.ListUserNotifications(ctx, author.ID)
	if err != nil || len(authorPage.Notifications) != 1 {
		t.Fatalf("agent direct-reply page=%+v err=%v", authorPage, err)
	}
	subscriberPage, err = s.ListUserNotifications(ctx, subscriber.ID)
	if err != nil || len(subscriberPage.Notifications) != 2 || subscriberPage.Notifications[0].Kind != store.NotificationKindIssueComment {
		t.Fatalf("agent subscribed page=%+v err=%v", subscriberPage, err)
	}

	if err := s.SetNotificationRead(ctx, author.ID, authorPage.Notifications[0].ID, true); err != nil {
		t.Fatal(err)
	}
	authorPage, err = s.ListUserNotifications(ctx, author.ID)
	if err != nil || authorPage.UnreadCount != 0 || authorPage.Notifications[0].ReadAt == nil {
		t.Fatalf("read author page=%+v err=%v", authorPage, err)
	}
	if updated, err := s.MarkAllNotificationsRead(ctx, subscriber.ID); err != nil || updated != 2 {
		t.Fatalf("mark all updated=%d err=%v", updated, err)
	}

	if err := s.DeleteProjectUserAccess(ctx, project.ID, subscriber.ID); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListUserNotifications(ctx, subscriber.ID)
	if err != nil || len(page.Notifications) != 0 {
		t.Fatalf("inaccessible page=%+v err=%v", page, err)
	}
	if _, err := s.GetIssueSubscription(ctx, project.ID, issue.ID, subscriber.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("inaccessible subscription err=%v", err)
	}
	if err := s.SetIssueSubscription(ctx, project.ID, issue.ID, subscriber.ID, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("inaccessible unsubscribe err=%v", err)
	}
}
