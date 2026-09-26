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

	// A direct reply must win over a generic subscription for the same recipient/source pair.
	if err := s.SetIssueSubscription(ctx, project.ID, issue.ID, author.ID, true); err != nil {
		t.Fatal(err)
	}

	agentKey := "agent-reply-request"
	agentReply, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
		IssueID: issue.ID, ParentCommentID: &root.Comment.ID, AuthorType: store.ActorTypeAgent,
		AuthorID: agent.ID, SourceRunID: &run.ID, Body: "Agent follow-up", SourceActionKey: &agentKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	issueBeforeNotifications, err := s.GetIssue(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	runsBeforeNotifications, err := s.ListRuns(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateIssueCommentNotifications(ctx, project.ID, agentReply.Comment.ID); err != nil {
		t.Fatal(err)
	}
	issueAfterNotifications, err := s.GetIssue(ctx, project.ID, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	runsAfterNotifications, err := s.ListRuns(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if issueAfterNotifications.Status != issueBeforeNotifications.Status ||
		(issueAfterNotifications.AssigneeType == nil) != (issueBeforeNotifications.AssigneeType == nil) ||
		(issueAfterNotifications.AssigneeID == nil) != (issueBeforeNotifications.AssigneeID == nil) {
		t.Fatalf("notification generation mutated issue before=%+v after=%+v", issueBeforeNotifications, issueAfterNotifications)
	}
	if issueBeforeNotifications.AssigneeType != nil && *issueAfterNotifications.AssigneeType != *issueBeforeNotifications.AssigneeType {
		t.Fatalf("notification generation changed assignee type before=%q after=%q", *issueBeforeNotifications.AssigneeType, *issueAfterNotifications.AssigneeType)
	}
	if issueBeforeNotifications.AssigneeID != nil && *issueAfterNotifications.AssigneeID != *issueBeforeNotifications.AssigneeID {
		t.Fatalf("notification generation changed assignee id before=%q after=%q", *issueBeforeNotifications.AssigneeID, *issueAfterNotifications.AssigneeID)
	}
	if len(runsAfterNotifications) != len(runsBeforeNotifications) {
		t.Fatalf("notification generation changed run count before=%d after=%d", len(runsBeforeNotifications), len(runsAfterNotifications))
	}
	for i := range runsBeforeNotifications {
		if runsAfterNotifications[i].ID != runsBeforeNotifications[i].ID || runsAfterNotifications[i].Status != runsBeforeNotifications[i].Status {
			t.Fatalf("notification generation changed run state before=%+v after=%+v", runsBeforeNotifications, runsAfterNotifications)
		}
	}
	authorPage, err = s.ListUserNotifications(ctx, author.ID)
	if err != nil || len(authorPage.Notifications) != 2 || authorPage.UnreadCount != 2 {
		t.Fatalf("agent direct-reply page=%+v err=%v", authorPage, err)
	}
	agentNotificationID := ""
	for _, notification := range authorPage.Notifications {
		if notification.SourceCommentID == agentReply.Comment.ID {
			if notification.Kind != store.NotificationKindCommentReply {
				t.Fatalf("agent direct-reply notification=%+v", notification)
			}
			agentNotificationID = notification.ID
		}
	}
	if agentNotificationID == "" {
		t.Fatalf("agent direct-reply notification missing from %+v", authorPage)
	}
	subscriberPage, err = s.ListUserNotifications(ctx, subscriber.ID)
	if err != nil || len(subscriberPage.Notifications) != 2 || subscriberPage.Notifications[0].Kind != store.NotificationKindIssueComment || subscriberPage.Notifications[0].SourceCommentID != agentReply.Comment.ID {
		t.Fatalf("agent subscribed page=%+v err=%v", subscriberPage, err)
	}

	if err := s.SetNotificationRead(ctx, author.ID, agentNotificationID, true); err != nil {
		t.Fatal(err)
	}
	authorPage, err = s.ListUserNotifications(ctx, author.ID)
	if err != nil || authorPage.UnreadCount != 1 {
		t.Fatalf("read author page=%+v err=%v", authorPage, err)
	}
	for _, notification := range authorPage.Notifications {
		if notification.ID == agentNotificationID && notification.ReadAt == nil {
			t.Fatalf("read agent notification=%+v", notification)
		}
	}
	if err := s.SetNotificationRead(ctx, author.ID, agentNotificationID, false); err != nil {
		t.Fatal(err)
	}
	authorPage, err = s.ListUserNotifications(ctx, author.ID)
	if err != nil || authorPage.UnreadCount != 2 {
		t.Fatalf("unread author page=%+v err=%v", authorPage, err)
	}
	for _, notification := range authorPage.Notifications {
		if notification.ID == agentNotificationID && notification.ReadAt != nil {
			t.Fatalf("unread agent notification=%+v", notification)
		}
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

	projectB, err := s.CreateProjectWithAdmin(ctx, testProjectInput("Other Notifications", "/repo/notifications-b", "NTB"), other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertProjectUserAccess(ctx, store.ProjectUserAccess{ProjectID: projectB.ID, UserID: author.ID, Role: store.ProjectRoleViewer}); err != nil {
		t.Fatal(err)
	}
	issueB, err := s.CreateIssue(ctx, store.Issue{ProjectID: projectB.ID, Title: "Private discussion", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetIssueSubscription(ctx, projectB.ID, issueB.ID, author.ID, true); err != nil {
		t.Fatal(err)
	}
	projectBKey := "project-b-comment"
	projectBComment, err := s.CreateIssueComment(ctx, projectB.ID, store.IssueComment{
		IssueID: issueB.ID, AuthorType: store.ActorTypeHuman, AuthorID: other.ID, Body: "Project B only", SourceActionKey: &projectBKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateIssueCommentNotifications(ctx, projectB.ID, projectBComment.Comment.ID); err != nil {
		t.Fatal(err)
	}
	authorPage, err = s.ListUserNotifications(ctx, author.ID)
	if err != nil {
		t.Fatal(err)
	}
	projectBNotificationID := ""
	for _, notification := range authorPage.Notifications {
		if notification.ProjectID == projectB.ID {
			projectBNotificationID = notification.ID
		}
	}
	if projectBNotificationID == "" {
		t.Fatalf("project B notification missing before access removal: %+v", authorPage)
	}
	if err := s.DeleteProjectUserAccess(ctx, projectB.ID, author.ID); err != nil {
		t.Fatal(err)
	}
	authorPage, err = s.ListUserNotifications(ctx, author.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, notification := range authorPage.Notifications {
		if notification.ProjectID == projectB.ID {
			t.Fatalf("inaccessible project B notification leaked: %+v", notification)
		}
	}
	if err := s.SetNotificationRead(ctx, author.ID, projectBNotificationID, true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("inaccessible project B notification mutation err=%v", err)
	}
	if err := s.SetNotificationRead(ctx, author.ID, agentNotificationID, true); err != nil {
		t.Fatalf("project A notification should remain mutable: %v", err)
	}
}

func TestUserNotificationsAreDeterministicallyOrdered(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	recipient, err := s.CreateUser(ctx, authUser("notification-order-recipient", "notification-order-recipient@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	author, err := s.CreateUser(ctx, authUser("notification-order-author", "notification-order-author@example.com", store.UserStatusActive))
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProjectWithAdmin(ctx, testProjectInput("Notification order", "/repo/notification-order", "NTO"), recipient.ID)
	if err != nil {
		t.Fatal(err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "Order notifications", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}

	comments := make([]store.IssueComment, 0, 3)
	for i, key := range []string{"order-comment-1", "order-comment-2", "order-comment-3"} {
		created, err := s.CreateIssueComment(ctx, project.ID, store.IssueComment{
			IssueID: issue.ID, AuthorType: store.ActorTypeHuman, AuthorID: author.ID, Body: key, SourceActionKey: &key,
		})
		if err != nil {
			t.Fatalf("create ordering comment %d: %v", i+1, err)
		}
		comments = append(comments, created.Comment)
	}

	ids := []string{
		"10000000-0000-4000-8000-000000000001",
		"10000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000001",
	}
	createdAt := []string{
		"2099-01-01T00:00:00Z",
		"2099-01-01T00:00:00Z",
		"2099-01-02T00:00:00Z",
	}
	for i := range comments {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO user_notifications (id, recipient_user_id, project_id, issue_id, source_comment_id, kind, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7::timestamptz)
		`, ids[i], recipient.ID, project.ID, issue.ID, comments[i].ID, store.NotificationKindIssueComment, createdAt[i]); err != nil {
			t.Fatalf("insert ordering notification %d: %v", i+1, err)
		}
	}

	page, err := s.ListUserNotifications(ctx, recipient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Notifications) != 3 {
		t.Fatalf("ordered notification page=%+v", page)
	}
	want := []string{ids[2], ids[1], ids[0]}
	for i, notification := range page.Notifications {
		if notification.ID != want[i] {
			t.Fatalf("notification[%d]=%s want=%s page=%+v", i, notification.ID, want[i], page)
		}
	}
}
