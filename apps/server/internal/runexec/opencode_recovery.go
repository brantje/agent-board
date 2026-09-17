package runexec

// ResetAttachedProcess clears the recovered Execution Session binding after an
// Engine has deliberately stopped an attached process because its effective
// capabilities no longer match the current Run. A subsequent Start therefore
// creates a fresh Execution Session instead of trying to restart the terminal
// attached session.
func (l *processLauncher) ResetAttachedProcess() {
	if l != nil {
		l.attachSessionID = ""
	}
}
