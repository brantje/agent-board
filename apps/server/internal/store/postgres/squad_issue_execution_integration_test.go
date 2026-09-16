package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func createSquadExecutionMember(t *testing.T, s *Store, f runFixture, name string) store.Agent {
	t.Helper()
	agent, err := s.CreateAgent(t.Context(), store.Agent{
		ProjectID:      &f.project.ID,
		Name:           name,
		Engine:         "scripted",
		ModelProfileID: f.model.ID,
		EngineSettings: store.EmptyObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func TestSquadIssueAutoEnqueueUsesLeaderAndNormalScheduler(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "squad-auto")
	member := createSquadExecutionMember(t, s, f, "squad-auto-member")
	squad, err := s.CreateSquad(t.Context(), store.Squad{
		ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID,
		Members: []store.SquadMember{{AgentID: member.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerType := "SQUAD"
	issue, err := s.CreateIssue(t.Context(), store.Issue{
		ProjectID: f.project.ID, Title: "Squad TODO", Status: "TODO",
		AssigneeType: &ownerType, AssigneeID: &squad.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := s.GetIssue(t.Context(), f.project.ID, issue.ID)
	if err != nil || persisted.AssignedTo() == nil || persisted.AssignedTo().Type != "SQUAD" || persisted.AssignedTo().ID != squad.ID {
		t.Fatalf("persisted=%+v err=%v", persisted, err)
	}
	var runID, agentID, status string
	if err := s.pool.QueryRow(t.Context(), `SELECT id::text,agent_id::text,status FROM runs WHERE issue_id=$1`, issue.ID).Scan(&runID, &agentID, &status); err != nil {
		t.Fatal(err)
	}
	if agentID != f.agent.ID || status != "QUEUED" {
		t.Fatalf("run agent=%s status=%s", agentID, status)
	}
	var jobs int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM scheduler_jobs WHERE run_id=$1`, runID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("scheduler jobs=%d err=%v", jobs, err)
	}
	var memberRuns int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM runs WHERE issue_id=$1 AND agent_id=$2`, issue.ID, member.ID).Scan(&memberRuns); err != nil || memberRuns != 0 {
		t.Fatalf("member runs=%d err=%v", memberRuns, err)
	}

	backlog, err := s.CreateIssue(t.Context(), store.Issue{
		ProjectID: f.project.ID, Title: "Squad backlog", Status: "BACKLOG",
		AssigneeType: &ownerType, AssigneeID: &squad.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var backlogRuns int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM runs WHERE issue_id=$1`, backlog.ID).Scan(&backlogRuns); err != nil || backlogRuns != 0 {
		t.Fatalf("backlog runs=%d err=%v", backlogRuns, err)
	}
	backlog.Status = "TODO"
	if _, err := s.UpdateIssue(t.Context(), backlog); err != nil {
		t.Fatal(err)
	}
	var backlogAgent string
	if err := s.pool.QueryRow(t.Context(), `SELECT agent_id::text FROM runs WHERE issue_id=$1`, backlog.ID).Scan(&backlogAgent); err != nil || backlogAgent != f.agent.ID {
		t.Fatalf("backlog resolved agent=%q err=%v", backlogAgent, err)
	}
}

func TestSquadIssueExecutionRecoveryAndExplicitStartUseCurrentLeader(t *testing.T) {
	s := New(testPool(t))
	f := seedRunFixture(t, s, "squad-recovery")
	squad, err := s.CreateSquad(t.Context(), store.Squad{ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(t.Context(), `UPDATE model_profiles SET enabled=false WHERE id=$1`, f.model.ID); err != nil {
		t.Fatal(err)
	}
	issue, err := s.CreateIssue(t.Context(), store.Issue{ProjectID: f.project.ID, Title: "Explicit", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetIssueAssignee(t.Context(), f.project.ID, issue.ID, &store.Assignee{Type: "SQUAD", ID: squad.ID}, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM runs WHERE issue_id=$1`, issue.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unready runs=%d err=%v", count, err)
	}
	state, err := s.GetIssueExecutionState(t.Context(), f.project.ID, issue.ID)
	if err != nil || state.State != store.IssueExecutionConfigurationUnavailable || state.ExecutionAgent == nil || state.ExecutionAgent.ID != f.agent.ID || state.ExecutionAgent.Name != f.agent.Name {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	if _, err := s.pool.Exec(t.Context(), `UPDATE model_profiles SET enabled=true WHERE id=$1`, f.model.ID); err != nil {
		t.Fatal(err)
	}
	runValue, _, err := s.StartIssueRun(t.Context(), f.project.ID, issue.ID)
	if err != nil || runValue.AgentID == nil || *runValue.AgentID != f.agent.ID {
		t.Fatalf("explicit run=%+v err=%v", runValue, err)
	}

	if _, err := s.pool.Exec(t.Context(), `UPDATE model_profiles SET enabled=false WHERE id=$1`, f.model.ID); err != nil {
		t.Fatal(err)
	}
	recoveryIssue, err := s.CreateIssue(t.Context(), store.Issue{ProjectID: f.project.ID, Title: "Recovery", Status: "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetIssueAssignee(t.Context(), f.project.ID, recoveryIssue.ID, &store.Assignee{Type: "SQUAD", ID: squad.ID}, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(t.Context(), `UPDATE model_profiles SET enabled=true WHERE id=$1`, f.model.ID); err != nil {
		t.Fatal(err)
	}
	events, err := s.ReconcileIssueExecution(t.Context(), store.IssueExecutionFilter{AgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "run.created" {
		t.Fatalf("events=%+v", events)
	}
	var recoveredAgent string
	if err := s.pool.QueryRow(t.Context(), `SELECT agent_id::text FROM runs WHERE issue_id=$1`, recoveryIssue.ID).Scan(&recoveredAgent); err != nil || recoveredAgent != f.agent.ID {
		t.Fatalf("recovered agent=%q err=%v", recoveredAgent, err)
	}
	recovered, err := s.GetIssue(t.Context(), f.project.ID, recoveryIssue.ID)
	if err != nil || recovered.AssignedTo() == nil || recovered.AssignedTo().Type != "SQUAD" || recovered.AssignedTo().ID != squad.ID {
		t.Fatalf("ownership=%+v err=%v", recovered.AssignedTo(), err)
	}
}
