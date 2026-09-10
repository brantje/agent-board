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
	agent     store.Agent
	run       store.Run
}

func seedRunFixture(t *testing.T, s *Store, suffix string) runFixture {
	t.Helper()
	if s.runnerCandidates == nil {
		rows, err := s.pool.Query(context.Background(), `INSERT INTO runners(name,token_hash,registered_at) SELECT 'fixture-'||gen_random_uuid()::text,decode(repeat('00',32),'hex'),now() FROM generate_series(1,16) RETURNING id::text`)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		s.SetRunnerCandidates(func(string) []string { return append([]string(nil), ids...) })
	}

	ctx := context.Background()
	project, err := s.CreateProject(ctx, testProjectInput("project-"+suffix, "/repo/"+suffix, ""))
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
	agent, err := s.CreateAgent(ctx, store.Agent{ProjectID: &project.ID, Name: "agent-" + suffix, Engine: "test", ModelProfileID: model.ID, EngineSettings: store.EmptyObject})
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
	return runFixture{project, issue, workspace, provider, model, runtime, agent, run}
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

	session, err := s.CreateExecutionSession(ctx, store.ExecutionSession{ProjectID: f.project.ID, RunID: f.run.ID, RuntimeInstanceID: &instance.ID, Command: []string{"echo", "hello"}})
	if err != nil || session.RuntimeInstanceID == nil || *session.RuntimeInstanceID != instance.ID {
		t.Fatalf("create session: %+v %v", session, err)
	}
	if _, err = s.UpdateExecutionSessionState(ctx, other.project.ID, session.ID, "RUNNING", nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project session update error=%v", err)
	}
	if _, err = s.UpdateExecutionSessionState(ctx, f.project.ID, session.ID, "RUNNING", nil); err != nil {
		t.Fatal(err)
	}
	zero := 0
	if _, err = s.UpdateExecutionSessionState(ctx, f.project.ID, session.ID, "COMPLETED", &zero); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerPersistenceIsScopedAndClaimsOnce(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "scheduler")
	other := seedRunFixture(t, s, "scheduler-other")
	job, err := s.EnqueueSchedulerJob(ctx, store.SchedulerJob{ProjectID: f.project.ID, RunID: f.run.ID, Kind: "START", IdempotencyKey: "scheduler-start"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSchedulerJob(ctx, other.project.ID, job.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project job error=%v", err)
	}
	first, err := s.ClaimSchedulerJob(ctx, "worker-1", time.Minute)
	if err != nil || first == nil || first.Job.ID != job.ID {
		t.Fatalf("first claim=%+v err=%v", first, err)
	}
	second, err := s.ClaimSchedulerJob(ctx, "worker-2", time.Minute)
	if err != nil || second != nil {
		t.Fatalf("second claim=%+v err=%v", second, err)
	}
	if _, err = s.TransitionSchedulerRun(ctx, first.Job.ProjectID, first.Job.RunID, first.Job.ID, first.LeaseToken, "RUNNING", ""); err != nil {
		t.Fatal(err)
	}
}

func TestEvidencePersistenceIsImmutableOrderedAndScoped(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "evidence")
	other := seedRunFixture(t, s, "evidence-other")

	first, err := s.AppendEvent(ctx, store.Event{ProjectID: f.project.ID, IssueID: &f.issue.ID, RunID: &f.run.ID, Type: "run.queued", Actor: store.EmptyObject, Payload: store.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AppendEvent(ctx, store.Event{ProjectID: f.project.ID, IssueID: &f.issue.ID, RunID: &f.run.ID, Type: "run.started", Actor: store.EmptyObject, Payload: store.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence == nil || *first.Sequence != 1 || second.Sequence == nil || *second.Sequence != 2 {
		t.Fatalf("sequences first=%v second=%v", first.Sequence, second.Sequence)
	}
	events, err := s.ListEvents(ctx, f.project.ID, &f.run.ID)
	if err != nil || len(events) != 2 || events[0].Type != "run.queued" || events[1].Type != "run.started" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if otherEvents, err := s.ListEvents(ctx, other.project.ID, &f.run.ID); err != nil || len(otherEvents) != 0 {
		t.Fatalf("cross-project events=%+v err=%v", otherEvents, err)
	}

	chunk, err := s.CreateRawOutputChunk(ctx, store.RawOutputChunk{ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID, Stream: "STDOUT", Sequence: 1, StorageRef: "raw/1", SizeBytes: 5})
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := s.ListRawOutputChunks(ctx, f.project.ID, f.run.ID)
	if err != nil || len(chunks) != 1 || chunks[0].ID != chunk.ID {
		t.Fatalf("chunks=%+v err=%v", chunks, err)
	}
	if crossChunks, err := s.ListRawOutputChunks(ctx, other.project.ID, f.run.ID); err != nil || len(crossChunks) != 0 {
		t.Fatalf("cross-project chunks=%+v err=%v", crossChunks, err)
	}

	artifact, err := s.CreateArtifact(ctx, store.Artifact{ProjectID: f.project.ID, IssueID: f.issue.ID, RunID: f.run.ID, Name: "report", Kind: "text", SizeBytes: 10, StorageRef: "artifact/1", SafeMetadata: store.EmptyObject})
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := s.ListArtifacts(ctx, f.project.ID, f.run.ID)
	if err != nil || len(artifacts) != 1 || artifacts[0].ID != artifact.ID {
		t.Fatalf("artifacts=%+v err=%v", artifacts, err)
	}
	if crossArtifacts, err := s.ListArtifacts(ctx, other.project.ID, f.run.ID); err != nil || len(crossArtifacts) != 0 {
		t.Fatalf("cross-project artifacts=%+v err=%v", crossArtifacts, err)
	}

	provenance, err := s.CreateRunProvenance(ctx, store.RunProvenance{RunID: f.run.ID, ProjectID: f.project.ID, Snapshot: json.RawMessage(`{"engine":"test"}`)})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRunProvenance(ctx, f.project.ID, f.run.ID)
	if err != nil || got.RunID != provenance.RunID {
		t.Fatalf("provenance=%+v err=%v", got, err)
	}
	if _, err = s.GetRunProvenance(ctx, other.project.ID, f.run.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-project provenance error=%v", err)
	}
}

func TestConcurrentEventSequenceAllocation(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	f := seedRunFixture(t, s, "events-concurrent")
	const total = 16

	var wg sync.WaitGroup
	sequences := make(chan int64, total)
	errorsCh := make(chan error, total)
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
		event, err := s.AppendEvent(ctx, store.Event{ProjectID: f.project.ID, IssueID: &f.issue.ID, RunID: &f.run.ID, Type: "test.event", Actor: store.EmptyObject, Payload: json.RawMessage(`{"index":` + string(rune('0'+index%10)) + `}`)})
			if err != nil {
				errorsCh <- err
				return
			}
			if event.Sequence != nil {
				sequences <- *event.Sequence
			}
		}(i)
	}
	wg.Wait()
	close(sequences)
	close(errorsCh)
	for err := range errorsCh {
		t.Fatal(err)
	}
	values := make([]int, 0, total)
	for sequence := range sequences {
		values = append(values, int(sequence))
	}
	sort.Ints(values)
	want := make([]int, total)
	for i := range want {
		want[i] = i + 1
	}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("event sequences=%v want=%v", values, want)
	}
}
