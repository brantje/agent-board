package store

// ShouldAutoEnqueueIssue is the shared policy for Issue mutations. Assignment
// includes initial ownership on creation. Callers supply the locked previous
// state; unchanged ownership is a retry, not a new assignment. Readiness and
// duplicate suppression are checked transactionally before creating a Run.
func ShouldAutoEnqueueIssue(previousStatus, status, assigneeType string, assignment bool) bool {
	if assigneeType != "AGENT" {
		return false
	}
	if assignment {
		return status != "BACKLOG"
	}
	return previousStatus == "BACKLOG" && status != "BACKLOG" && status != "DONE"
}
