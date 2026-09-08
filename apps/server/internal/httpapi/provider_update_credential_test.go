package httpapi

import (
	"context"
	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"net/http/httptest"
	"strings"
	"testing"
)

type captureProviderStore struct {
	fakeControlPlaneStore
	saved store.Provider
}

func (s *captureProviderStore) UpdateProvider(_ context.Context, p store.Provider) (store.Provider, error) {
	s.saved = p
	return p, nil
}
func TestProviderEditPreservesWriteOnlyCredentialUnlessReplaced(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"name":"Renamed","kind":"test"}`, "secret-ref"},
		{`{"name":"Renamed","kind":"test","credentialRef":"replacement"}`, "replacement"},
	} {
		s := &captureProviderStore{}
		w := httptest.NewRecorder()
		NewRouter(app.New(s)).ServeHTTP(w, httptest.NewRequest("PUT", "/api/providers/"+providerID, strings.NewReader(tc.body)))
		if w.Code != 200 {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		if s.saved.CredentialRef == nil || *s.saved.CredentialRef != tc.want {
			t.Fatalf("credential was lost or not replaced: %v", s.saved.CredentialRef)
		}
		if strings.Contains(w.Body.String(), tc.want) {
			t.Fatal("write-only reference leaked in response")
		}
	}
}
