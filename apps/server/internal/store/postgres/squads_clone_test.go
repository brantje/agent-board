package postgres

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestCloneSquadMembersDeepCopiesRole(t *testing.T) {
	role := "worker"
	members := []store.SquadMember{{AgentID: "agent-1", Role: &role}}

	cloned := cloneSquadMembers(members)
	role = "changed"
	members[0].AgentID = "agent-2"

	if len(cloned) != 1 {
		t.Fatalf("cloned members = %d, want 1", len(cloned))
	}
	if cloned[0].AgentID != "agent-1" {
		t.Fatalf("cloned agent id = %q, want agent-1", cloned[0].AgentID)
	}
	if cloned[0].Role == nil || *cloned[0].Role != "worker" {
		t.Fatalf("cloned role = %v, want worker", cloned[0].Role)
	}
}
