package runexec

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/packages/runnerprotocol"
)

type revisionTrackingRunStore struct {
	*runnerSyncStore
	current   string
	updateErr error
}

func (s *revisionTrackingRunStore) GetWorkspaceCurrentRevision(context.Context, string, string) (string, error) {
	return s.current, nil
}

func (s *revisionTrackingRunStore) UpdateWorkspaceCurrentRevision(_ context.Context, _, _, revision string) (string, error) {
	if s.updateErr != nil {
		return "", s.updateErr
	}
	s.current = revision
	return revision, nil
}

type gitControlClient struct {
	directions []string
	payloads   [][]byte
	receiveID  string
	receive    []byte
	confirmed  bool
}

func (c *gitControlClient) SendTransfer(_ context.Context, _, _ string, direction string, payload []byte, progress runner.TransferProgressFunc) error {
	c.directions = append(c.directions, direction)
	c.payloads = append(c.payloads, append([]byte(nil), payload...))
	if progress != nil && len(payload) > 0 {
		progress(runner.TransferProgress{BytesTransferred: int64(len(payload)), TotalBytes: int64(len(payload))})
	}
	return nil
}

func (c *gitControlClient) ReceiveTransfer(context.Context, string, runner.TransferProgressFunc) (string, []byte, error) {
	return c.receiveID, append([]byte(nil), c.receive...), nil
}

func (c *gitControlClient) ConfirmTransferApplied(context.Context, string, string) error {
	c.confirmed = true
	return nil
}

func remoteRunnerSafeContext(t *testing.T) (storeFake *runnerSyncStore, safeContext store.Run, processor *Processor, safeProjectCloneURL string) {
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
	client := &gitControlClient{}
	p := newRunnerSyncProcessor(t, repo, safe, base, client)
	return base, store.Run{ID: safe.Run.ID, ProjectID: safe.Project.ID, IssueID: safe.Issue.ID, WorkspaceID: safe.Workspace.ID}, p, cloneURL
}

func TestPrepareRemoteGitWorkspaceSendsSourceContractAndRecordedRevision(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	cloneURL := "https://example.invalid/acme/repo.git"
	ref := "release"
	safe.Project.SourceType = store.ProjectSourceGit
	safe.Project.CloneURL = &cloneURL
	safe.Project.SourceRef = &ref
	safe.Workspace.WorkingBranch = "agent-board/AB-42"
	base := &runnerSyncStore{}
	client := &gitControlClient{}
	processor := newRunnerSyncProcessor(t, repo, safe, base, client)
	revisions := &revisionTrackingRunStore{runnerSyncStore: base, current: "0123456789012345678901234567890123456789"}
	processor.store = revisions

	if err := processor.prepareRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); err != nil {
		t.Fatal(err)
	}
	if len(client.directions) != 1 || client.directions[0] != runnerprotocol.TransferDirectionGitPrepare {
		t.Fatalf("directions=%v", client.directions)
	}
	var request runnerprotocol.GitPrepare
	if err := json.Unmarshal(client.payloads[0], &request); err != nil {
		t.Fatal(err)
	}
	if request.CloneURL != cloneURL || request.Ref != ref || request.IssueBranch != "agent-board/AB-42" || request.RecordedRevision != revisions.current {
		t.Fatalf("prepare request=%+v", request)
	}
	if client.confirmed {
		t.Fatal("prepare transfer must not acknowledge Runner workspace cleanup")
	}
}

func TestPublishRemoteGitWorkspacePersistsRevisionBeforeAcknowledgement(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	cloneURL := "https://example.invalid/acme/repo.git"
	safe.Project.SourceType = store.ProjectSourceGit
	safe.Project.CloneURL = &cloneURL
	safe.Workspace.WorkingBranch = "agent-board/AB-42"
	base := &runnerSyncStore{}
	startRevision := "0123456789012345678901234567890123456789"
	published := "fedcba9876543210fedcba9876543210fedcba98"
	payload, err := json.Marshal(runnerprotocol.GitPublished{StartRevision: startRevision, Revision: published})
	if err != nil {
		t.Fatal(err)
	}
	client := &gitControlClient{receiveID: "publish-result", receive: payload}
	processor := newRunnerSyncProcessor(t, repo, safe, base, client)
	revisions := &revisionTrackingRunStore{runnerSyncStore: base}
	processor.store = revisions

	if err := processor.publishRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); err != nil {
		t.Fatal(err)
	}
	if revisions.current != published {
		t.Fatalf("persisted revision=%q want=%q", revisions.current, published)
	}
	if !client.confirmed {
		t.Fatal("persisted publication was not acknowledged")
	}
	if len(client.directions) != 1 || client.directions[0] != runnerprotocol.TransferDirectionGitPublish {
		t.Fatalf("directions=%v", client.directions)
	}
}

func TestPublishRemoteGitWorkspacePersistenceFailureDoesNotAcknowledge(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	cloneURL := "https://example.invalid/acme/repo.git"
	safe.Project.SourceType = store.ProjectSourceGit
	safe.Project.CloneURL = &cloneURL
	safe.Workspace.WorkingBranch = "agent-board/AB-42"
	base := &runnerSyncStore{}
	payload, err := json.Marshal(runnerprotocol.GitPublished{
		StartRevision: "0123456789012345678901234567890123456789",
		Revision:      "fedcba9876543210fedcba9876543210fedcba98",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &gitControlClient{receiveID: "publish-result", receive: payload}
	processor := newRunnerSyncProcessor(t, repo, safe, base, client)
	processor.store = &revisionTrackingRunStore{runnerSyncStore: base, updateErr: errors.New("database unavailable")}

	if err := processor.publishRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); err == nil {
		t.Fatal("publication unexpectedly succeeded when revision persistence failed")
	}
	if client.confirmed {
		t.Fatal("Runner worktree cleanup was acknowledged before durable revision persistence")
	}
}

func TestSyncWorkspaceFromRunnerSelectsRemoteGitPublication(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	cloneURL := "https://example.invalid/acme/repo.git"
	safe.Project.SourceType = store.ProjectSourceGit
	safe.Project.CloneURL = &cloneURL
	safe.Workspace.WorkingBranch = "agent-board/AB-42"
	base := &runnerSyncStore{}
	payload, err := json.Marshal(runnerprotocol.GitPublished{
		StartRevision: "0123456789012345678901234567890123456789",
		Revision:      "fedcba9876543210fedcba9876543210fedcba98",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &gitControlClient{receive: payload}
	processor := newRunnerSyncProcessor(t, repo, safe, base, client)
	processor.store = &revisionTrackingRunStore{runnerSyncStore: base}

	if err := processor.syncWorkspaceFromRunner(t.Context(), safe, "runner-1", "session-1"); err != nil {
		t.Fatal(err)
	}
	if len(client.directions) != 1 || client.directions[0] != runnerprotocol.TransferDirectionGitPublish {
		t.Fatalf("remote Project used wrong hand-back path: %v", client.directions)
	}
}
