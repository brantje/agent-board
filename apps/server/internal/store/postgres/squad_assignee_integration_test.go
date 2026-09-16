package postgres

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSquadAssigneeOwnershipDirectoryAndIsolation(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "squad-assignee")
	ctx := t.Context()

	squad, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, &store.Assignee{Type: "SQUAD", ID: squad.ID}, store.EmptyObject)
	if err != nil {
		t.Fatal(err)
	}
	assigned := result.Issue.AssignedTo()
	if assigned == nil || assigned.Type != "SQUAD" || assigned.ID != squad.ID || assigned.Name != squad.Name {
		t.Fatalf("assigned=%+v", assigned)
	}
	if result.Issue.Status != f.issue.Status {
		t.Fatalf("assignment changed status: %s -> %s", f.issue.Status, result.Issue.Status)
	}
	persisted, err := s.GetIssue(ctx, f.project.ID, f.issue.ID)
	if err != nil || persisted.AssignedTo() == nil || persisted.AssignedTo().ID != squad.ID || persisted.AssignedTo().Type != "SQUAD" || persisted.AssignedTo().Name != squad.Name {
		t.Fatalf("persisted=%+v err=%v", persisted, err)
	}
	directory, err := s.ListIssueAssignees(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range directory {
		if candidate.Type == "SQUAD" && candidate.ID == squad.ID && candidate.Name == squad.Name {
			found = true
		}
	}
	if !found {
		t.Fatalf("Squad missing from directory: %+v", directory)
	}
	if err := s.DeleteSquad(ctx, f.project.ID, squad.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("delete referenced Squad error=%v", err)
	}

	other := seedRunFixture(t, s, "squad-assignee-other")
	otherSquad, err := s.CreateSquad(ctx, store.Squad{ProjectID: other.project.ID, Name: "Other", LeaderAgentID: other.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, &store.Assignee{Type: "SQUAD", ID: otherSquad.ID}, store.EmptyObject); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project Squad assignment error=%v", err)
	}

	if _, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, nil, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSquad(ctx, f.project.ID, squad.ID); err != nil {
		t.Fatalf("delete unreferenced Squad: %v", err)
	}
}

func TestSquadAssigneeRequiresUsableLeader(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "squad-disabled-leader")
	ctx := t.Context()
	squad, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE agents SET state='DISABLED' WHERE id=$1`, f.agent.ID); err != nil {
		t.Fatal(err)
	}
	directory, err := s.ListIssueAssignees(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range directory {
		if candidate.Type == "SQUAD" && candidate.ID == squad.ID {
			t.Fatalf("Squad with disabled leader remained eligible: %+v", candidate)
		}
	}
	if _, err := s.SetIssueAssignee(ctx, f.project.ID, f.issue.ID, &store.Assignee{Type: "SQUAD", ID: squad.ID}, store.EmptyObject); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("disabled-leader assignment error=%v", err)
	}
}

func TestSquadAssigneeReassignmentBacklogNoOpAndEventIdentity(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "squad-assignee-lifecycle")
	ctx := t.Context()
	secondLeader := createSquadExecutionMember(t, s, f, "squad-assignee-second-leader")
	squadA, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Squad A", LeaderAgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	squadB, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Squad B", LeaderAgentID: secondLeader.ID})
	if err != nil {
		t.Fatal(err)
	}

	backlog, err := s.CreateIssue(ctx, store.Issue{ProjectID: f.project.ID, Title: "backlog ownership", Status: "BACKLOG"})
	if err != nil {
		t.Fatal(err)
	}
	assignedA, err := s.SetIssueAssignee(ctx, f.project.ID, backlog.ID, &store.Assignee{Type: "SQUAD", ID: squadA.ID}, store.EmptyObject)
	if err != nil {
		t.Fatal(err)
	}
	assertSquadAssignmentEvent(t, assignedA.Events, squadA)
	if assignedA.Issue.Status != "BACKLOG" {
		t.Fatalf("status=%s", assignedA.Issue.Status)
	}
	assertIssueRunCount(t, s, backlog.ID, 0)

	repeatedA, err := s.SetIssueAssignee(ctx, f.project.ID, backlog.ID, &store.Assignee{Type: "SQUAD", ID: squadA.ID}, store.EmptyObject)
	if err != nil {
		t.Fatal(err)
	}
	if len(repeatedA.Events) != 0 {
		t.Fatalf("repeated assignment events=%+v", repeatedA.Events)
	}
	assertIssueRunCount(t, s, backlog.ID, 0)

	assignedB, err := s.SetIssueAssignee(ctx, f.project.ID, backlog.ID, &store.Assignee{Type: "SQUAD", ID: squadB.ID}, store.EmptyObject)
	if err != nil {
		t.Fatal(err)
	}
	assertSquadAssignmentEvent(t, assignedB.Events, squadB)
	owner := assignedB.Issue.AssignedTo()
	if owner == nil || owner.Type != "SQUAD" || owner.ID != squadB.ID {
		t.Fatalf("reassigned owner=%+v", owner)
	}
	assertIssueRunCount(t, s, backlog.ID, 0)

	eligible, err := s.CreateIssue(ctx, store.Issue{ProjectID: f.project.ID, Title: "eligible ownership", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.SetIssueAssignee(ctx, f.project.ID, eligible.ID, &store.Assignee{Type: "SQUAD", ID: squadA.ID}, store.EmptyObject)
	if err != nil {
		t.Fatal(err)
	}
	assertSquadAssignmentEvent(t, first.Events, squadA)
	assertIssueAgentRunCount(t, s, eligible.ID, f.agent.ID, 1)
	repeated, err := s.SetIssueAssignee(ctx, f.project.ID, eligible.ID, &store.Assignee{Type: "SQUAD", ID: squadA.ID}, store.EmptyObject)
	if err != nil {
		t.Fatal(err)
	}
	if len(repeated.Events) != 0 {
		t.Fatalf("repeated eligible assignment events=%+v", repeated.Events)
	}
	assertIssueAgentRunCount(t, s, eligible.ID, f.agent.ID, 1)
}

func assertSquadAssignmentEvent(t *testing.T, events []store.Event, squad store.Squad) {
	t.Helper()
	if len(events) == 0 || events[0].Type != "issue.assigned" {
		t.Fatalf("assignment events=%+v", events)
	}
	var payload struct {
		AssignedTo *store.Assignee `json:"assignedTo"`
	}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.AssignedTo == nil || payload.AssignedTo.Type != "SQUAD" || payload.AssignedTo.ID != squad.ID || payload.AssignedTo.Name != squad.Name {
		t.Fatalf("assignedTo payload=%+v", payload.AssignedTo)
	}
}
