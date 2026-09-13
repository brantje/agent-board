package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCreateIssueRequiresCreator(t *testing.T) {
	projectID := coverageProjectID()
	svc := New(&fakeStore{project: store.Project{ID: projectID}})

	_, err := svc.CreateIssue(context.Background(), store.Issue{
		ProjectID: projectID,
		Title:     "Missing creator",
		Status:    "TODO",
	})
	appErr, ok := AsError(err)
	if !ok || appErr.Code != "invalid_argument" {
		t.Fatalf("create issue error=%v want invalid_argument", err)
	}
}
