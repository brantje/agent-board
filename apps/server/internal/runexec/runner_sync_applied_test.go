package runexec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

func TestSyncWorkspaceFromRunnerAcknowledgesAfterApply(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := git.TransferSnapshot(context.Background(), repo, "returned-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingSyncClient{receive: func(context.Context) (string, []byte, error) {
		return "returned-transfer", payload, nil
	}}
	processor := processorForTransferTests(t, &runnerSyncStore{}, git, recordingSyncConnector{client: client})
	if err := processor.syncWorkspaceFromRunner(context.Background(), safe, "runner-1", "session-1"); err != nil {
		t.Fatal(err)
	}
	if len(client.applied) != 1 || client.applied[0] != "session-1/returned-transfer" {
		t.Fatalf("apply acknowledgements=%v", client.applied)
	}
}

func TestSyncWorkspaceFromRunnerTreatsAckFailureAsSyncFailure(t *testing.T) {
	repo := initProcessTestRepository(t)
	safe := processTestSafeContext(repo)
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := git.TransferSnapshot(context.Background(), repo, "returned-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingSyncClient{
		receive: func(context.Context) (string, []byte, error) { return "returned-transfer", payload, nil },
		confirmErr: errors.New("connection lost before acknowledgement"),
	}
	store := &runnerSyncStore{}
	processor := processorForTransferTests(t, store, git, recordingSyncConnector{client: client})
	err = processor.syncWorkspaceFromRunner(context.Background(), safe, "runner-1", "session-1")
	if err == nil || !strings.Contains(err.Error(), "acknowledge applied workspace transfer") {
		t.Fatalf("ack failure=%v", err)
	}
	if !hasProcessTestEvent(store.events, "workspace.transfer.failed") {
		t.Fatal("ack failure was not recorded as transfer failure")
	}
}
