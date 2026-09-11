package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/agent-runner/internal/protocol"
)

func TestRemoteGitPublicationCanRetryBeforeAckAndContinueOnAnotherRunner(t *testing.T) {
	origin, mainRevision := createRemoteTransferOrigin(t)
	firstRunner, firstServer := newTestRunner(t)
	firstConn := dialAndHandshake(t, firstServer.URL, 1)
	defer firstConn.Close()
	firstWriter := &connectionWriter{conn: firstConn}
	const sessionID = "remote-recovery"
	const branch = "agent-board/AB-80"

	prepare, err := json.Marshal(protocol.GitPrepare{CloneURL: origin, Ref: "main", IssueBranch: branch})
	if err != nil {
		t.Fatal(err)
	}
	if err := firstWriter.sendTransfer(context.Background(), sessionID, "git-prepare", protocol.TransferDirectionGitPrepare, prepare); err != nil {
		t.Fatal(err)
	}
	send(t, firstConn, protocol.TypeStart, sessionID, protocol.StartRequest{
		Command: []string{"sh", "-c", "printf recovered > recovered.txt"},
		Dir:     "/workspace",
	})
	waitForExit(t, firstConn, sessionID)
	waitFor(t, time.Second, func() bool { return firstRunner.manager.ActiveCount() == 0 })

	if err := firstWriter.sendTransfer(context.Background(), sessionID, "publish-1", protocol.TransferDirectionGitPublish, nil); err != nil {
		t.Fatal(err)
	}
	_, firstPublished := readGitPublishedTransfer(t, firstConn)
	if firstPublished.Revision == "" || firstPublished.Revision == mainRevision {
		t.Fatalf("first published revision=%q start=%q", firstPublished.Revision, mainRevision)
	}
	worktree := firstRunner.manager.SessionWorkspacePath(sessionID)
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("worktree was removed before acknowledgement: %v", err)
	}

	// Simulate a successful push whose response or server-side persistence was
	// lost: do not acknowledge the first publication and ask the same retained
	// session to publish again. The second normal push must be idempotent.
	if err := firstWriter.sendTransfer(context.Background(), sessionID, "publish-2", protocol.TransferDirectionGitPublish, nil); err != nil {
		t.Fatal(err)
	}
	secondTransferID, secondPublished := readGitPublishedTransfer(t, firstConn)
	if secondPublished.Revision != firstPublished.Revision {
		t.Fatalf("retry revision=%q want=%q", secondPublished.Revision, firstPublished.Revision)
	}
	if got := gitOutput(t, origin, "rev-parse", "refs/heads/"+branch); got != secondPublished.Revision {
		t.Fatalf("remote Issue branch=%q want=%q", got, secondPublished.Revision)
	}
	if _, err := os.Stat(filepath.Join(worktree, "recovered.txt")); err != nil {
		t.Fatalf("retained worktree lost published content: %v", err)
	}

	send(t, firstConn, protocol.TypeTransferApplied, sessionID, protocol.TransferApplied{TransferID: secondTransferID})
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(worktree)
		return os.IsNotExist(err)
	})

	secondRunner, secondServer := newTestRunner(t)
	secondConn := dialAndHandshake(t, secondServer.URL, 1)
	defer secondConn.Close()
	secondWriter := &connectionWriter{conn: secondConn}
	secondPrepare, err := json.Marshal(protocol.GitPrepare{
		CloneURL:         origin,
		Ref:              "main",
		IssueBranch:      branch,
		RecordedRevision: secondPublished.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := secondWriter.sendTransfer(context.Background(), "remote-recovery-next", "git-prepare-next", protocol.TransferDirectionGitPrepare, secondPrepare); err != nil {
		t.Fatal(err)
	}
	secondWorktree := secondRunner.manager.SessionWorkspacePath("remote-recovery-next")
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(secondWorktree, "recovered.txt"))
		return err == nil
	})
	if got := gitOutput(t, secondWorktree, "rev-parse", "HEAD"); got != secondPublished.Revision {
		t.Fatalf("second Runner start=%q want=%q", got, secondPublished.Revision)
	}
}
