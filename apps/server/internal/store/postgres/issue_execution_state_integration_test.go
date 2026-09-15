package postgres

import (
    "testing"

    "github.com/brantje/agent-board/apps/server/internal/store"
)

func TestIssueExecutionStateUsesCurrentPairAndConfiguration(t *testing.T) {
    s := New(testPool(t))
    ctx := t.Context()
    f := seedRunFixture(t, s, "execution-state")

    if _, err := s.pool.Exec(ctx, `UPDATE issues SET status='BACKLOG' WHERE id=$1`, f.issue.ID); err != nil { t.Fatal(err) }
    state, err := s.GetIssueExecutionState(ctx, f.project.ID, f.issue.ID)
    if err != nil || state.State != store.IssueExecutionBacklog || state.CanStart {
        t.Fatalf("backlog state=%+v err=%v", state, err)
    }

    if _, err := s.pool.Exec(ctx, `UPDATE issues SET status='TODO',assignee_type=NULL,assignee_id=NULL WHERE id=$1`, f.issue.ID); err != nil { t.Fatal(err) }
    state, err = s.GetIssueExecutionState(ctx, f.project.ID, f.issue.ID)
    if err != nil || state.State != store.IssueExecutionNotAgentOwned { t.Fatalf("manual state=%+v err=%v", state, err) }

    other := f.agent
    other.ID = ""
    other.Name = "execution-state-other"
    other, err = s.CreateAgent(ctx, other)
    if err != nil { t.Fatal(err) }
    if _, err := s.pool.Exec(ctx, `UPDATE issues SET assignee_type='AGENT',assignee_id=$2 WHERE id=$1`, f.issue.ID, other.ID); err != nil { t.Fatal(err) }
    // f.run belongs to another Agent and must not suppress the current pair.
    state, err = s.GetIssueExecutionState(ctx, f.project.ID, f.issue.ID)
    if err != nil || state.State != store.IssueExecutionReady || !state.CanStart { t.Fatalf("ready state=%+v err=%v", state, err) }

    if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=false WHERE id=$1`, f.model.ID); err != nil { t.Fatal(err) }
    state, err = s.GetIssueExecutionState(ctx, f.project.ID, f.issue.ID)
    if err != nil || state.State != store.IssueExecutionConfigurationUnavailable || state.CanStart { t.Fatalf("configuration state=%+v err=%v", state, err) }

    if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=true WHERE id=$1`, f.model.ID); err != nil { t.Fatal(err) }
    active, err := s.CreateRun(ctx, store.Run{ProjectID: f.project.ID, IssueID: f.issue.ID, WorkspaceID: f.workspace.ID, AgentID: &other.ID, Attempt: 2})
    if err != nil { t.Fatal(err) }
    if _, err := s.pool.Exec(ctx, `UPDATE runs SET queue_reason=$2 WHERE id=$1`, active.ID, store.SchedulerWaitWorkspace); err != nil { t.Fatal(err) }
    state, err = s.GetIssueExecutionState(ctx, f.project.ID, f.issue.ID)
    if err != nil || state.State != store.IssueExecutionActive || state.CanStart || state.ActiveRun == nil || state.ActiveRun.ID != active.ID || state.ActiveRun.QueueReason == nil || *state.ActiveRun.QueueReason != store.SchedulerWaitWorkspace {
        t.Fatalf("active state=%+v err=%v", state, err)
    }
}
