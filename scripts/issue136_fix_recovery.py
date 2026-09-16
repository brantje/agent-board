#!/usr/bin/env python3
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
BRANCH = "feat/136-squad-issue-ownership"


def run(args, cwd=ROOT):
    print("+", " ".join(args), flush=True)
    subprocess.run(args, cwd=cwd, check=True)


def output(args, cwd=ROOT):
    return subprocess.check_output(args, cwd=cwd, text=True)


def has_commit(subject):
    return subject in output(["git", "log", "--format=%s", "origin/main..HEAD"]).splitlines()


def patch_recovery():
    subject = "fix(#136): close execution candidate rows before lookup"
    if has_commit(subject):
        print(f"skip existing: {subject}", flush=True)
        return

    p = ROOT / "apps/server/internal/store/postgres/issue_execution.go"
    text = p.read_text()
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
    p.write_text(text[:start] + replacement + text[end:])
    run(["gofmt", "-w", str(p.relative_to(ROOT))])
    run(["go", "test", "./internal/store/postgres", "-run", "^TestExecutionConfigurationChangeReconcilesOnlyAffectedAssignments$"], ROOT / "apps/server")
    run(["git", "add", str(p.relative_to(ROOT))])
    run(["git", "commit", "-m", subject])
    sha = output(["git", "rev-parse", "HEAD"]).strip()
    run(["git", "push", "origin", f"HEAD:{BRANCH}"])
    print(f"COMMIT {subject}: {sha}", flush=True)


def run_main():
    p = ROOT / "scripts/issue136_implement.py"
    text = p.read_text()
    text = text.replace('"main..HEAD"', '"origin/main..HEAD"')
    text = text.replace("old = '`\\\"type\\\": \\\"USER\\\" | \\\"AGENT\\\"`'", "old = '\\\"type\\\": \\\"USER\\\" | \\\"AGENT\\\"'")
    text = text.replace("p.write_text(text.replace(old, '`\\\"type\\\": \\\"USER\\\" | \\\"AGENT\\\" | \\\"SQUAD\\\"`', 1))", "p.write_text(text.replace(old, '\\\"type\\\": \\\"USER\\\" | \\\"AGENT\\\" | \\\"SQUAD\\\"', 1))")
    p.write_text(text)
    run(["python3", str(p.relative_to(ROOT))])


def cleanup_helper():
    path = "scripts/issue136_fix_recovery.py"
    if not (ROOT / path).exists():
        return
    run(["git", "rm", path])
    run(["git", "commit", "-m", "chore(#136): remove recovery verification helper"])
    sha = output(["git", "rev-parse", "HEAD"]).strip()
    run(["git", "push", "origin", f"HEAD:{BRANCH}"])
    print(f"COMMIT cleanup helper: {sha}", flush=True)


def main():
    run(["git", "config", "user.name", "agent-board implementation"])
    run(["git", "config", "user.email", "agent-board-implementation@users.noreply.github.com"])
    patch_recovery()
    run_main()
    cleanup_helper()


if __name__ == "__main__":
    main()
