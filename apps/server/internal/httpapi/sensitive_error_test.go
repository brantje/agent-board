package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthenticationSecretsAreNotEchoedInErrorPayloads(t *testing.T) {
	now := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	const (
		passwordSentinel = "PASSWORD-SENTINEL-79"
		accessSentinel   = "ACCESS-TOKEN-SENTINEL-79"
		refreshSentinel  = "REFRESH-TOKEN-SENTINEL-79"
	)

	responses := []*httptest.ResponseRecorder{
		authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"missing-user","password":"`+passwordSentinel+`"}`, nil),
		authHTTPRequest(t, handler, http.MethodPost, "/api/auth/refresh", `{"refreshToken":"`+refreshSentinel+`"}`, nil),
		authHTTPRequest(t, handler, http.MethodGet, "/api/projects", "", map[string]string{"Authorization": "Bearer " + accessSentinel}),
	}
	for index, response := range responses {
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("response %d status=%d body=%s", index, response.Code, response.Body.String())
		}
		body := response.Body.String()
		for _, secret := range []string{passwordSentinel, accessSentinel, refreshSentinel} {
			if strings.Contains(body, secret) {
				t.Fatalf("response %d leaked %q: %s", index, secret, body)
			}
		}
	}
}
