package runnerprotocol

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const (
	TransferChunkSize = 64 << 10
	MaxTransferBytes  = 512 << 20
	MaxMessageSize    = 1 << 20

	TransferDirectionToRunner   = "to_runner"
	TransferDirectionFromRunner = "from_runner"
	TransferDirectionGitPrepare = "git_prepare"
	TransferDirectionGitPublish = "git_publish"
)

func ValidateTransferBegin(begin TransferBegin) error {
	if begin.TransferID == "" {
		return fmt.Errorf("transfer id is required")
	}
	if begin.TotalBytes < 0 || begin.TotalBytes > MaxTransferBytes {
		return fmt.Errorf("transfer size is invalid")
	}
	return nil
}

func TransferChecksum(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func AppendTransferChunk(buffer []byte, expected int64, chunk TransferChunk) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(chunk.Data)
	if err != nil {
		return buffer, fmt.Errorf("decode transfer chunk: %w", err)
	}
	total := int64(len(buffer) + len(data))
	if (expected == 0 && len(data) > 0) || total > MaxTransferBytes || (expected > 0 && total > expected) {
		return buffer, fmt.Errorf("transfer payload exceeded declared size")
	}
	return append(buffer, data...), nil
}

func ValidateTransferPayload(payload []byte, expected int64, checksum string) error {
	if checksum != "" && TransferChecksum(payload) != checksum {
		return fmt.Errorf("transfer checksum mismatch")
	}
	if expected > 0 && int64(len(payload)) != expected {
		return fmt.Errorf("transfer payload size mismatch")
	}
	return nil
}

type TransferBegin struct {
	TransferID string `json:"transfer_id"`
	Direction  string `json:"direction"`
	TotalBytes int64  `json:"total_bytes"`
	Checksum   string `json:"checksum"`
}

type TransferChunk struct {
	TransferID string `json:"transfer_id"`
	Data       string `json:"data"`
}

type TransferEnd struct {
	TransferID string `json:"transfer_id"`
}

type TransferFailed struct {
	TransferID string `json:"transfer_id"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

// GitPrepare describes the remote source needed to prepare one durable Issue
// branch on the Runner. It is sent as the small payload of a git_prepare
// transfer; repository contents themselves are fetched by Git on the Runner.
type GitPrepare struct {
	CloneURL         string `json:"clone_url"`
	Ref              string `json:"ref,omitempty"`
	IssueBranch      string `json:"issue_branch"`
	RecordedRevision string `json:"recorded_revision,omitempty"`
}

// GitPublished is returned after the Runner safely finalizes and normally
// pushes the Issue branch. StartRevision is the exact revision from which this
// retained Runner worktree began; the server persists it as the remote
// Workspace base before acknowledging cleanup.
type GitPublished struct {
	StartRevision string `json:"start_revision"`
	Revision      string `json:"revision"`
}

// TransferApplied acknowledges that the server verified and successfully
// applied a from_runner transfer or persisted a git_publish revision. Only this
// acknowledgement permits the Runner to delete its session Workspace/worktree.
type TransferApplied struct {
	TransferID string `json:"transfer_id"`
}
