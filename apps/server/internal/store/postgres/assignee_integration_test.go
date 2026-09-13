package postgres

import (
	"encoding/json"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"strings"
	"testing"
)

func TestGenericAssigneeOwnershipAndEvents(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "assignee")
	ctx := t.Context()
	if _, _, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, nil, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	target := &store.Assignee{Type: "AGENT", ID: f.agent.ID}
	issue, event, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, json.RawMessage(`{"type":"HUMAN","id":"actor"}`))
	if err != nil {
		t.Fatal(err)
	}
	if issue.AssigneeID == nil || *issue.AssigneeID != f.agent.ID || event.Type != "issue.assigned" {
		t.Fatalf("issue=%+v event=%+v", issue, event)
	}
	target.ID = strings.ToUpper(target.ID)
	_, event, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, store.EmptyObject)
	if err != nil || event.ID != "" {
		t.Fatalf("no-op event=%+v err=%v", event, err)
	}
	issue, event, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, nil, store.EmptyObject)
	if err != nil || issue.AssigneeID != nil || string(event.Payload) != `{"assignedTo": null}` {
		t.Fatalf("unassign issue=%+v event=%+v err=%v", issue, event, err)
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
		issue, event, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, store.EmptyObject)
		if eligible {
			if err != nil || issue.AssignedTo().Name != u.DisplayName || event.ID == "" {
				t.Fatalf("%s issue=%+v err=%v", name, issue, err)
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
		current, _, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, nil, store.EmptyObject)
		if err != nil || current.Status != status {
			t.Fatalf("clear in %s: %+v %v", status, current, err)
		}
		current, _, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, &store.Assignee{Type: "USER", ID: users["member"].ID}, store.EmptyObject)
		if err != nil || current.Status != status {
			t.Fatalf("assign in %s: %+v %v", status, current, err)
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
	after, _, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, store.EmptyObject)
	if err != nil || after.Status != before.Status {
		t.Fatalf("unready assignment=%+v err=%v", after, err)
	}
	runs, err := s.ListRuns(ctx, f.project.ID)
	if err != nil || len(runs) != 1 || runs[0].Status != f.run.Status {
		t.Fatalf("runs changed: %+v %v", runs, err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE agents SET state='DISABLED' WHERE id=$1`, f.agent.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, store.EmptyObject); err == nil {
		t.Fatal("disabled agent assigned")
	}
	for _, target := range []*store.Assignee{{Type: "OTHER", ID: f.agent.ID}, {Type: "USER", ID: f.agent.ID}, {Type: "AGENT", ID: users["member"].ID}} {
		if _, _, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, store.EmptyObject); err == nil {
			t.Fatalf("invalid reference accepted: %+v", target)
		}
	}
	other := seedRunFixture(t, s, "foreign")
	if _, _, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, &store.Assignee{Type: "AGENT", ID: other.agent.ID}, store.EmptyObject); err == nil {
		t.Fatal("foreign agent assigned")
	}
	if _, _, err = s.SetIssueAssignee(ctx, other.project.ID, f.issue.ID, nil, store.EmptyObject); err == nil {
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
	if _, _, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, nil, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	target := &store.Assignee{Type: "AGENT", ID: f.agent.ID}
	const callers = 8
	results := make(chan error, callers)
	for range callers {
		go func() {
			_, _, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, json.RawMessage(`{"type":"HUMAN","id":"actor"}`))
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
	issue, event, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, target, store.EmptyObject)
	if err != nil || event.ID != "" || issue.LastEvent == nil {
		t.Fatalf("no-op=%+v event=%+v err=%v", issue, event, err)
	}
	var payload struct {
		AssignedTo store.Assignee `json:"assignedTo"`
	}
	if err = json.Unmarshal(issue.LastEvent.Payload, &payload); err != nil || payload.AssignedTo.Name != f.agent.Name || string(issue.LastEvent.Actor) != `{"id": "actor", "type": "HUMAN"}` {
		t.Fatalf("persisted event=%+v err=%v", issue.LastEvent, err)
	}
	listed, err := s.ListIssues(ctx, f.project.ID)
	if err != nil || len(listed) != 1 || listed[0].AssignedTo().Name != f.agent.Name {
		t.Fatalf("listing=%+v err=%v", listed, err)
	}
	if _, err = s.pool.Exec(ctx, `CREATE FUNCTION reject_assignment_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'event failure'; END $$; CREATE TRIGGER reject_assignment_event BEFORE INSERT ON events FOR EACH ROW EXECUTE FUNCTION reject_assignment_event()`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, nil, store.EmptyObject); err == nil {
		t.Fatal("event failure accepted")
	}
	current, err := s.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil || current.AssignedTo() == nil || current.AssignedTo().ID != f.agent.ID || !current.UpdatedAt.Equal(issue.UpdatedAt) {
		t.Fatalf("mutation escaped rollback: %+v %v", current, err)
	}
}
