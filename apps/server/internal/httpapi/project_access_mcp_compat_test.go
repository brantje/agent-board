package httpapi

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *projectAccessHTTPStore) ListProjectMembers(context.Context, string) ([]store.ProjectMember, error) {
	return nil, nil
}
