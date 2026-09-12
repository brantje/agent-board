package runexec

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/packages/runnerprotocol"
)

type failingRevisionRunStore struct {
	*runnerSyncStore
	current   string
	getErr    error
	updateErr error
}

func (s *failingRevisionRunStore) GetWorkspaceCurrentRevision(context.Context, string, string) (string, error) {
	if s.getErr != nil {
		return "", s.getErr
	}
	return s.current, nil
}

func (s *failingRevisionRunStore) UpdateWorkspaceCurrentRevision(_ context.Context, _, _, revision string) (string, error) {
	if s.updateErr != nil {
		return "", s.updateErr
	}
	s.current = revision
	return revision, nil
}

type controlledGitClient struct {
	sendErr    error
	receiveErr error
	confirmErr error
	receiveID  string
	receive    []byte
	directions []string
	confirmed  bool
}

func (c *controlledGitClient) SendTransfer(_ context.Context, _, _ string, direction string, _ []byte, _ runner.TransferProgressFunc) error {
	c.directions = append(c.directions, direction)
	return c.sendErr
}

func (c *controlledGitClient) ReceiveTransfer(context.Context, string, runner.TransferProgressFunc) (string, []byte, error) {
	if c.receiveErr != nil {
		return "", nil, c.receiveErr
	}
	return c.receiveID, append([]byte(nil), c.receive...), nil
}

func (c *controlledGitClient) ConfirmTransferApplied(context.Context, string, string) error {
	if c.confirmErr != nil {
		return c.confirmErr
	}
	c.confirmed = true
	return nil
}

func remoteGitFailureHarness(t *testing.T, client runnerClient) (executioncontext.SafeContext, *runnerSyncStore, *Processor) {
	t.Helper()
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	cloneURL := "https://example.invalid/acme/repo.git"
	safe.Project.SourceType = store.ProjectSourceGit
	safe.Project.CloneURL = &cloneURL
	safe.Project.RepositoryPath = ""
	safe.Project.DefaultBranch = ""
	safe.Workspace.WorkingBranch = "agent-board/AB-42"
	base := &runnerSyncStore{}
	return safe, base, newRunnerSyncProcessor(t, repo, safe, base, client)
}

func TestPrepareRemoteGitWorkspaceRejectsInvalidStateBeforeTransfer(t *testing.T) {
	t.Run("missing prepared session", func(t *testing.T) {
		safe, _, processor := remoteGitFailureHarness(t, &controlledGitClient{})
		if err := processor.prepareRemoteGitWorkspace(t.Context(), safe, "runner-1", ""); err == nil || !strings.Contains(err.Error(), "prepared runner execution session") {
			t.Fatalf("prepare error=%v", err)
		}
	})

	t.Run("missing clone URL", func(t *testing.T) {
		safe, _, processor := remoteGitFailureHarness(t, &controlledGitClient{})
		safe.Project.CloneURL = nil
		if err := processor.prepareRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); err == nil || !strings.Contains(err.Error(), "clone URL") {
			t.Fatalf("prepare error=%v", err)
		}
	})

	t.Run("blank clone URL", func(t *testing.T) {
		safe, _, processor := remoteGitFailureHarness(t, &controlledGitClient{})
		blank := "   "
		safe.Project.CloneURL = &blank
		if err := processor.prepareRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); err == nil || !strings.Contains(err.Error(), "clone URL") {
			t.Fatalf("prepare error=%v", err)
		}
	})

	t.Run("wrong Issue branch namespace", func(t *testing.T) {
		safe, _, processor := remoteGitFailureHarness(t, &controlledGitClient{})
		safe.Workspace.WorkingBranch = "feature/AB-42"
		if err := processor.prepareRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); err == nil || !strings.Contains(err.Error(), "agent-board/") {
			t.Fatalf("prepare error=%v", err)
		}
	})

	t.Run("revision read failure", func(t *testing.T) {
		safe, base, processor := remoteGitFailureHarness(t, &controlledGitClient{})
		want := errors.New("revision unavailable")
		processor.store = &failingRevisionRunStore{runnerSyncStore: base, getErr: want}
		if err := processor.prepareRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); !errors.Is(err, want) {
			t.Fatalf("prepare error=%v want=%v", err, want)
		}
	})
}

func TestPrepareRemoteGitWorkspaceReportsRunnerTransportFailures(t *testing.T) {
	t.Run("connect failure", func(t *testing.T) {
		safe, base, processor := remoteGitFailureHarness(t, &controlledGitClient{})
		processor.store = &failingRevisionRunStore{runnerSyncStore: base}
		want := errors.New("runner disconnected")
		processor.runners = failingRunnerSyncConnector{err: want}
		if err := processor.prepareRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); !errors.Is(err, want) {
			t.Fatalf("prepare error=%v want=%v", err, want)
		}
		if !hasProcessTestEvent(base.events, "workspace.transfer.failed") {
			t.Fatalf("events=%+v", base.events)
		}
	})

	t.Run("send failure", func(t *testing.T) {
		want := errors.New("prepare transfer failed")
		client := &controlledGitClient{sendErr: want}
		safe, base, processor := remoteGitFailureHarness(t, client)
		processor.store = &failingRevisionRunStore{runnerSyncStore: base}
		if err := processor.prepareRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); !errors.Is(err, want) {
			t.Fatalf("prepare error=%v want=%v", err, want)
		}
		if len(client.directions) != 1 || client.directions[0] != runnerprotocol.TransferDirectionGitPrepare {
			t.Fatalf("directions=%v", client.directions)
		}
		if !hasProcessTestEvent(base.events, "workspace.transfer.failed") {
			t.Fatalf("events=%+v", base.events)
		}
	})
}

func TestPublishRemoteGitWorkspaceRejectsUnsafePublication(t *testing.T) {
	t.Run("missing prepared session", func(t *testing.T) {
		safe, _, processor := remoteGitFailureHarness(t, &controlledGitClient{})
		if err := processor.publishRemoteGitWorkspace(t.Context(), safe, "runner-1", ""); err == nil || !strings.Contains(err.Error(), "prepared runner execution session") {
			t.Fatalf("publish error=%v", err)
		}
	})

	t.Run("malformed publication", func(t *testing.T) {
		client := &controlledGitClient{receiveID: "publish-result", receive: []byte("not-json")}
		safe, base, processor := remoteGitFailureHarness(t, client)
		processor.store = &failingRevisionRunStore{runnerSyncStore: base}
		if err := processor.publishRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); err == nil || !strings.Contains(err.Error(), "decode remote Git publication") {
			t.Fatalf("publish error=%v", err)
		}
		if client.confirmed {
			t.Fatal("malformed publication was acknowledged")
		}
	})

	t.Run("empty published revision", func(t *testing.T) {
		payload, err := json.Marshal(runnerprotocol.GitPublished{StartRevision: "0123456789012345678901234567890123456789"})
		if err != nil {
			t.Fatal(err)
		}
		client := &controlledGitClient{receiveID: "publish-result", receive: payload}
		safe, base, processor := remoteGitFailureHarness(t, client)
		processor.store = &failingRevisionRunStore{runnerSyncStore: base}
		if err := processor.publishRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); err == nil || !strings.Contains(err.Error(), "published start/review revision is empty") {
			t.Fatalf("publish error=%v", err)
		}
		if client.confirmed {
			t.Fatal("empty publication was acknowledged")
		}
	})
}

func TestPublishRemoteGitWorkspaceReportsTransportAndAckFailures(t *testing.T) {
	published := "fedcba9876543210fedcba9876543210fedcba98"
	payload, err := json.Marshal(runnerprotocol.GitPublished{
		StartRevision: "0123456789012345678901234567890123456789",
		Revision:      published,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("connect failure", func(t *testing.T) {
		safe, base, processor := remoteGitFailureHarness(t, &controlledGitClient{})
		processor.store = &failingRevisionRunStore{runnerSyncStore: base}
		want := errors.New("runner unavailable")
		processor.runners = failingRunnerSyncConnector{err: want}
		if err := processor.publishRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); !errors.Is(err, want) {
			t.Fatalf("publish error=%v want=%v", err, want)
		}
		if !hasProcessTestEvent(base.events, "workspace.transfer.failed") {
			t.Fatalf("events=%+v", base.events)
		}
	})

	t.Run("send failure", func(t *testing.T) {
		want := errors.New("publish request failed")
		client := &controlledGitClient{sendErr: want}
		safe, base, processor := remoteGitFailureHarness(t, client)
		processor.store = &failingRevisionRunStore{runnerSyncStore: base}
		if err := processor.publishRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); !errors.Is(err, want) {
			t.Fatalf("publish error=%v want=%v", err, want)
		}
	})

	t.Run("receive failure", func(t *testing.T) {
		want := errors.New("publish response failed")
		client := &controlledGitClient{receiveErr: want}
		safe, base, processor := remoteGitFailureHarness(t, client)
		processor.store = &failingRevisionRunStore{runnerSyncStore: base}
		if err := processor.publishRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); !errors.Is(err, want) {
			t.Fatalf("publish error=%v want=%v", err, want)
		}
	})

	t.Run("acknowledgement failure after persistence", func(t *testing.T) {
		want := errors.New("ack failed")
		client := &controlledGitClient{receiveID: "publish-result", receive: payload, confirmErr: want}
		safe, base, processor := remoteGitFailureHarness(t, client)
		revisions := &failingRevisionRunStore{runnerSyncStore: base}
		processor.store = revisions
		err := processor.publishRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1")
		if err == nil || !strings.Contains(err.Error(), "acknowledge persisted remote Git publication") || !errors.Is(err, want) {
			t.Fatalf("publish error=%v", err)
		}
		if revisions.current != published {
			t.Fatalf("persisted revision=%q want=%q", revisions.current, published)
		}
		if client.confirmed {
			t.Fatal("failed acknowledgement reported as confirmed")
		}
		if !hasProcessTestEvent(base.events, "workspace.transfer.failed") {
			t.Fatalf("events=%+v", base.events)
		}
	})
}

func TestPersistLocalWorkspaceRevisionUsesGitAndPersistsHead(t *testing.T) {
	t.Run("missing Git", func(t *testing.T) {
		safe, base, processor := remoteGitFailureHarness(t, &controlledGitClient{})
		safe.Project.SourceType = store.ProjectSourceLocal
		processor.store = &failingRevisionRunStore{runnerSyncStore: base}
		processor.git = nil
		if err := processor.persistLocalWorkspaceRevision(t.Context(), safe); err == nil || !strings.Contains(err.Error(), "Git is unavailable") {
			t.Fatalf("persist error=%v", err)
		}
	})

	t.Run("head failure", func(t *testing.T) {
		safe, base, processor := remoteGitFailureHarness(t, &controlledGitClient{})
		safe.Project.SourceType = store.ProjectSourceLocal
		safe.Workspace.Path = t.TempDir()
		processor.store = &failingRevisionRunStore{runnerSyncStore: base}
		if err := processor.persistLocalWorkspaceRevision(t.Context(), safe); err == nil {
			t.Fatal("non-repository workspace unexpectedly persisted a revision")
		}
	})

	t.Run("persistence failure", func(t *testing.T) {
		safe, base, processor := remoteGitFailureHarness(t, &controlledGitClient{})
		safe.Project.SourceType = store.ProjectSourceLocal
		want := errors.New("database unavailable")
		processor.store = &failingRevisionRunStore{runnerSyncStore: base, updateErr: want}
		if err := processor.persistLocalWorkspaceRevision(t.Context(), safe); !errors.Is(err, want) {
			t.Fatalf("persist error=%v want=%v", err, want)
		}
	})

	t.Run("success", func(t *testing.T) {
		safe, base, processor := remoteGitFailureHarness(t, &controlledGitClient{})
		safe.Project.SourceType = store.ProjectSourceLocal
		revisions := &failingRevisionRunStore{runnerSyncStore: base}
		processor.store = revisions
		if err := processor.persistLocalWorkspaceRevision(t.Context(), safe); err != nil {
			t.Fatal(err)
		}
		if revisions.current == "" {
			t.Fatal("workspace revision was not persisted")
		}
	})
}
