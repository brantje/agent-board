package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSquadMembershipOnlyUpdatePreservesLeaderExecution(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "squad-membership-only")
	ctx := t.Context()
	member := createSquadExecutionMember(t, s, f, "squad-membership-member")
	replacement := createSquadExecutionMember(t, s, f, "squad-membership-replacement")
	human := createSquadWorkflowUser(t, s, f.project.ID, "squad-membership-human", store.ProjectRoleAdmin, store.DeploymentRoleMember, store.UserStatusActive)
	role := "Product"

	squad, err := s.CreateSquad(ctx, store.Squad{
		ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID,
		Members: []store.SquadMember{
			{Type: store.SquadMemberTypeAgent, ID: member.ID},
			{Type: store.SquadMemberTypeUser, ID: human.ID, Role: &role},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerType := "SQUAD"
	issue, err := s.CreateIssue(ctx, store.Issue{
		ProjectID: f.project.ID, Title: "membership-only update", Status: "TODO",
		AssigneeType: &ownerType, AssigneeID: &squad.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	assertIssueRunCount(t, s, issue.ID, 1)
	assertIssueAgentRunCount(t, s, issue.ID, f.agent.ID, 1)
	assertIssueSchedulerJobCount(t, s, issue.ID, 1)

	updatedRole := "Advisor"
	squad.Members = []store.SquadMember{
		{Type: store.SquadMemberTypeAgent, ID: replacement.ID},
		{Type: store.SquadMemberTypeUser, ID: human.ID, Role: &updatedRole},
	}
	if _, err := app.New(s).UpdateSquad(ctx, squad); err != nil {
		t.Fatal(err)
	}

	assertIssueRunCount(t, s, issue.ID, 1)
	assertIssueAgentRunCount(t, s, issue.ID, f.agent.ID, 1)
	assertIssueAgentRunCount(t, s, issue.ID, member.ID, 0)
	assertIssueAgentRunCount(t, s, issue.ID, replacement.ID, 0)
	assertIssueUserRunCount(t, s, issue.ID, human.ID, 0)
	assertIssueSchedulerJobCount(t, s, issue.ID, 1)

	persisted, err := s.GetIssue(ctx, f.project.ID, issue.ID)
	if err != nil || persisted.AssignedTo() == nil || persisted.AssignedTo().Type != "SQUAD" || persisted.AssignedTo().ID != squad.ID {
		t.Fatalf("ownership after membership-only update=%+v err=%v", persisted.AssignedTo(), err)
	}
	state, err := s.GetIssueExecutionState(ctx, f.project.ID, issue.ID)
	if err != nil || state.ExecutionAgent == nil || state.ExecutionAgent.ID != f.agent.ID {
		t.Fatalf("execution state=%+v err=%v", state, err)
	}
}

func TestStaleSquadUserDoesNotPreserveAccessOrAffectLeaderExecution(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "squad-stale-user-execution")
	ctx := t.Context()
	human := createSquadWorkflowUser(t, s, f.project.ID, "squad-stale-user", store.ProjectRoleAdmin, store.DeploymentRoleMember, store.UserStatusActive)
	role := "Product"

	squad, err := s.CreateSquad(ctx, store.Squad{
		ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID,
		Members: []store.SquadMember{{Type: store.SquadMemberTypeUser, ID: human.ID, Role: &role}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerType := "SQUAD"
	issue, err := s.CreateIssue(ctx, store.Issue{
		ProjectID: f.project.ID, Title: "stale human member", Status: "BACKLOG",
		AssigneeType: &ownerType, AssigneeID: &squad.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertIssueRunCount(t, s, issue.ID, 0)

	if err := s.DeleteProjectUserAccess(ctx, f.project.ID, human.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EffectiveProjectRole(ctx, f.project.ID, human.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale Squad User still has Project access: %v", err)
	}
	if err := s.ValidateProjectWorkflowUser(ctx, f.project.ID, human.ID); err == nil {
		t.Fatal("stale Squad User still satisfies workflow eligibility")
	}

	persistedSquad, err := s.GetSquad(ctx, f.project.ID, squad.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertSquadMember(t, persistedSquad.Members, store.SquadMemberTypeUser, human.ID, &role)

	issue.Status = "TODO"
	if _, err := s.UpdateIssue(ctx, issue); err != nil {
		t.Fatal(err)
	}
	assertIssueRunCount(t, s, issue.ID, 1)
	assertIssueAgentRunCount(t, s, issue.ID, f.agent.ID, 1)
	assertIssueUserRunCount(t, s, issue.ID, human.ID, 0)
	assertIssueSchedulerJobCount(t, s, issue.ID, 1)

	persistedIssue, err := s.GetIssue(ctx, f.project.ID, issue.ID)
	if err != nil || persistedIssue.AssignedTo() == nil || persistedIssue.AssignedTo().Type != "SQUAD" || persistedIssue.AssignedTo().ID != squad.ID {
		t.Fatalf("ownership after stale User execution=%+v err=%v", persistedIssue.AssignedTo(), err)
	}
	state, err := s.GetIssueExecutionState(ctx, f.project.ID, issue.ID)
	if err != nil || state.ExecutionAgent == nil || state.ExecutionAgent.ID != f.agent.ID {
		t.Fatalf("execution state=%+v err=%v", state, err)
	}
	persistedSquad, err = s.GetSquad(ctx, f.project.ID, squad.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertSquadMember(t, persistedSquad.Members, store.SquadMemberTypeUser, human.ID, &role)
}

func assertIssueSchedulerJobCount(t *testing.T, s *Store, issueID string, want int) {
	t.Helper()
	var got int
	if err := s.pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM scheduler_jobs sj
		JOIN runs r ON r.id = sj.run_id
		WHERE r.issue_id = $1
	`, issueID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("issue %s scheduler job count=%d want=%d", issueID, got, want)
	}
}

func assertIssueUserRunCount(t *testing.T, s *Store, issueID, userID string, want int) {
	t.Helper()
	var got int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM runs WHERE issue_id=$1 AND agent_id::text=$2`, issueID, userID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("issue %s User %s run count=%d want=%d", issueID, userID, got, want)
	}
}
