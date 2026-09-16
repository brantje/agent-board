#!/usr/bin/env python3
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
BRANCH = "feat/136-squad-issue-ownership"
SUBJECT = "fix(#136): enforce Squad assignee scope in database"


def run(args, cwd=ROOT):
    print("+", " ".join(args), flush=True)
    subprocess.run(args, cwd=cwd, check=True)


def output(args, cwd=ROOT):
    return subprocess.check_output(args, cwd=cwd, text=True).strip()


def main():
    run(["git", "config", "user.name", "agent-board implementation"])
    run(["git", "config", "user.email", "agent-board-implementation@users.noreply.github.com"])
    subjects = output(["git", "log", "--format=%s", "origin/main..HEAD"]).splitlines()
    if SUBJECT not in subjects:
        schema = ROOT / "packages/database/schema.sql"
        text = schema.read_text()
        old = '''        ELSIF NEW.assignee_type = 'USER' THEN
            PERFORM 1 FROM users WHERE id = NEW.assignee_id;
            IF NOT FOUND THEN RAISE EXCEPTION 'invalid user assignee' USING ERRCODE = '23514'; END IF;
        END IF;'''
        new = '''        ELSIF NEW.assignee_type = 'USER' THEN
            PERFORM 1 FROM users WHERE id = NEW.assignee_id;
            IF NOT FOUND THEN RAISE EXCEPTION 'invalid user assignee' USING ERRCODE = '23514'; END IF;
        ELSIF NEW.assignee_type = 'SQUAD' THEN
            SELECT project_id INTO referenced_project_id FROM squads WHERE id = NEW.assignee_id;
            IF NOT FOUND THEN RAISE EXCEPTION 'invalid Squad assignee' USING ERRCODE = '23514'; END IF;
            IF referenced_project_id IS DISTINCT FROM NEW.project_id THEN
                RAISE EXCEPTION 'issue cannot reference Squad from another project' USING ERRCODE = '23514';
            END IF;
        END IF;'''
        if text.count(old) != 1:
            raise RuntimeError("configuration scope Issue assignee block changed unexpectedly")
        schema.write_text(text.replace(old, new, 1))

        test = ROOT / "apps/server/internal/store/postgres/squad_assignee_scope_integration_test.go"
        test.write_text(r'''package postgres

import (
    "testing"

    "github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSquadAssigneeDatabaseScopeGuard(t *testing.T) {
    s := New(testPool(t))
    ctx := t.Context()
    local := seedRunFixture(t, s, "squad-scope-local")
    foreign := seedRunFixture(t, s, "squad-scope-foreign")

    localSquad, err := s.CreateSquad(ctx, store.Squad{
        ProjectID: local.project.ID,
        Name: "Local Squad",
        LeaderAgentID: local.agent.ID,
    })
    if err != nil {
        t.Fatal(err)
    }
    foreignSquad, err := s.CreateSquad(ctx, store.Squad{
        ProjectID: foreign.project.ID,
        Name: "Foreign Squad",
        LeaderAgentID: foreign.agent.ID,
    })
    if err != nil {
        t.Fatal(err)
    }

    if _, err := s.pool.Exec(ctx, `UPDATE issues SET assignee_type='SQUAD',assignee_id=$2 WHERE id=$1`, local.issue.ID, localSquad.ID); err != nil {
        t.Fatalf("same-project Squad assignment rejected: %v", err)
    }
    issue, err := s.GetIssue(ctx, local.project.ID, local.issue.ID)
    if err != nil {
        t.Fatal(err)
    }
    assigned := issue.AssignedTo()
    if assigned == nil || assigned.Type != "SQUAD" || assigned.ID != localSquad.ID || assigned.Name != localSquad.Name {
        t.Fatalf("assigned=%+v", assigned)
    }

    if _, err := s.pool.Exec(ctx, `UPDATE issues SET assignee_type='SQUAD',assignee_id=$2 WHERE id=$1`, local.issue.ID, foreignSquad.ID); err == nil {
        t.Fatal("cross-project Squad assignment unexpectedly succeeded")
    }
    if _, err := s.pool.Exec(ctx, `UPDATE issues SET assignee_type='SQUAD',assignee_id='00000000-0000-4000-8000-000000000999' WHERE id=$1`, local.issue.ID); err == nil {
        t.Fatal("missing Squad assignment unexpectedly succeeded")
    }
}
''')
        run(["gofmt", "-w", str(test.relative_to(ROOT))])
        run(["go", "test", "./internal/store/postgres", "-run", "^TestSquadAssigneeDatabaseScopeGuard$"], ROOT / "apps/server")
        run(["git", "add", str(schema.relative_to(ROOT)), str(test.relative_to(ROOT))])
        run(["git", "commit", "-m", SUBJECT])
        sha = output(["git", "rev-parse", "HEAD"])
        run(["git", "push", "origin", f"HEAD:{BRANCH}"])
        print(f"COMMIT {SUBJECT}: {sha}", flush=True)

    run(["go", "test", "./internal/app", "./internal/store", "./internal/store/postgres", "./internal/httpapi", "./internal/mcpapi"], ROOT / "apps/server")
    run(["go", "vet", "./internal/app", "./internal/store/...", "./internal/httpapi", "./internal/mcpapi"], ROOT / "apps/server")

    run(["git", "rm", ".github/workflows/issue-136-scope-guard.yml", "scripts/issue136_scope_guard.py"])
    run(["git", "commit", "-m", "chore(#136): remove Squad scope guard harness"])
    cleanup_sha = output(["git", "rev-parse", "HEAD"])
    run(["git", "push", "origin", f"HEAD:{BRANCH}"])
    print(f"COMMIT cleanup: {cleanup_sha}", flush=True)


if __name__ == "__main__":
    main()
