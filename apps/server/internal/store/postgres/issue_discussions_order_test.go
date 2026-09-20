package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestOrderIssueCommentsAncestorFirstRejectsInvalidGraphsAndOrdersSiblings(t *testing.T) {
	at := time.Date(2026, 9, 20, 18, 0, 0, 0, time.UTC)
	rootID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	earlyID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	lateID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"

	t.Run("orders siblings deterministically", func(t *testing.T) {
		values := []store.IssueComment{
			{ID: lateID, ParentCommentID: &rootID, CreatedAt: at.Add(time.Second)},
			{ID: rootID, CreatedAt: at},
			{ID: earlyID, ParentCommentID: &rootID, CreatedAt: at.Add(time.Second)},
		}
		ordered, err := orderIssueCommentsAncestorFirst(values)
		if err != nil {
			t.Fatal(err)
		}
		if len(ordered) != 3 || ordered[0].ID != rootID || ordered[1].ID != earlyID || ordered[2].ID != lateID {
			t.Fatalf("ordered=%+v", ordered)
		}
	})

	t.Run("rejects orphan", func(t *testing.T) {
		missing := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
		_, err := orderIssueCommentsAncestorFirst([]store.IssueComment{{
			ID: earlyID, ParentCommentID: &missing, CreatedAt: at,
		}})
		if !errors.Is(err, store.ErrInvalidArgument) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("rejects cycle", func(t *testing.T) {
		leftID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
		rightID := "ffffffff-ffff-4fff-8fff-ffffffffffff"
		_, err := orderIssueCommentsAncestorFirst([]store.IssueComment{
			{ID: leftID, ParentCommentID: &rightID, CreatedAt: at},
			{ID: rightID, ParentCommentID: &leftID, CreatedAt: at},
		})
		if !errors.Is(err, store.ErrInvalidArgument) {
			t.Fatalf("error=%v", err)
		}
	})
}
