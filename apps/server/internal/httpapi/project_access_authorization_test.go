package httpapi

import (
	"net/http"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestMinimumProjectRoleUsesFixedPhase4Semantics(t *testing.T) {
	tests := []struct {
		name   string
		method string
		tail   []string
		want   string
	}{
		{name: "project read", method: http.MethodGet, want: store.ProjectRoleViewer},
		{name: "project settings mutation", method: http.MethodPatch, want: store.ProjectRoleAdmin},
		{name: "issue read", method: http.MethodGet, tail: []string{"issues"}, want: store.ProjectRoleViewer},
		{name: "issue mutation", method: http.MethodPost, tail: []string{"issues"}, want: store.ProjectRoleMember},
		{name: "question answer", method: http.MethodPost, tail: []string{"questions", "q1", "answer"}, want: store.ProjectRoleMember},
		{name: "review approval", method: http.MethodPost, tail: []string{"reviews", "r1", "approve"}, want: store.ProjectRoleMember},
		{name: "access listing", method: http.MethodGet, tail: []string{"access", "users"}, want: store.ProjectRoleAdmin},
		{name: "access mutation", method: http.MethodPut, tail: []string{"access", "users", "u1"}, want: store.ProjectRoleAdmin},
		{name: "effective role read", method: http.MethodGet, tail: []string{"access", "effective-role"}, want: store.ProjectRoleViewer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := minimumProjectRole(tt.method, tt.tail); got != tt.want {
				t.Fatalf("minimumProjectRole(%q, %v) = %q, want %q", tt.method, tt.tail, got, tt.want)
			}
		})
	}
}
