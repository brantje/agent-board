package store

import "testing"

func TestIssueAutoEnqueueMatrix(t *testing.T) {
	statuses := []string{"BACKLOG", "TODO", "IN_PROGRESS", "BLOCKED", "REVIEW", "DONE"}
	for _, before := range statuses {
		for _, after := range statuses {
			for _, assignment := range []bool{false, true} {
				for _, kind := range []string{"AGENT", "USER", ""} {
					want := kind == "AGENT" && ((assignment && after != "BACKLOG") || (!assignment && before == "BACKLOG" && after != "BACKLOG" && after != "DONE"))
					if got := ShouldAutoEnqueueIssue(before, after, kind, assignment); got != want {
						t.Fatalf("%s -> %s kind=%s assignment=%v: %v want %v", before, after, kind, assignment, got, want)
					}
				}
			}
		}
	}
}
