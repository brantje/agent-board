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

type flakyRevisionStore struct {
	*runnerSyncStore
	current     string
	updateCalls int
}

func (s *flakyRevisionStore) GetWorkspaceCurrentRevision(context.Context, string, string) (string, error) {
	return s.current, nil
}

func (s *flakyRevisionStore) UpdateWorkspaceCurrentRevision(_ context.Context, _, _, revision string) (string, error) {
	s.updateCalls++
	if s.updateCalls == 1 {
		return "", errors.New("transient database failure")
	}
	s.current = revision
	return revision, nil
}

type publicationRecoveryClient struct {
	payload      []byte
	sendCalls    int
	receiveCalls int
	confirmCalls int
}

func (c *publicationRecoveryClient) SendTransfer(_ context.Context, _, _ string, direction string, _ []byte, _ runner.TransferProgressFunc) error {
	if direction != runnerprotocol.TransferDirectionGitPublish {
		return errors.New("unexpected transfer direction")
	}
	c.sendCalls++
	return nil
}

func (c *publicationRecoveryClient) ReceiveTransfer(context.Context, string, runner.TransferProgressFunc) (string, []byte, error) {
	c.receiveCalls++
	return "publish-result", append([]byte(nil), c.payload...), nil
}

func (c *publicationRecoveryClient) ConfirmTransferApplied(context.Context, string, string) error {
	c.confirmCalls++
	return nil
}

func TestPublishRemoteGitWorkspaceRetriesRetainedPublicationAfterPersistenceFailure(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	cloneURL := "https://example.invalid/acme/repo.git"
	safe.Project.SourceType = store.ProjectSourceGit
	safe.Project.CloneURL = &cloneURL
	safe.Workspace.WorkingBranch = "agent-board/AB-42"
	published := "fedcba9876543210fedcba9876543210fedcba98"
	payload, err := json.Marshal(runnerprotocol.GitPublished{Revision: published})
	if err != nil {
		t.Fatal(err)
	}

	base := &runnerSyncStore{}
	client := &publicationRecoveryClient{payload: payload}
	processor := newRunnerSyncProcessor(t, repo, safe, base, client)
	revisions := &flakyRevisionStore{runnerSyncStore: base}
	processor.store = revisions

	if err := processor.publishRemoteGitWorkspace(t.Context(), safe, "runner-1", "session-1"); err != nil {
		t.Fatalf("publishRemoteGitWorkspace() error=%v", err)
	}
	if revisions.current != published || revisions.updateCalls != 2 {
		t.Fatalf("revision=%q updateCalls=%d want %q,2", revisions.current, revisions.updateCalls, published)
	}
	if client.sendCalls != 2 || client.receiveCalls != 2 {
		t.Fatalf("publication attempts send=%d receive=%d want 2,2", client.sendCalls, client.receiveCalls)
	}
	if client.confirmCalls != 1 {
		t.Fatalf("ack calls=%d want 1 after durable persistence", client.confirmCalls)
	}
}
