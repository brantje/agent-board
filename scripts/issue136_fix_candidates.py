#!/usr/bin/env python3
from pathlib import Path
import subprocess

SUBJECT = "fix(#136): close execution candidate rows before lookup"
BRANCH = "feat/136-squad-issue-ownership"


def run(*args: str, cwd: str | None = None) -> str:
    result = subprocess.run(args, cwd=cwd, check=True, text=True, capture_output=True)
    if result.stdout:
        print(result.stdout, end="")
    if result.stderr:
        print(result.stderr, end="")
    return result.stdout.strip()


subjects = run("git", "log", "--format=%s", "origin/main..HEAD").splitlines()
if SUBJECT not in subjects:
    path = Path("apps/server/internal/store/postgres/issue_execution.go")
    text = path.read_text()
    start = text.index("func resolvedIssueExecutionCandidates(")
    end = text.index("\nfunc executionAgentMatchesFilter", start)
    replacement = r'''func resolvedIssueExecutionCandidates(ctx context.Context, q issueExecutionCandidateQuerier, filter store.IssueExecutionFilter) ([]issueExecutionCandidate, error) {
	rows, err := q.Query(ctx, `
		SELECT project_id::text, id::text, assignee_type, assignee_id::text
		FROM issues
		WHERE ($1='' OR project_id::text=$1)
		  AND assignee_type IN ('AGENT','SQUAD')
		  AND assignee_id IS NOT NULL
		ORDER BY project_id, id
	`, filter.ProjectID)
	if err != nil {
		return nil, err
	}
	type ownerCandidate struct {
		projectID string
		issueID   string
		ownerType string
		ownerID   string
	}
	owners := make([]ownerCandidate, 0)
	for rows.Next() {
		var owner ownerCandidate
		if err := rows.Scan(&owner.projectID, &owner.issueID, &owner.ownerType, &owner.ownerID); err != nil {
			rows.Close()
			return nil, err
		}
		owners = append(owners, owner)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}

	candidates := make([]issueExecutionCandidate, 0, len(owners))
	for _, owner := range owners {
		issue := store.Issue{ProjectID: owner.projectID, ID: owner.issueID, AssigneeType: &owner.ownerType, AssigneeID: &owner.ownerID}
		agentID, executable, err := resolveIssueExecutionAgent(ctx, q, issue)
		if err != nil {
			return nil, err
		}
		if !executable || (filter.AgentID != "" && agentID != filter.AgentID) {
			continue
		}
		matches, err := executionAgentMatchesFilter(ctx, q, owner.projectID, agentID, filter)
		if err != nil {
			return nil, err
		}
		if matches {
			candidates = append(candidates, issueExecutionCandidate{projectID: owner.projectID, issueID: owner.issueID, agentID: agentID})
		}
	}
	return candidates, nil
}
'''
    path.write_text(text[:start] + replacement + text[end:])
    run("gofmt", "-w", str(path))
    run("go", "test", "./internal/store/postgres", "-run", "^TestExecutionConfigurationChangeReconcilesOnlyAffectedAssignments$", cwd="apps/server")
    run("git", "config", "user.name", "agent-board implementation")
    run("git", "config", "user.email", "agent-board-implementation@users.noreply.github.com")
    run("git", "add", str(path))
    run("git", "commit", "-m", SUBJECT)
    sha = run("git", "rev-parse", "HEAD")
    run("git", "push", "origin", f"HEAD:{BRANCH}")
    print(f"COMMIT {SUBJECT}: {sha}")
else:
    print(f"skip existing: {SUBJECT}")
