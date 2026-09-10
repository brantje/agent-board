package runnerprotocol

import "fmt"

const (
	TransferChunkSize = 64 << 10
	MaxTransferBytes  = 512 << 20
	MaxMessageSize    = 1 << 20
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

// TransferApplied acknowledges that the server verified and successfully
// applied a from_runner transfer. Only this acknowledgement permits the Runner
// to delete its session Workspace.
type TransferApplied struct {
	TransferID string `json:"transfer_id"`
}
