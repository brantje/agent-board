package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSquadLeaderChangeReconcilesOnlyThatSquad(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "squad-leader-scope")
	ctx := t.Context()
	newLeader := createSquadExecutionMember(t, s, f, "squad-scope-new-leader")

	squad, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	otherSquad, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Other", LeaderAgentID: newLeader.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=false WHERE id=$1`, f.model.ID); err != nil {
		t.Fatal(err)
	}

	squadOwner := "SQUAD"
	agentOwner := "AGENT"
	squadIssue, err := s.CreateIssue(ctx, store.Issue{
		ProjectID: f.project.ID, Title: "owned by changed squad", Status: "TODO",
		AssigneeType: &squadOwner, AssigneeID: &squad.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	directIssue, err := s.CreateIssue(ctx, store.Issue{
		ProjectID: f.project.ID, Title: "owned directly by new leader", Status: "TODO",
		AssigneeType: &agentOwner, AssigneeID: &newLeader.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	otherSquadIssue, err := s.CreateIssue(ctx, store.Issue{
		ProjectID: f.project.ID, Title: "owned by other squad", Status: "TODO",
		AssigneeType: &squadOwner, AssigneeID: &otherSquad.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=true WHERE id=$1`, f.model.ID); err != nil {
		t.Fatal(err)
	}

	service := app.New(s)
	squad.LeaderAgentID = newLeader.ID
	if _, err := service.UpdateSquad(ctx, squad); err != nil {
		t.Fatal(err)
	}

	assertIssueAgentRunCount(t, s, squadIssue.ID, newLeader.ID, 1)
	assertIssueRunCount(t, s, directIssue.ID, 0)
	assertIssueRunCount(t, s, otherSquadIssue.ID, 0)
	persisted, err := s.GetIssue(ctx, f.project.ID, squadIssue.ID)
	if err != nil || persisted.AssignedTo() == nil || persisted.AssignedTo().Type != "SQUAD" || persisted.AssignedTo().ID != squad.ID {
		t.Fatalf("ownership=%+v err=%v", persisted.AssignedTo(), err)
	}
}

func TestSquadNonLeaderUpdateDoesNotReconcileExecution(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "squad-non-leader-update")
	ctx := t.Context()
	member := createSquadExecutionMember(t, s, f, "squad-non-leader-member")
	human := createSquadWorkflowUser(t, s, f.project.ID, "squad-non-leader-human", store.ProjectRoleAdmin, store.DeploymentRoleMember, store.UserStatusActive)
	squad, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=false WHERE id=$1`, f.model.ID); err != nil {
		t.Fatal(err)
	}
	ownerType := "SQUAD"
	issue, err := s.CreateIssue(ctx, store.Issue{
		ProjectID: f.project.ID, Title: "parked until config recovery", Status: "TODO",
		AssigneeType: &ownerType, AssigneeID: &squad.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=true WHERE id=$1`, f.model.ID); err != nil {
		t.Fatal(err)
	}

	role := "Product"
	squad.Name = "Renamed Backend"
	squad.Members = []store.SquadMember{
		{Type: store.SquadMemberTypeAgent, ID: member.ID},
		{Type: store.SquadMemberTypeUser, ID: human.ID, Role: &role},
	}
	if _, err := app.New(s).UpdateSquad(ctx, squad); err != nil {
		t.Fatal(err)
	}
	assertIssueRunCount(t, s, issue.ID, 0)
	persisted, err := s.GetIssue(ctx, f.project.ID, issue.ID)
	if err != nil || persisted.AssignedTo() == nil || persisted.AssignedTo().Type != "SQUAD" || persisted.AssignedTo().ID != squad.ID {
		t.Fatalf("ownership after member-only update=%+v err=%v", persisted.AssignedTo(), err)
	}
}

func TestSquadLeaderChangeKeepsOldRunsAndSuppressesExistingNewLeaderPair(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "squad-leader-existing-pair")
	ctx := t.Context()
	newLeader := createSquadExecutionMember(t, s, f, "squad-existing-pair-new-leader")
	squad, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	ownerType := "SQUAD"
	issue, err := s.CreateIssue(ctx, store.Issue{
		ProjectID: f.project.ID, Title: "leader changes", Status: "TODO",
		AssigneeType: &ownerType, AssigneeID: &squad.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := app.New(s)

	squad.LeaderAgentID = newLeader.ID
	if _, err := service.UpdateSquad(ctx, squad); err != nil {
		t.Fatal(err)
	}
	assertIssueAgentRunCount(t, s, issue.ID, f.agent.ID, 1)
	assertIssueAgentRunCount(t, s, issue.ID, newLeader.ID, 1)

	squad.LeaderAgentID = f.agent.ID
	if _, err := service.UpdateSquad(ctx, squad); err != nil {
		t.Fatal(err)
	}
	squad.LeaderAgentID = newLeader.ID
	if _, err := service.UpdateSquad(ctx, squad); err != nil {
		t.Fatal(err)
	}
	assertIssueRunCount(t, s, issue.ID, 2)
	assertIssueAgentRunCount(t, s, issue.ID, f.agent.ID, 1)
	assertIssueAgentRunCount(t, s, issue.ID, newLeader.ID, 1)

	persisted, err := s.GetIssue(ctx, f.project.ID, issue.ID)
	if err != nil || persisted.AssignedTo() == nil || persisted.AssignedTo().Type != "SQUAD" || persisted.AssignedTo().ID != squad.ID {
		t.Fatalf("ownership=%+v err=%v", persisted.AssignedTo(), err)
	}
}

func TestSquadLeaderChangeUnavailableConfigRecoversThroughGenericPath(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "squad-leader-config-recovery")
	ctx := t.Context()
	disabledModel, err := s.CreateModelProfile(ctx, store.ModelProfile{
		ProjectID: &f.project.ID, ProviderID: f.provider.ID, Name: "disabled-leader-model", Model: "test", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	newLeader, err := s.CreateAgent(ctx, store.Agent{
		ProjectID: &f.project.ID, Name: "disabled-config-leader", Engine: "scripted",
		ModelProfileID: disabledModel.ID, EngineSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	squad, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	ownerType := "SQUAD"
	issue, err := s.CreateIssue(ctx, store.Issue{
		ProjectID: f.project.ID, Title: "recover new leader", Status: "TODO",
		AssigneeType: &ownerType, AssigneeID: &squad.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := app.New(s)

	squad.LeaderAgentID = newLeader.ID
	if _, err := service.UpdateSquad(ctx, squad); err != nil {
		t.Fatal(err)
	}
	assertIssueAgentRunCount(t, s, issue.ID, f.agent.ID, 1)
	assertIssueAgentRunCount(t, s, issue.ID, newLeader.ID, 0)
	persisted, err := s.GetIssue(ctx, f.project.ID, issue.ID)
	if err != nil || persisted.AssignedTo() == nil || persisted.AssignedTo().Type != "SQUAD" || persisted.AssignedTo().ID != squad.ID {
		t.Fatalf("ownership=%+v err=%v", persisted.AssignedTo(), err)
	}

	disabledModel.Enabled = true
	if _, err := service.UpdateModelProfile(ctx, &f.project.ID, disabledModel); err != nil {
		t.Fatal(err)
	}
	assertIssueAgentRunCount(t, s, issue.ID, newLeader.ID, 1)
	assertIssueRunCount(t, s, issue.ID, 2)
}

func assertIssueRunCount(t *testing.T, s *Store, issueID string, want int) {
	t.Helper()
	var got int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM runs WHERE issue_id=$1`, issueID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("issue %s run count=%d want=%d", issueID, got, want)
	}
}

func assertIssueAgentRunCount(t *testing.T, s *Store, issueID, agentID string, want int) {
	t.Helper()
	var got int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM runs WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("issue %s agent %s run count=%d want=%d", issueID, agentID, got, want)
	}
}
