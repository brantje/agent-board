package store

// ShouldAutoEnqueueIssue is the shared lifecycle-trigger policy for Issue
// mutations. Assignment includes initial ownership on creation. Callers supply
// the locked previous state; unchanged ownership is a retry, not a new
// assignment. Execution-configuration validity and pair-scoped duplicate
// suppression are checked transactionally before creating a Run. Scheduler
// admission (Runner/source/capacity availability) is deliberately not part of
// this predicate or Run-creation eligibility.
func ShouldAutoEnqueueIssue(previousStatus, status, assigneeType string, assignment bool) bool {
	if assigneeType != "AGENT" {
		return false
	}
	if assignment {
		return status != "BACKLOG"
	}
	return previousStatus == "BACKLOG" && status != "BACKLOG" && status != "DONE"
}
