package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

func TestIssueKeyHelpersRejectUUIDsAndInvalidKeys(t *testing.T) {
	resolve := func(context.Context, string, string) (string, error) {
		return issueID, nil
	}

	t.Run("path rejects uuid", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/issues/"+issueID, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("issueID", issueID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		_, ok := pathIssueKey(rec, req, projectID, resolve)
		if ok || rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid path key rejection, ok=%v status=%d body=%s", ok, rec.Code, rec.Body.String())
		}
	})

	t.Run("path rejects malformed key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/issues/bad-key", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("issueID", "bad-key")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		_, ok := pathIssueKey(rec, req, projectID, resolve)
		if ok || rec.Code != http.StatusBadRequest {
			t.Fatalf("expected malformed key rejection, ok=%v status=%d", ok, rec.Code)
		}
	})

	t.Run("query rejects uuid", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/questions?issueId="+issueID, nil)
		value, ok := queryIssueKey(rec, req, "issueId", projectID, resolve)
		if ok || value != nil || rec.Code != http.StatusBadRequest {
			t.Fatalf("expected uuid query rejection, ok=%v value=%v status=%d", ok, value, rec.Code)
		}
	})

	t.Run("query omits empty filter", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/questions", nil)
		value, ok := queryIssueKey(rec, req, "issueId", projectID, resolve)
		if !ok || value != nil {
			t.Fatalf("expected empty query to pass, ok=%v value=%v", ok, value)
		}
	})

	t.Run("body rejects uuid", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/relationships", nil)
		_, ok := bodyIssueKey(rec, req, projectID, issueID, "targetIssueId", resolve)
		if ok || rec.Code != http.StatusBadRequest {
			t.Fatalf("expected uuid body rejection, ok=%v status=%d", ok, rec.Code)
		}
	})

	t.Run("query rejects malformed key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/questions?issueId=bad-key", nil)
		value, ok := queryIssueKey(rec, req, "issueId", projectID, resolve)
		if ok || value != nil || rec.Code != http.StatusBadRequest {
			t.Fatalf("expected malformed query rejection, ok=%v value=%v status=%d", ok, value, rec.Code)
		}
	})

	t.Run("path propagates resolve errors", func(t *testing.T) {
		resolveErr := func(context.Context, string, string) (string, error) {
			return "", app.NewError("issue_not_found", "issue not found", store.ErrNotFound)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/issues/"+issueKey, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("issueID", issueKey)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		_, ok := pathIssueKey(rec, req, projectID, resolveErr)
		if ok || rec.Code != http.StatusNotFound {
			t.Fatalf("expected resolve error, ok=%v status=%d body=%s", ok, rec.Code, rec.Body.String())
		}
	})

	t.Run("query propagates resolve errors", func(t *testing.T) {
		resolveErr := func(context.Context, string, string) (string, error) {
			return "", app.NewError("issue_not_found", "issue not found", store.ErrNotFound)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/questions?issueId="+issueKey, nil)
		value, ok := queryIssueKey(rec, req, "issueId", projectID, resolveErr)
		if ok || value != nil || rec.Code != http.StatusNotFound {
			t.Fatalf("expected resolve error, ok=%v value=%v status=%d", ok, value, rec.Code)
		}
	})

	t.Run("body propagates resolve errors", func(t *testing.T) {
		resolveErr := func(context.Context, string, string) (string, error) {
			return "", app.NewError("issue_not_found", "issue not found", store.ErrNotFound)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/relationships", nil)
		_, ok := bodyIssueKey(rec, req, projectID, otherIssueKey, "targetIssueId", resolveErr)
		if ok || rec.Code != http.StatusNotFound {
			t.Fatalf("expected resolve error, ok=%v status=%d", ok, rec.Code)
		}
	})

	t.Run("body rejects malformed key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/relationships", nil)
		_, ok := bodyIssueKey(rec, req, projectID, "not-valid", "targetIssueId", resolve)
		if ok || rec.Code != http.StatusBadRequest {
			t.Fatalf("expected malformed body key rejection, ok=%v status=%d", ok, rec.Code)
		}
	})
}

func TestIssueKeyForUUIDFallsBackToUUID(t *testing.T) {
	if issueKeyForUUID(map[string]string{issueID: issueKey}, issueID) != issueKey {
		t.Fatalf("expected mapped key")
	}
	if issueKeyForUUID(map[string]string{}, issueID) != issueID {
		t.Fatalf("expected uuid fallback")
	}
}
