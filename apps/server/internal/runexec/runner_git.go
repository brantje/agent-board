package runexec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/packages/runnerprotocol"
)

const remoteGitPublicationAttempts = 2

func isRemoteGitProject(safe executioncontext.SafeContext) bool {
	return strings.EqualFold(strings.TrimSpace(safe.Project.SourceType), store.ProjectSourceGit)
}

func (p *Processor) prepareRemoteGitWorkspace(ctx context.Context, safe executioncontext.SafeContext, runnerID, sessionID string) error {
	if sessionID == "" || p.runners == nil {
		return fmt.Errorf("remote Git workspace requires a prepared runner execution session")
	}
	if safe.Project.CloneURL == nil || strings.TrimSpace(*safe.Project.CloneURL) == "" {
		return fmt.Errorf("remote Git Project clone URL is unavailable")
	}
	branch := strings.TrimSpace(safe.Workspace.WorkingBranch)
	if !strings.HasPrefix(branch, "agent-board/") {
		return fmt.Errorf("remote Git Issue branch must use agent-board/ namespace")
	}
	revisions, ok := p.store.(store.WorkspaceRevisionStore)
	if !ok {
		return fmt.Errorf("workspace revision store is unavailable")
	}
	recordedRevision, err := revisions.GetWorkspaceCurrentRevision(ctx, safe.Project.ID, safe.Workspace.ID)
	if err != nil {
		return err
	}
	request := runnerprotocol.GitPrepare{
		CloneURL:         strings.TrimSpace(*safe.Project.CloneURL),
		Ref:              optionalStringValue(safe.Project.SourceRef),
		IssueBranch:      branch,
		RecordedRevision: recordedRevision,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode remote Git workspace request: %w", err)
	}
	transferID := fmt.Sprintf("%s-git-prepare-%d", safe.Run.ID, time.Now().UnixNano())
	if err := p.record(ctx, safe, "workspace.transfer.started", p.transferEventPayload(ctx, runnerID, transferID, runnerprotocol.TransferDirectionGitPrepare, nil), nil, nil); err != nil {
		return err
	}
	client, err := p.runners.Connect(ctx, safe.Project.ID, runnerID)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, runnerprotocol.TransferDirectionGitPrepare, map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	if err := client.SendTransfer(ctx, sessionID, transferID, runnerprotocol.TransferDirectionGitPrepare, payload, nil); err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, runnerprotocol.TransferDirectionGitPrepare, map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	return p.record(ctx, safe, "workspace.transfer.completed", p.transferEventPayload(ctx, runnerID, transferID, runnerprotocol.TransferDirectionGitPrepare, map[string]any{
		"bytesTransferred": len(payload), "totalBytes": len(payload),
	}), nil, nil)
}

func (p *Processor) publishRemoteGitWorkspace(ctx context.Context, safe executioncontext.SafeContext, runnerID, sessionID string) error {
	if sessionID == "" || p.runners == nil {
		return fmt.Errorf("remote Git publication requires a prepared runner execution session")
	}
	revisions, ok := p.store.(store.WorkspaceRevisionStore)
	if !ok {
		return fmt.Errorf("workspace revision store is unavailable")
	}
	var lastErr error
	for attempt := 0; attempt < remoteGitPublicationAttempts; attempt++ {
		lastErr = p.publishRemoteGitWorkspaceAttempt(ctx, safe, runnerID, sessionID, revisions)
		if lastErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			return lastErr
		}
	}
	return lastErr
}

func (p *Processor) publishRemoteGitWorkspaceAttempt(ctx context.Context, safe executioncontext.SafeContext, runnerID, sessionID string, revisions store.WorkspaceRevisionStore) error {
	transferID := fmt.Sprintf("%s-git-publish-%d", sessionID, time.Now().UnixNano())
	if err := p.record(ctx, safe, "workspace.transfer.started", p.transferEventPayload(ctx, runnerID, transferID, runnerprotocol.TransferDirectionGitPublish, nil), nil, nil); err != nil {
		return err
	}
	client, err := p.runners.Connect(ctx, safe.Project.ID, runnerID)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, runnerprotocol.TransferDirectionGitPublish, map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	if err := client.SendTransfer(ctx, sessionID, transferID, runnerprotocol.TransferDirectionGitPublish, nil, nil); err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, runnerprotocol.TransferDirectionGitPublish, map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	receivedID, payload, err := client.ReceiveTransfer(ctx, sessionID, nil)
	if err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, runnerprotocol.TransferDirectionGitPublish, map[string]any{"reason": err.Error()}), nil, nil)
		return err
	}
	if receivedID != "" {
		transferID = receivedID
	}
	var published runnerprotocol.GitPublished
	if err := json.Unmarshal(payload, &published); err != nil || strings.TrimSpace(published.Revision) == "" {
		if err == nil {
			err = fmt.Errorf("published revision is empty")
		}
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, runnerprotocol.TransferDirectionGitPublish, map[string]any{"reason": err.Error()}), nil, nil)
		return fmt.Errorf("decode remote Git publication: %w", err)
	}
	if _, err := revisions.UpdateWorkspaceCurrentRevision(ctx, safe.Project.ID, safe.Workspace.ID, published.Revision); err != nil {
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, runnerprotocol.TransferDirectionGitPublish, map[string]any{"reason": err.Error()}), nil, nil)
		return fmt.Errorf("persist published Issue revision: %w", err)
	}
	if err := client.ConfirmTransferApplied(ctx, sessionID, transferID); err != nil {
		wrapped := fmt.Errorf("acknowledge persisted remote Git publication: %w", err)
		_ = p.record(ctx, safe, "workspace.transfer.failed", p.transferEventPayload(ctx, runnerID, transferID, runnerprotocol.TransferDirectionGitPublish, map[string]any{"reason": wrapped.Error()}), nil, nil)
		return wrapped
	}
	return p.record(ctx, safe, "workspace.transfer.completed", p.transferEventPayload(ctx, runnerID, transferID, runnerprotocol.TransferDirectionGitPublish, map[string]any{
		"revision": published.Revision, "bytesTransferred": len(payload), "totalBytes": len(payload),
	}), nil, nil)
}

func (p *Processor) persistLocalWorkspaceRevision(ctx context.Context, safe executioncontext.SafeContext) error {
	revisions, ok := p.store.(store.WorkspaceRevisionStore)
	if !ok {
		return fmt.Errorf("workspace revision store is unavailable")
	}
	if p.git == nil {
		return fmt.Errorf("workspace Git is unavailable")
	}
	revision, err := p.git.HeadRevision(ctx, safe.Workspace.Path)
	if err != nil {
		return err
	}
	_, err = revisions.UpdateWorkspaceCurrentRevision(ctx, safe.Project.ID, safe.Workspace.ID, revision)
	return err
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
