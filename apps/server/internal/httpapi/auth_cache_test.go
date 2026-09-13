package httpapi

import (
	"net/http"
	"testing"
	"time"
)

func TestAuthMeResponseIsNotCacheable(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	tokens := loginAuthHTTPUser(t, handler)

	response := authHTTPRequest(t, handler, http.MethodGet, "/api/auth/me", "", map[string]string{
		"Authorization": "Bearer " + tokens.AccessToken,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("me Cache-Control=%q, want no-store", response.Header().Get("Cache-Control"))
	}
}
