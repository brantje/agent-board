package runner

import (
	"context"
	"fmt"
	"strings"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
)

// ConfirmTransferApplied tells the runner that the authoritative Workspace has
// accepted a returned transfer. Runner-local cleanup is gated on this message.
func (c *Connection) ConfirmTransferApplied(ctx context.Context, sessionID, transferID string) error {
	if c == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(transferID) == "" {
		return fmt.Errorf("runner transfer apply acknowledgement requires session and transfer ids")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.write(protocol.TypeTransferApplied, sessionID, protocol.TransferApplied{TransferID: transferID})
}
