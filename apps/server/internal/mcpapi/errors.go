package mcpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

func toolError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if apiErr, ok := app.AsError(err); ok {
		return fmt.Errorf("%s: %s", apiErr.Code, apiErr.Message)
	}
	slog.ErrorContext(ctx, "mcp tool failed", "error", err)
	return errors.New("internal_error: internal server error")
}

func requireUUID(value, field string) error {
	if validUUID(value) {
		return nil
	}
	return app.NewError("invalid_argument", field+" must be a UUID", store.ErrInvalidArgument)
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for i, r := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}
