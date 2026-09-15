package postgres

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func eventOfType(events []store.Event, eventType string) *store.Event {
	for i := range events {
		if events[i].Type == eventType {
			return &events[i]
		}
	}
	return nil
}

func TestGenericAssigneeOwnershipAndEvents(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "assignee")
	ctx := t.Context()
	if _, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, nil, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	target := &store.Assignee{Type: "AGENT", ID: f.agent.ID}
	result, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, json.RawMessage(`{"type":"HUMAN","id":"actor"}`))
	if err != nil {
		t.Fatal(err)
	}
	event := eventOfType(result.Events, "issue.assigned")
	if result.Issue.AssigneeID == nil || *result.Issue.AssigneeID != f.agent.ID || event == nil {
		t.Fatalf("issue=%+v events=%+v", result.Issue, result.Events)
	}
	target.ID = strings.ToUpper(target.ID)
	result, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, store.EmptyObject)
	if err != nil || len(result.Events) != 0 {
		t.Fatalf("no-op events=%+v err=%v", result.Events, err)
	}
	result, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, nil, store.EmptyObject)
	event = eventOfType(result.Events, "issue.assigned")
	if err != nil || result.Issue.AssigneeID != nil || event == nil || string(event.Payload) != `{"assignedTo": null}` {
		t.Fatalf("unassign issue=%+v events=%+v err=%v", result.Issue, result.Events, err)
	}
}

func TestAssigneeEligibilityAndDirectory(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "eligibility")
	ctx := t.Context()
	users := map[string]store.User{}
	for _, name := range []string{"admin", "member", "viewer", "group", "outside", "disabled", "pending", "deployment"} {
		status := store.UserStatusActive
		if name == "disabled" {
			status = store.UserStatusDisabled
		}
		if name == "pending" {
			status = store.UserStatusPending
		}
		input := authUser(name, name+"@example.com", status)
		if name == "deployment" {
			input.DeploymentRole = store.DeploymentRoleAdmin
		}
		u, err := s.CreateUser(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		users[name] = u
	}
	for _, name := range []string{"admin", "member", "viewer", "group", "disabled", "pending"} {
		role := store.ProjectRoleMember
		if name == "admin" {
			role = store.ProjectRoleAdmin
		}
		if name == "viewer" || name == "group" {
			role = store.ProjectRoleViewer
		}
		if _, err := s.UpsertProjectUserAccess(ctx, store.ProjectUserAccess{ProjectID: f.project.ID, UserID: users[name].ID, Role: role}); err != nil {
			t.Fatal(err)
		}
	}
	group, err := s.CreateGroup(ctx, store.Group{Name: "assignees"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AddGroupMember(ctx, group.ID, users["group"].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpsertProjectGroupAccess(ctx, store.ProjectGroupAccess{ProjectID: f.project.ID, GroupID: group.ID, Role: store.ProjectRoleMember}); err != nil {
		t.Fatal(err)
	}
	for name, u := range users {
		target := &store.Assignee{Type: "USER", ID: u.ID, Name: "untrusted name"}
		eligible := name == "admin" || name == "member" || name == "group" || name == "deployment"
		result, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, store.EmptyObject)
		if eligible {
			if err != nil || result.Issue.AssignedTo().Name != u.DisplayName || eventOfType(result.Events, "issue.assigned") == nil {
				t.Fatalf("%s issue=%+v events=%+v err=%v", name, result.Issue, result.Events, err)
			}
		} else {
			if err == nil {
				t.Fatalf("assigned ineligible %s", name)
			}
			_, err = s.CreateIssue(ctx, store.Issue{ProjectID: f.project.ID, Title: "invalid owner", AssigneeType: &target.Type, AssigneeID: &target.ID})
			if err == nil {
				t.Fatalf("created issue with ineligible %s", name)
			}
		}
	}
	// Ownership works in every Board status, including DONE, without moving it.
	for _, status := range []string{"BACKLOG", "TODO", "IN_PROGRESS", "BLOCKED", "REVIEW", "DONE"} {
		if _, err = s.pool.Exec(ctx, `UPDATE issues SET status=$2 WHERE id=$1`, f.issue.ID, status); err != nil {
			t.Fatal(err)
		}
		result, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, nil, store.EmptyObject)
		if err != nil || result.Issue.Status != status {
			t.Fatalf("clear in %s: %+v %v", status, result.Issue, err)
		}
		result, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, &store.Assignee{Type: "USER", ID: users["member"].ID}, store.EmptyObject)
		if err != nil || result.Issue.Status != status {
			t.Fatalf("assign in %s: %+v %v", status, result.Issue, err)
		}
	}
	// Unavailable model/provider must not exclude an otherwise enabled Agent.
	if _, err = s.pool.Exec(ctx, `UPDATE providers SET enabled=false,health_status='UNHEALTHY' WHERE id=$1`, f.provider.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=false WHERE id=$1`, f.model.ID); err != nil {
		t.Fatal(err)
	}
	directory, err := s.ListIssueAssignees(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(directory) != 5 {
		t.Fatalf("directory=%+v", directory)
	}
	target := &store.Assignee{Type: "AGENT", ID: f.agent.ID}
	before, err := s.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, store.EmptyObject)
	if err != nil || result.Issue.Status != before.Status {
		t.Fatalf("unready assignment=%+v err=%v", result.Issue, err)
	}
	runs, err := s.ListRuns(ctx, f.project.ID)
	if err != nil || len(runs) != 1 || runs[0].Status != f.run.Status {
		t.Fatalf("runs changed: %+v %v", runs, err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE agents SET state='DISABLED' WHERE id=$1`, f.agent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, store.EmptyObject); err == nil {
		t.Fatal("disabled agent assigned")
	}
	for _, target := range []*store.Assignee{{Type: "OTHER", ID: f.agent.ID}, {Type: "USER", ID: f.agent.ID}, {Type: "AGENT", ID: users["member"].ID}} {
		if _, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, store.EmptyObject); err == nil {
			t.Fatalf("invalid reference accepted: %+v", target)
		}
	}
	other := seedRunFixture(t, s, "foreign")
	if _, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, &store.Assignee{Type: "AGENT", ID: other.agent.ID}, store.EmptyObject); err == nil {
		t.Fatal("foreign agent assigned")
	}
	if _, err = s.SetIssueAssignee(ctx, other.project.ID, f.issue.ID, nil, store.EmptyObject); err == nil {
		t.Fatal("foreign issue assigned")
	}
	for _, pair := range []struct{ kind, id any }{{nil, f.agent.ID}, {"AGENT", nil}, {"OTHER", f.agent.ID}, {"USER", f.agent.ID}, {"AGENT", users["member"].ID}, {"AGENT", other.agent.ID}} {
		if _, err = s.pool.Exec(ctx, `UPDATE issues SET assignee_type=$2,assignee_id=$3 WHERE id=$1`, f.issue.ID, pair.kind, pair.id); err == nil {
			t.Fatalf("invalid persistence pair accepted: %+v", pair)
		}
	}
}

func TestAssigneeAtomicEventAndConcurrentIdempotency(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "atomic")
	ctx := t.Context()
	if _, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, nil, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	target := &store.Assignee{Type: "AGENT", ID: f.agent.ID}
	const callers = 8
	results := make(chan error, callers)
	for range callers {
		go func() {
			_, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, json.RawMessage(`{"type":"HUMAN","id":"actor"}`))
			results <- err
		}()
	}
	for range callers {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE issue_id=$1 AND payload->'assignedTo'->>'id'=$2`, f.issue.ID, f.agent.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	result, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, store.EmptyObject)
	if err != nil || len(result.Events) != 0 || result.Issue.LastEvent == nil {
		t.Fatalf("no-op=%+v events=%+v err=%v", result.Issue, result.Events, err)
	}
	var payload struct {
		AssignedTo store.Assignee `json:"assignedTo"`
	}
	if err = json.Unmarshal(result.Issue.LastEvent.Payload, &payload); err != nil || payload.AssignedTo.Name != f.agent.Name || string(result.Issue.LastEvent.Actor) != `{"id": "actor", "type": "HUMAN"}` {
		t.Fatalf("persisted event=%+v err=%v", result.Issue.LastEvent, err)
	}
	listed, err := s.ListIssues(ctx, f.project.ID)
	if err != nil || len(listed) != 1 || listed[0].AssignedTo().Name != f.agent.Name {
		t.Fatalf("listing=%+v err=%v", listed, err)
	}
	if _, err = s.pool.Exec(ctx, `CREATE FUNCTION reject_assignment_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'event failure'; END $$; CREATE TRIGGER reject_assignment_event BEFORE INSERT ON events FOR EACH ROW EXECUTE FUNCTION reject_assignment_event()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, nil, store.EmptyObject); err == nil {
		t.Fatal("event failure accepted")
	}
	current, err := s.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil || current.AssignedTo() == nil || current.AssignedTo().ID != f.agent.ID || !current.UpdatedAt.Equal(result.Issue.UpdatedAt) {
		t.Fatalf("mutation escaped rollback: %+v %v", current, err)
	}
}
