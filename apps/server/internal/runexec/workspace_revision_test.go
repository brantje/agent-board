package runexec

import (
	"context"
	"strings"
)

func (s *processTestStore) GetWorkspaceCurrentRevision(context.Context, string, string) (string, error) {
	return "", nil
}

func (s *processTestStore) UpdateWorkspaceCurrentRevision(_ context.Context, _, _, revision string) (string, error) {
	return strings.TrimSpace(revision), nil
}
