package app

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *projectAccessCRUDStore) ListProjectMembers(context.Context, string) ([]store.ProjectMember, error) {
	return nil, nil
}
