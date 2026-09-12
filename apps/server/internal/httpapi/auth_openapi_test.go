package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthOpenAPIContractsAreIntentional(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "packages", "api")
	openAPIData, err := os.ReadFile(filepath.Join(root, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	openAPI := string(openAPIData)
	for _, route := range []string{
		"/api/auth/bootstrap:",
		"/api/auth/bootstrap/register:",
		"/api/auth/login:",
		"/api/auth/refresh:",
		"/api/auth/logout:",
		"/api/auth/setup/complete:",
		"/api/auth/reset/complete:",
		"/api/auth/me:",
		"/api/auth/users:",
		"/api/auth/users/{userID}/setup-token:",
		"/api/auth/users/{userID}/reset-token:",
		"/api/auth/users/{userID}/password:",
		"/api/auth/users/{userID}/disable:",
		"/api/auth/users/{userID}/enable:",
		"/api/auth/me/password:",
		"/api/auth/me/sessions:",
		"/api/auth/me/sessions/{sessionID}:",
		"/api/auth/me/sessions/logout-others:",
		"/api/auth/settings:",
	} {
		if !strings.Contains(openAPI, route) {
			t.Fatalf("OpenAPI missing auth route %s", route)
		}
	}
	if !strings.Contains(openAPI, "BearerAuth:") || !strings.Contains(openAPI, "scheme: bearer") {
		t.Fatal("OpenAPI must register JWT bearer authentication")
	}

	pathsData, err := os.ReadFile(filepath.Join(root, "paths", "auth.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	paths := string(pathsData)
	for _, secretField := range []string{
		"password: {type: string, minLength: 1, writeOnly: true}",
		"refreshToken: {type: string, minLength: 1, writeOnly: true}",
		"token: {type: string, minLength: 1, writeOnly: true}",
	} {
		if !strings.Contains(paths, secretField) {
			t.Fatalf("auth OpenAPI must mark secret write-only: %s", secretField)
		}
	}
	if !strings.Contains(paths, "Cache-Control:") || !strings.Contains(paths, "const: no-store") {
		t.Fatal("token responses must document Cache-Control: no-store")
	}
	if !strings.Contains(paths, "security: [{BearerAuth: []}]") {
		t.Fatal("authenticated phase 2 endpoints must require bearer authentication")
	}
	for _, operation := range []string{"updateAuthenticatedUser", "listDeploymentUsers", "changeAuthenticatedUserPassword", "listAuthenticatedUserSessions", "updateAuthenticationSettings"} {
		if !strings.Contains(paths, "operationId: "+operation) {
			t.Fatalf("auth OpenAPI missing phase 2 operation %s", operation)
		}
	}

	schemaData, err := os.ReadFile(filepath.Join(root, "schemas", "auth.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	schema := string(schemaData)
	for _, field := range []string{"forcePasswordChange: {type: boolean}", "AuthSession:", "AuthSettings:", "PendingUserSecret:", "PasswordTokenSecret:"} {
		if !strings.Contains(schema, field) {
			t.Fatalf("auth schema missing %s", field)
		}
	}
	if strings.Contains(schema, "passwordHash") || strings.Contains(schema, "refreshTokenHash") || strings.Contains(schema, "tokenHash") {
		t.Fatal("auth response schemas must not expose credential hashes")
	}
}
