package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runFixture struct {
	project   store.Project
	issue     store.Issue
	workspace store.Workspace
	provider  store.Provider
	model     store.ModelProfile
	runtime   store.Runtime
	profile   store.ExecutorProfile
	agent     store.Agent
	run       store.Run
}

func seedRunFixture(t *testing.T, s *Store, suffix string) runFixture {
	t.Helper()
	ctx := context.Background()
	project, err := s.CreateProject(ctx, store.Project{Name: "project-" + suffix, RepositoryPath: "/repo/" + suffix})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	provider, err := s.CreateProvider(ctx, store.Provider{Name: "provider-" + suffix, Kind: "test", Enabled: true})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	model, err := s.CreateModelProfile(ctx, store.ModelProfile{ProjectID: &project.ID, ProviderID: provider.ID, Name: "model-" + suffix, Model: "test", Enabled: true})
	if err != nil {
		t.Fatalf("create model: %v", err)
	}
	runtime, err := s.CreateRuntime(ctx, store.Runtime{ProjectID: &project.ID, Name: "runtime-" + suffix, Kind: "docker", Image: "test", NetworkPolicy: "restricted", Enabled: true})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	profile, err := s.CreateExecutorProfile(ctx, store.ExecutorProfile{ProjectID: &project.ID, Name: "profile-" + suffix, Engine: "test", ModelProfileID: model.ID, RuntimeID: runtime.ID, Enabled: true})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	agent, err := s.CreateAgent(ctx, store.Agent{ProjectID: &project.ID, Name: "agent-" + suffix, ExecutorProfileID: profile.ID})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	issue, err := s.CreateIssue(ctx, store.Issue{ProjectID: project.ID, Title: "issue " + suffix, Status: "TODO", AssignedAgentID: &agent.ID})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	workspace, err := s.CreateWorkspace(ctx, store.Workspace{ProjectID: project.ID, IssueID: issue.ID, Path: "/workspace/" + suffix, WorkingBranch: "issue/" + suffix})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	run, err := s.CreateRun(ctx, store.Run{ProjectID: project.ID, IssueID: issue.ID, WorkspaceID: workspace.ID, AgentID: &agent.ID, Attempt: 1})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	return runFixture{project, issue, workspace, provider, model, runtime, profile, agent, run}
}

func TestExecutionPersistenceInvariants(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "exec")
	other := seedRunFixture(t, s, "other")

	if got, err := s.GetWorkspaceByIssue(ctx, f.project.ID, f.issue.ID); err != nil || got.ID != f.workspace.ID {
		t.Fatalf("get workspace: got=%+v err=%v", got, err)
	}
	if _, err := s.GetWorkspaceByIssue(ctx, other.project.ID, f.issue.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project workspace error=%v", err)
	}
	if got, err := s.GetRun(ctx, f.project.ID, f.run.ID); err != nil || got.Status != "QUEUED" {
		t.Fatalf("get run: got=%+v err=%v", got, err)
	}
	if _, err := s.GetRun(ctx, other.project.ID, f.run.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project run error=%v", err)
	}

	instance, err := s.CreateRuntimeInstance(ctx, store.RuntimeInstance{
		ProjectID: f.project.ID, WorkspaceID: f.workspace.ID, RuntimeID: f.runtime.ID,
	})
	if err != nil {
		t.Fatalf("create runtime instance: %v", err)
	}
	if instance.Status != "PROVISIONING" || instance.RunnerStatus != "CONNECTING" {
		t.Fatalf("runtime defaults=%+v", instance)
	}
	external := "container-1"
	instance, err = s.UpdateRuntimeInstanceState(ctx, f.project.ID, instance.ID, "RUNNING", &external, "READY", json.RawMessage(`{"safe":true}`))
	if err != nil || instance.WorkspaceID != f.workspace.ID || instance.StartedAt == nil {
		t.Fatalf("update runtime instance: got=%+v err=%v", instance, err)
	}
	if _, err := s.UpdateRuntimeInstanceState(ctx, other.project.ID, instance.ID, "RUNNING", nil, "READY", nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project instance update error=%v", err)
	}

	session, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: f.project.ID, RunID: f.run.ID, RuntimeInstanceID: instance.ID,
	})
	if err != nil || session.Status != "PENDING" || session.CWD != "/workspace" {
		t.Fatalf("create session: got=%+v err=%v", session, err)
	}
	if _, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: f.project.ID, RunID: f.run.ID, RuntimeInstanceID: instance.ID,
	}); err == nil {
		t.Fatal("expected one-active-session constraint")
	}
	if _, err := s.CreateExecutionSession(ctx, store.ExecutionSession{
		ProjectID: other.project.ID, RunID: other.run.ID, RuntimeInstanceID: instance.ID, Status: "COMPLETED",
	}); err == nil {
		t.Fatal("expected cross-workspace session rejection")
	}

	q, err := s.CreateQuestion(ctx, store.Question{
		ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID, Prompt: "choose", Blocking: true,
	})
	if err != nil || q.Kind != "TEXT" || q.Status != "OPEN" {
		t.Fatalf("create question: got=%+v err=%v", q, err)
	}
	if _, err := s.CreateQuestion(ctx, store.Question{
		ProjectID: f.project.ID, IssueID: other.issue.ID, RunID: f.run.ID, Prompt: "bad",
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("mismatched question error=%v", err)
	}
	decision, err := s.CreateDecision(ctx, store.Decision{
		ProjectID: f.project.ID, IssueID: &f.issue.ID, RunID: &f.run.ID, QuestionID: &q.ID,
		Kind: "QUESTION_ANSWER", Outcome: "yes", ActorType: "HUMAN",
	})
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}
	if _, err := s.CreateDecision(ctx, store.Decision{
		ProjectID: other.project.ID, QuestionID: &q.ID, Kind: "QUESTION_ANSWER", Outcome: "no", ActorType: "HUMAN",
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign question decision error=%v", err)
	}
	review, err := s.CreateReview(ctx, store.Review{
		ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID, DecisionID: &decision.ID,
	})
	if err != nil || review.Status != "PENDING" {
		t.Fatalf("create review: got=%+v err=%v", review, err)
	}
	if _, err := s.CreateReview(ctx, store.Review{
		ProjectID: f.project.ID, IssueID: other.issue.ID, RunID: f.run.ID,
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("mismatched review error=%v", err)
	}
}

func TestSchedulerPersistenceIsScopedAndClaimsOnce(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "sched")
	other := seedRunFixture(t, s, "sched-other")

	if _, err := s.EnqueueJob(ctx, store.SchedulerJob{ProjectID: f.project.ID, RunID: f.run.ID}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("empty idempotency error=%v", err)
	}
	job, err := s.EnqueueJob(ctx, store.SchedulerJob{
		ProjectID: f.project.ID, RunID: f.run.ID, IdempotencyKey: "start-run",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	dup, err := s.EnqueueJob(ctx, store.SchedulerJob{
		ProjectID: f.project.ID, RunID: f.run.ID, IdempotencyKey: "start-run",
	})
	if err != nil || dup.ID != job.ID {
		t.Fatalf("idempotent enqueue: got=%+v err=%v", dup, err)
	}
	if _, _, err := s.ClaimNextJob(ctx, "", time.Minute); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank owner error=%v", err)
	}
	if _, _, err := s.ClaimNextJob(ctx, "worker", 0); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("zero lease error=%v", err)
	}
	claimed, lease, err := s.ClaimNextJob(ctx, "worker-1", time.Minute)
	if err != nil || claimed == nil || lease == nil || claimed.ID != job.ID {
		t.Fatalf("claim: job=%+v lease=%+v err=%v", claimed, lease, err)
	}
	if next, nextLease, err := s.ClaimNextJob(ctx, "worker-2", time.Minute); err != nil || next != nil || nextLease != nil {
		t.Fatalf("expected empty queue: job=%+v lease=%+v err=%v", next, nextLease, err)
	}
	if _, err := s.RenewLease(ctx, other.project.ID, job.ID, lease.LeaseToken, time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project renew error=%v", err)
	}
	renewed, err := s.RenewLease(ctx, f.project.ID, job.ID, lease.LeaseToken, 2*time.Minute)
	if err != nil || !renewed.ExpiresAt.After(lease.ExpiresAt) {
		t.Fatalf("renew: got=%+v err=%v", renewed, err)
	}

	if err := s.ReserveCapacity(ctx, f.project.ID, job.ID, f.run.ID, "BAD", f.agent.ID); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("bad capacity kind error=%v", err)
	}
	if err := s.ReserveCapacity(ctx, f.project.ID, job.ID, f.run.ID, "AGENT", other.agent.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign agent reservation error=%v", err)
	}
	for _, tc := range []struct{ kind, id string }{{"AGENT", f.agent.ID}, {"MODEL_PROFILE", f.model.ID}} {
		if err := s.ReserveCapacity(ctx, f.project.ID, job.ID, f.run.ID, tc.kind, tc.id); err != nil {
			t.Fatalf("reserve %s: %v", tc.kind, err)
		}
		if err := s.ReserveCapacity(ctx, f.project.ID, job.ID, f.run.ID, tc.kind, tc.id); err != nil {
			t.Fatalf("idempotent reserve %s: %v", tc.kind, err)
		}
	}
	if err := s.ReleaseCapacity(ctx, other.project.ID, job.ID); err != nil {
		t.Fatalf("foreign release capacity: %v", err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM scheduler_capacity_reservations WHERE project_id=$1 AND job_id=$2`, f.project.ID, job.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("capacity count=%d err=%v", count, err)
	}
	if err := s.ReleaseCapacity(ctx, f.project.ID, job.ID); err != nil {
		t.Fatalf("release capacity: %v", err)
	}

	if err := s.ReleaseLease(ctx, other.project.ID, job.ID, lease.LeaseToken); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project release lease error=%v", err)
	}
	if err := s.ReleaseLease(ctx, f.project.ID, job.ID, lease.LeaseToken); err != nil {
		t.Fatalf("release lease: %v", err)
	}

	second, err := s.EnqueueJob(ctx, store.SchedulerJob{
		ProjectID: f.project.ID, RunID: f.run.ID, Kind: "RESUME", IdempotencyKey: "resume-run",
	})
	if err != nil {
		t.Fatalf("enqueue second: %v", err)
	}
	results := make(chan *store.SchedulerJob, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			j, _, err := s.ClaimNextJob(ctx, "concurrent", time.Minute)
			if err != nil {
				errs <- err
				return
			}
			results <- j
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent claim: %v", err)
	}
	claims := 0
	for j := range results {
		if j != nil {
			claims++
			if j.ID != second.ID {
				t.Fatalf("claimed wrong job %s", j.ID)
			}
		}
	}
	if claims != 1 {
		t.Fatalf("claims=%d, want 1", claims)
	}
}

func TestEvidencePersistenceIsImmutableOrderedAndScoped(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "evidence")
	other := seedRunFixture(t, s, "evidence-other")

	snapshot := json.RawMessage(`{"runtime":{"id":"runtime"}}`)
	if err := s.PutRunProvenance(ctx, f.project.ID, f.run.ID, snapshot); err != nil {
		t.Fatalf("put provenance: %v", err)
	}
	got, err := s.GetRunProvenance(ctx, f.project.ID, f.run.ID)
	if err != nil {
		t.Fatalf("get provenance: %v", err)
	}
	var gotSnapshot, wantSnapshot any
	if err := json.Unmarshal(got, &gotSnapshot); err != nil {
		t.Fatalf("decode stored provenance: %v", err)
	}
	if err := json.Unmarshal(snapshot, &wantSnapshot); err != nil {
		t.Fatalf("decode expected provenance: %v", err)
	}
	if !reflect.DeepEqual(gotSnapshot, wantSnapshot) {
		t.Fatalf("get provenance=%s want=%s", got, snapshot)
	}
	if err := s.PutRunProvenance(ctx, f.project.ID, f.run.ID, json.RawMessage(`{"changed":true}`)); err == nil {
		t.Fatal("expected immutable provenance")
	}
	if _, err := s.GetRunProvenance(ctx, other.project.ID, f.run.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project provenance error=%v", err)
	}

	projectEvent, err := s.AppendEvent(ctx, store.Event{ProjectID: f.project.ID, Type: "project.test"})
	if err != nil || projectEvent.Sequence != nil || projectEvent.SchemaVersion != 1 {
		t.Fatalf("project event=%+v err=%v", projectEvent, err)
	}
	if _, err := s.AppendEvent(ctx, store.Event{ProjectID: f.project.ID, Type: "bad.agent", AgentID: &other.agent.ID}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign agent event error=%v", err)
	}
	foreignParent, err := s.AppendEvent(ctx, store.Event{ProjectID: other.project.ID, Type: "parent"})
	if err != nil {
		t.Fatalf("foreign parent: %v", err)
	}
	if _, err := s.AppendEvent(ctx, store.Event{ProjectID: f.project.ID, Type: "bad.parent", ParentEventID: &foreignParent.ID}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign parent event error=%v", err)
	}

	const n = 6
	seqs := make(chan int64, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ev, err := s.AppendEvent(ctx, store.Event{
				ProjectID: f.project.ID, IssueID: &f.issue.ID, RunID: &f.run.ID,
				AgentID: &f.agent.ID, WorkspaceID: &f.workspace.ID, Type: "agent.message",
				Payload: json.RawMessage(`{"message":"test"}`),
			})
			if err != nil {
				errs <- err
				return
			}
			seqs <- *ev.Sequence
		}()
	}
	wg.Wait()
	close(seqs)
	close(errs)
	for err := range errs {
		t.Fatalf("append event: %v", err)
	}
	gotSeqs := make([]int, 0, n)
	for seq := range seqs {
		gotSeqs = append(gotSeqs, int(seq))
	}
	sort.Ints(gotSeqs)
	for i, seq := range gotSeqs {
		if seq != i+1 {
			t.Fatalf("sequences=%v", gotSeqs)
		}
	}
	events, err := s.ListRunEvents(ctx, f.project.ID, f.run.ID, 0, 0)
	if err != nil || len(events) != n {
		t.Fatalf("list events len=%d err=%v", len(events), err)
	}
	page, err := s.ListRunEvents(ctx, f.project.ID, f.run.ID, 2, 2)
	if err != nil || len(page) != 2 || *page[0].Sequence != 3 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if _, err := s.AppendEvent(ctx, store.Event{ProjectID: other.project.ID, RunID: &f.run.ID, Type: "cross"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project run event error=%v", err)
	}

	chunk, err := s.CreateRawOutputChunk(ctx, store.RawOutputChunk{
		ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID,
		Stream: "STDOUT", Sequence: 1, StorageRef: "raw/1", SizeBytes: 3,
	})
	if err != nil || chunk.RunID != f.run.ID {
		t.Fatalf("raw chunk=%+v err=%v", chunk, err)
	}
	if _, err := s.CreateRawOutputChunk(ctx, store.RawOutputChunk{
		ProjectID: f.project.ID, IssueID: other.issue.ID, RunID: f.run.ID,
		Stream: "STDOUT", Sequence: 2, StorageRef: "raw/bad",
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("mismatched raw error=%v", err)
	}

	artifact, err := s.CreateArtifact(ctx, store.Artifact{
		ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID,
		Name: "patch.diff", Kind: "diff", StorageRef: "artifact/1", SizeBytes: 5,
	})
	if err != nil {
		t.Fatalf("artifact: %v", err)
	}
	if _, err := s.CreateArtifact(ctx, store.Artifact{
		ProjectID: f.project.ID, IssueID: other.issue.ID, RunID: f.run.ID,
		Name: "bad", Kind: "bad", StorageRef: "artifact/bad",
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("mismatched artifact error=%v", err)
	}
	artifacts, err := s.ListArtifacts(ctx, f.project.ID, f.run.ID)
	if err != nil || len(artifacts) != 1 || artifacts[0].ID != artifact.ID {
		t.Fatalf("artifacts=%+v err=%v", artifacts, err)
	}
	foreign, err := s.ListArtifacts(ctx, other.project.ID, f.run.ID)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("cross-project artifacts=%+v err=%v", foreign, err)
	}
}
