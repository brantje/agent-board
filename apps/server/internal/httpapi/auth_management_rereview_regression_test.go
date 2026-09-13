package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestAuthPhase2HTTPSelfPasswordChangeRequiresCurrentPasswordUnlessForced(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	login := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + login.AccessToken}

	missing := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/me/password", `{"newPassword":"replacement-long-password"}`, headers)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing current password status=%d body=%s", missing.Code, missing.Body.String())
	}

	wrong := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/me/password", `{"currentPassword":"wrong-long-password","newPassword":"replacement-long-password"}`, headers)
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password status=%d body=%s", wrong.Code, wrong.Body.String())
	}

	correct := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/me/password", `{"currentPassword":"long-enough-password","newPassword":"replacement-long-password"}`, headers)
	if correct.Code != http.StatusOK {
		t.Fatalf("correct current password status=%d body=%s", correct.Code, correct.Body.String())
	}

	oldLogin := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"admin","password":"long-enough-password"}`, nil)
	if oldLogin.Code != http.StatusUnauthorized {
		t.Fatalf("old password remained valid status=%d body=%s", oldLogin.Code, oldLogin.Body.String())
	}
	newLogin := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"admin","password":"replacement-long-password"}`, nil)
	if newLogin.Code != http.StatusOK {
		t.Fatalf("new password login status=%d body=%s", newLogin.Code, newLogin.Body.String())
	}
}

func TestAuthPhase2HTTPForcedPasswordChangeAcceptsNewPasswordOnly(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler := newPhase2AuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + admin.AccessToken}

	assigned := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/users/00000000-0000-0000-0000-000000000001/password", `{"password":"temporary-admin-password"}`, headers)
	if assigned.Code != http.StatusOK {
		t.Fatalf("assign temporary password status=%d body=%s", assigned.Code, assigned.Body.String())
	}
	forcedLogin := authHTTPRequest(t, handler, http.MethodPost, "/api/auth/login", `{"login":"admin","password":"temporary-admin-password"}`, nil)
	if forcedLogin.Code != http.StatusOK {
		t.Fatalf("forced login status=%d body=%s", forcedLogin.Code, forcedLogin.Body.String())
	}
	var forced authTokensResponse
	if err := json.Unmarshal(forcedLogin.Body.Bytes(), &forced); err != nil {
		t.Fatal(err)
	}
	forcedHeaders := map[string]string{"Authorization": "Bearer " + forced.AccessToken}

	changed := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/me/password", `{"newPassword":"permanent-admin-password"}`, forcedHeaders)
	if changed.Code != http.StatusOK {
		t.Fatalf("forced password completion status=%d body=%s", changed.Code, changed.Body.String())
	}
}

func TestAuthPhase2HTTPSettingsRejectsEveryOmittedRequiredField(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + admin.AccessToken}

	base := map[string]any{
		"accessTokenLifetimeSeconds": 3600,
		"refreshTokenLifetimeSeconds": 2592000,
		"minimumPasswordLength": 12,
		"requireUppercase": false,
		"requireLowercase": false,
		"requireNumber": false,
		"requireSymbol": false,
	}
	for _, field := range []string{
		"accessTokenLifetimeSeconds",
		"refreshTokenLifetimeSeconds",
		"minimumPasswordLength",
		"requireUppercase",
		"requireLowercase",
		"requireNumber",
		"requireSymbol",
	} {
		t.Run(field, func(t *testing.T) {
			body := make(map[string]any, len(base)-1)
			for key, value := range base {
				if key != field {
					body[key] = value
				}
			}
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			response := authHTTPRequest(t, handler, http.MethodPut, "/api/auth/settings", string(encoded), headers)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("omitting %s status=%d body=%s", field, response.Code, response.Body.String())
			}
		})
	}
}

func TestAuthPhase2HTTPPrivilegedGETResponsesAreNoStore(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	handler, _, _ := newAuthHTTPHandler(t, &now)
	registerAuthHTTPUser(t, handler)
	admin := loginAuthHTTPUser(t, handler)
	headers := map[string]string{"Authorization": "Bearer " + admin.AccessToken}

	for _, path := range []string{"/api/auth/users", "/api/auth/settings"} {
		response := authHTTPRequest(t, handler, http.MethodGet, path, "", headers)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("GET %s Cache-Control=%q want no-store", path, got)
		}
	}
}
