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
	if !strings.Contains(paths, "- BearerAuth: []") {
		t.Fatal("current-user endpoint must require bearer authentication")
	}

	schemaData, err := os.ReadFile(filepath.Join(root, "schemas", "auth.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	schema := string(schemaData)
	if !strings.Contains(schema, "forcePasswordChange: {type: boolean}") {
		t.Fatal("auth user schema must expose forced-password-change state")
	}
	if strings.Contains(schema, "passwordHash") || strings.Contains(schema, "refreshTokenHash") || strings.Contains(schema, "tokenHash") {
		t.Fatal("auth response schemas must not expose credential hashes")
	}
}
