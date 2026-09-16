#!/usr/bin/env python3
from pathlib import Path
import subprocess
import textwrap

ROOT = Path(__file__).resolve().parents[1]
BRANCH = "feat/136-squad-issue-ownership"


def replace_once(path: str, old: str, new: str) -> None:
    file = ROOT / path
    text = file.read_text()
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"{path}: expected one replacement, found {count}")
    file.write_text(text.replace(old, new, 1))


def write(path: str, content: str) -> None:
    (ROOT / path).write_text(textwrap.dedent(content).lstrip())


def run(*args: str, cwd: Path | None = None) -> None:
    subprocess.run(args, cwd=cwd or ROOT, check=True)


# Existing generic recovery gains only the concrete canonical-owner filter needed
# for leader-change reconciliation.
replace_once(
    "apps/server/internal/store/issue_execution.go",
    """// Empty filters select all current Agent assignments during startup recovery.\n// Configuration changes narrow the scan to the affected dependency.\ntype IssueExecutionFilter struct {\n\tProjectID      string\n\tAgentID        string\n\tModelProfileID string\n\tProviderID     string\n}\n""",
    """// Empty filters select all current executable Issue ownership during startup recovery.\n// Configuration changes narrow the scan to the affected dependency; SquadID\n// narrows reconciliation to one canonical Squad owner.\ntype IssueExecutionFilter struct {\n\tProjectID      string\n\tAgentID        string\n\tSquadID        string\n\tModelProfileID string\n\tProviderID     string\n}\n""",
)

replace_once(
    "apps/server/internal/store/postgres/issue_execution.go",
    """\trows, err := q.Query(ctx, `\n\t\tSELECT project_id::text, id::text, assignee_type, assignee_id::text\n\t\tFROM issues\n\t\tWHERE ($1='' OR project_id::text=$1)\n\t\t  AND assignee_type IN ('AGENT','SQUAD')\n\t\t  AND assignee_id IS NOT NULL\n\t\tORDER BY project_id, id\n\t`, filter.ProjectID)\n""",
    """\trows, err := q.Query(ctx, `\n\t\tSELECT project_id::text, id::text, assignee_type, assignee_id::text\n\t\tFROM issues\n\t\tWHERE ($1='' OR project_id::text=$1)\n\t\t  AND ($2='' OR (assignee_type='SQUAD' AND assignee_id::text=$2))\n\t\t  AND assignee_type IN ('AGENT','SQUAD')\n\t\t  AND assignee_id IS NOT NULL\n\t\tORDER BY project_id, id\n\t`, filter.ProjectID, filter.SquadID)\n""",
)

# Report whether leadership changed from the row already locked by UpdateSquad.
replace_once(
    "apps/server/internal/store/squads.go",
    "\tUpdateSquad(context.Context, Squad) (Squad, error)\n",
    "\tUpdateSquad(context.Context, Squad) (Squad, bool, error)\n",
)

replace_once(
    "apps/server/internal/store/postgres/squads.go",
    """func (s *Store) UpdateSquad(ctx context.Context, input store.Squad) (store.Squad, error) {\n\ttx, err := s.pool.Begin(ctx)\n\tif err != nil {\n\t\treturn store.Squad{}, err\n\t}\n\tdefer tx.Rollback(ctx) //nolint:errcheck\n\n\tvar squadID string\n\tif err := tx.QueryRow(ctx, `\n\t\tSELECT id::text\n\t\tFROM squads\n\t\tWHERE project_id = $1 AND id = $2\n\t\tFOR UPDATE\n\t`, input.ProjectID, input.ID).Scan(&squadID); err != nil {\n\t\treturn store.Squad{}, notFound(err)\n\t}\n\tif err := lockSquadAgents(ctx, tx, input); err != nil {\n\t\treturn store.Squad{}, err\n\t}\n\tif _, err := tx.Exec(ctx, `DELETE FROM squad_members WHERE squad_id = $1`, squadID); err != nil {\n\t\treturn store.Squad{}, err\n\t}\n\n\tvalue, err := scanSquad(tx.QueryRow(ctx, `\n\t\tUPDATE squads\n\t\tSET name = $3, leader_agent_id = $4, updated_at = now()\n\t\tWHERE project_id = $1 AND id = $2\n\t\tRETURNING `+squadSelectColumns+`\n\t`, input.ProjectID, input.ID, input.Name, input.LeaderAgentID))\n\tif err != nil {\n\t\treturn store.Squad{}, err\n\t}\n\tif err := insertSquadMembers(ctx, tx, value.ID, input.Members); err != nil {\n\t\treturn store.Squad{}, err\n\t}\n\tif err := tx.Commit(ctx); err != nil {\n\t\treturn store.Squad{}, err\n\t}\n\tvalue.Members = cloneSquadMembers(input.Members)\n\treturn value, nil\n}\n""",
    """func (s *Store) UpdateSquad(ctx context.Context, input store.Squad) (store.Squad, bool, error) {\n\ttx, err := s.pool.Begin(ctx)\n\tif err != nil {\n\t\treturn store.Squad{}, false, err\n\t}\n\tdefer tx.Rollback(ctx) //nolint:errcheck\n\n\tvar squadID, previousLeaderAgentID string\n\tif err := tx.QueryRow(ctx, `\n\t\tSELECT id::text, leader_agent_id::text\n\t\tFROM squads\n\t\tWHERE project_id = $1 AND id = $2\n\t\tFOR UPDATE\n\t`, input.ProjectID, input.ID).Scan(&squadID, &previousLeaderAgentID); err != nil {\n\t\treturn store.Squad{}, false, notFound(err)\n\t}\n\tif err := lockSquadAgents(ctx, tx, input); err != nil {\n\t\treturn store.Squad{}, false, err\n\t}\n\tif _, err := tx.Exec(ctx, `DELETE FROM squad_members WHERE squad_id = $1`, squadID); err != nil {\n\t\treturn store.Squad{}, false, err\n\t}\n\n\tvalue, err := scanSquad(tx.QueryRow(ctx, `\n\t\tUPDATE squads\n\t\tSET name = $3, leader_agent_id = $4, updated_at = now()\n\t\tWHERE project_id = $1 AND id = $2\n\t\tRETURNING `+squadSelectColumns+`\n\t`, input.ProjectID, input.ID, input.Name, input.LeaderAgentID))\n\tif err != nil {\n\t\treturn store.Squad{}, false, err\n\t}\n\tif err := insertSquadMembers(ctx, tx, value.ID, input.Members); err != nil {\n\t\treturn store.Squad{}, false, err\n\t}\n\tif err := tx.Commit(ctx); err != nil {\n\t\treturn store.Squad{}, false, err\n\t}\n\tvalue.Members = cloneSquadMembers(input.Members)\n\treturn value, previousLeaderAgentID != value.LeaderAgentID, nil\n}\n""",
)

replace_once(
    "apps/server/internal/app/squads.go",
    """\tvalue, err := s.store.UpdateSquad(ctx, prepared)\n\tif err == nil {\n\t\ts.reconcileExecutionConfiguration(ctx, store.IssueExecutionFilter{ProjectID: value.ProjectID, AgentID: value.LeaderAgentID})\n\t}\n\treturn value, translateStoreError(err, \"squad\")\n""",
    """\tvalue, leaderChanged, err := s.store.UpdateSquad(ctx, prepared)\n\tif err == nil && leaderChanged {\n\t\ts.reconcileExecutionConfiguration(ctx, store.IssueExecutionFilter{\n\t\t\tProjectID: value.ProjectID,\n\t\t\tAgentID:   value.LeaderAgentID,\n\t\t\tSquadID:   value.ID,\n\t\t})\n\t}\n\treturn value, translateStoreError(err, \"squad\")\n""",
)

replace_once(
    "apps/server/internal/app/squads_test.go",
    """func (s *squadTestStore) UpdateSquad(_ context.Context, value store.Squad) (store.Squad, error) {\n\ts.updateCalls++\n\texisting, ok := s.squads[value.ID]\n\tif !ok || existing.ProjectID != value.ProjectID {\n\t\treturn store.Squad{}, store.ErrNotFound\n\t}\n\tvalue.Members = cloneTestSquadMembers(value.Members)\n\ts.squads[value.ID] = value\n\treturn cloneTestSquad(value), nil\n}\n""",
    """func (s *squadTestStore) UpdateSquad(_ context.Context, value store.Squad) (store.Squad, bool, error) {\n\ts.updateCalls++\n\texisting, ok := s.squads[value.ID]\n\tif !ok || existing.ProjectID != value.ProjectID {\n\t\treturn store.Squad{}, false, store.ErrNotFound\n\t}\n\tleaderChanged := existing.LeaderAgentID != value.LeaderAgentID\n\tvalue.Members = cloneTestSquadMembers(value.Members)\n\ts.squads[value.ID] = value\n\treturn cloneTestSquad(value), leaderChanged, nil\n}\n""",
)

replace_once(
    "apps/server/internal/httpapi/squads_test.go",
    """func (s *squadHTTPStore) UpdateSquad(_ context.Context, value store.Squad) (store.Squad, error) {\n\tprevious, ok := s.squads[value.ID]\n\tif !ok || previous.ProjectID != value.ProjectID {\n\t\treturn store.Squad{}, store.ErrNotFound\n\t}\n\tvalue.CreatedAt = previous.CreatedAt\n\tvalue.UpdatedAt = time.Unix(2, 0).UTC()\n\ts.squads[value.ID] = value\n\treturn value, nil\n}\n""",
    """func (s *squadHTTPStore) UpdateSquad(_ context.Context, value store.Squad) (store.Squad, bool, error) {\n\tprevious, ok := s.squads[value.ID]\n\tif !ok || previous.ProjectID != value.ProjectID {\n\t\treturn store.Squad{}, false, store.ErrNotFound\n\t}\n\tleaderChanged := previous.LeaderAgentID != value.LeaderAgentID\n\tvalue.CreatedAt = previous.CreatedAt\n\tvalue.UpdatedAt = time.Unix(2, 0).UTC()\n\ts.squads[value.ID] = value\n\treturn value, leaderChanged, nil\n}\n""",
)

replace_once(
    "apps/server/internal/store/postgres/squads_integration_test.go",
    "\tupdated, err := s.UpdateSquad(ctx, store.Squad{\n",
    "\tupdated, leaderChanged, err := s.UpdateSquad(ctx, store.Squad{\n",
)
replace_once(
    "apps/server/internal/store/postgres/squads_integration_test.go",
    """\tif err != nil {\n\t\tt.Fatalf(\"update squad: %v\", err)\n\t}\n\tif updated.Name != \"Platform\" || updated.LeaderAgentID != agentsA[1].ID {\n""",
    """\tif err != nil {\n\t\tt.Fatalf(\"update squad: %v\", err)\n\t}\n\tif !leaderChanged {\n\t\tt.Fatal(\"leader change was not reported\")\n\t}\n\tif updated.Name != \"Platform\" || updated.LeaderAgentID != agentsA[1].ID {\n""",
)
replace_once(
    "apps/server/internal/store/postgres/squads_integration_test.go",
    "\tif _, err := s.UpdateSquad(ctx, store.Squad{\n",
    "\tif _, _, err := s.UpdateSquad(ctx, store.Squad{\n",
)
replace_once(
    "apps/server/internal/store/postgres/squads_read_consistency_integration_test.go",
    "\t\t\tif _, err := s.UpdateSquad(ctx, store.Squad{\n",
    "\t\t\tif _, _, err := s.UpdateSquad(ctx, store.Squad{\n",
)

write(
    "apps/server/internal/store/postgres/squad_leader_reconciliation_integration_test.go",
    r'''
    package postgres

    import (
        "testing"

        "github.com/brantje/agent-board/apps/server/internal/app"
        "github.com/brantje/agent-board/apps/server/internal/store"
    )

    func TestSquadLeaderChangeReconcilesOnlyThatSquad(t *testing.T) {
        s := New(testPool(t))
        f := seedRunFixture(t, s, "squad-leader-scope")
        ctx := t.Context()
        newLeader := createSquadExecutionMember(t, s, f, "squad-scope-new-leader")

        squad, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID})
        if err != nil {
            t.Fatal(err)
        }
        otherSquad, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Other", LeaderAgentID: newLeader.ID})
        if err != nil {
            t.Fatal(err)
        }
        if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=false WHERE id=$1`, f.model.ID); err != nil {
            t.Fatal(err)
        }

        squadOwner := "SQUAD"
        agentOwner := "AGENT"
        squadIssue, err := s.CreateIssue(ctx, store.Issue{
            ProjectID: f.project.ID, Title: "owned by changed squad", Status: "TODO",
            AssigneeType: &squadOwner, AssigneeID: &squad.ID,
        })
        if err != nil {
            t.Fatal(err)
        }
        directIssue, err := s.CreateIssue(ctx, store.Issue{
            ProjectID: f.project.ID, Title: "owned directly by new leader", Status: "TODO",
            AssigneeType: &agentOwner, AssigneeID: &newLeader.ID,
        })
        if err != nil {
            t.Fatal(err)
        }
        otherSquadIssue, err := s.CreateIssue(ctx, store.Issue{
            ProjectID: f.project.ID, Title: "owned by other squad", Status: "TODO",
            AssigneeType: &squadOwner, AssigneeID: &otherSquad.ID,
        })
        if err != nil {
            t.Fatal(err)
        }
        if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=true WHERE id=$1`, f.model.ID); err != nil {
            t.Fatal(err)
        }

        service := app.New(s)
        squad.LeaderAgentID = newLeader.ID
        if _, err := service.UpdateSquad(ctx, squad); err != nil {
            t.Fatal(err)
        }

        assertIssueAgentRunCount(t, s, squadIssue.ID, newLeader.ID, 1)
        assertIssueRunCount(t, s, directIssue.ID, 0)
        assertIssueRunCount(t, s, otherSquadIssue.ID, 0)
        persisted, err := s.GetIssue(ctx, f.project.ID, squadIssue.ID)
        if err != nil || persisted.AssignedTo() == nil || persisted.AssignedTo().Type != "SQUAD" || persisted.AssignedTo().ID != squad.ID {
            t.Fatalf("ownership=%+v err=%v", persisted.AssignedTo(), err)
        }
    }

    func TestSquadNonLeaderUpdateDoesNotReconcileExecution(t *testing.T) {
        s := New(testPool(t))
        f := seedRunFixture(t, s, "squad-non-leader-update")
        ctx := t.Context()
        member := createSquadExecutionMember(t, s, f, "squad-non-leader-member")
        squad, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID})
        if err != nil {
            t.Fatal(err)
        }
        if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=false WHERE id=$1`, f.model.ID); err != nil {
            t.Fatal(err)
        }
        ownerType := "SQUAD"
        issue, err := s.CreateIssue(ctx, store.Issue{
            ProjectID: f.project.ID, Title: "parked until config recovery", Status: "TODO",
            AssigneeType: &ownerType, AssigneeID: &squad.ID,
        })
        if err != nil {
            t.Fatal(err)
        }
        if _, err := s.pool.Exec(ctx, `UPDATE model_profiles SET enabled=true WHERE id=$1`, f.model.ID); err != nil {
            t.Fatal(err)
        }

        squad.Name = "Renamed Backend"
        squad.Members = []store.SquadMember{{AgentID: member.ID}}
        if _, err := app.New(s).UpdateSquad(ctx, squad); err != nil {
            t.Fatal(err)
        }
        assertIssueRunCount(t, s, issue.ID, 0)
    }

    func TestSquadLeaderChangeKeepsOldRunsAndSuppressesExistingNewLeaderPair(t *testing.T) {
        s := New(testPool(t))
        f := seedRunFixture(t, s, "squad-leader-existing-pair")
        ctx := t.Context()
        newLeader := createSquadExecutionMember(t, s, f, "squad-existing-pair-new-leader")
        squad, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID})
        if err != nil {
            t.Fatal(err)
        }
        ownerType := "SQUAD"
        issue, err := s.CreateIssue(ctx, store.Issue{
            ProjectID: f.project.ID, Title: "leader changes", Status: "TODO",
            AssigneeType: &ownerType, AssigneeID: &squad.ID,
        })
        if err != nil {
            t.Fatal(err)
        }
        service := app.New(s)

        squad.LeaderAgentID = newLeader.ID
        if _, err := service.UpdateSquad(ctx, squad); err != nil {
            t.Fatal(err)
        }
        assertIssueAgentRunCount(t, s, issue.ID, f.agent.ID, 1)
        assertIssueAgentRunCount(t, s, issue.ID, newLeader.ID, 1)

        squad.LeaderAgentID = f.agent.ID
        if _, err := service.UpdateSquad(ctx, squad); err != nil {
            t.Fatal(err)
        }
        squad.LeaderAgentID = newLeader.ID
        if _, err := service.UpdateSquad(ctx, squad); err != nil {
            t.Fatal(err)
        }
        assertIssueRunCount(t, s, issue.ID, 2)
        assertIssueAgentRunCount(t, s, issue.ID, f.agent.ID, 1)
        assertIssueAgentRunCount(t, s, issue.ID, newLeader.ID, 1)

        persisted, err := s.GetIssue(ctx, f.project.ID, issue.ID)
        if err != nil || persisted.AssignedTo() == nil || persisted.AssignedTo().Type != "SQUAD" || persisted.AssignedTo().ID != squad.ID {
            t.Fatalf("ownership=%+v err=%v", persisted.AssignedTo(), err)
        }
    }

    func TestSquadLeaderChangeUnavailableConfigRecoversThroughGenericPath(t *testing.T) {
        s := New(testPool(t))
        f := seedRunFixture(t, s, "squad-leader-config-recovery")
        ctx := t.Context()
        disabledModel, err := s.CreateModelProfile(ctx, store.ModelProfile{
            ProjectID: &f.project.ID, ProviderID: f.provider.ID, Name: "disabled-leader-model", Model: "test", Enabled: false,
        })
        if err != nil {
            t.Fatal(err)
        }
        newLeader, err := s.CreateAgent(ctx, store.Agent{
            ProjectID: &f.project.ID, Name: "disabled-config-leader", Engine: "scripted",
            ModelProfileID: disabledModel.ID, EngineSettings: store.EmptyObject,
        })
        if err != nil {
            t.Fatal(err)
        }
        squad, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Backend", LeaderAgentID: f.agent.ID})
        if err != nil {
            t.Fatal(err)
        }
        ownerType := "SQUAD"
        issue, err := s.CreateIssue(ctx, store.Issue{
            ProjectID: f.project.ID, Title: "recover new leader", Status: "TODO",
            AssigneeType: &ownerType, AssigneeID: &squad.ID,
        })
        if err != nil {
            t.Fatal(err)
        }
        service := app.New(s)

        squad.LeaderAgentID = newLeader.ID
        if _, err := service.UpdateSquad(ctx, squad); err != nil {
            t.Fatal(err)
        }
        assertIssueAgentRunCount(t, s, issue.ID, f.agent.ID, 1)
        assertIssueAgentRunCount(t, s, issue.ID, newLeader.ID, 0)
        persisted, err := s.GetIssue(ctx, f.project.ID, issue.ID)
        if err != nil || persisted.AssignedTo() == nil || persisted.AssignedTo().Type != "SQUAD" || persisted.AssignedTo().ID != squad.ID {
            t.Fatalf("ownership=%+v err=%v", persisted.AssignedTo(), err)
        }

        disabledModel.Enabled = true
        if _, err := service.UpdateModelProfile(ctx, &f.project.ID, disabledModel); err != nil {
            t.Fatal(err)
        }
        assertIssueAgentRunCount(t, s, issue.ID, newLeader.ID, 1)
        assertIssueRunCount(t, s, issue.ID, 2)
    }

    func assertIssueRunCount(t *testing.T, s *Store, issueID string, want int) {
        t.Helper()
        var got int
        if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM runs WHERE issue_id=$1`, issueID).Scan(&got); err != nil {
            t.Fatal(err)
        }
        if got != want {
            t.Fatalf("issue %s run count=%d want=%d", issueID, got, want)
        }
    }

    func assertIssueAgentRunCount(t *testing.T, s *Store, issueID, agentID string, want int) {
        t.Helper()
        var got int
        if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM runs WHERE issue_id=$1 AND agent_id=$2`, issueID, agentID).Scan(&got); err != nil {
            t.Fatal(err)
        }
        if got != want {
            t.Fatalf("issue %s agent %s run count=%d want=%d", issueID, agentID, got, want)
        }
    }
    ''',
)

# Fill the directly related ownership acceptance gaps through the canonical command.
replace_once(
    "apps/server/internal/store/postgres/squad_assignee_integration_test.go",
    'import (\n\t"errors"\n\t"testing"\n',
    'import (\n\t"encoding/json"\n\t"errors"\n\t"testing"\n',
)

with (ROOT / "apps/server/internal/store/postgres/squad_assignee_integration_test.go").open("a") as file:
    file.write(textwrap.dedent(r'''

    func TestSquadAssigneeReassignmentBacklogNoOpAndEventIdentity(t *testing.T) {
        s := New(testPool(t))
        f := seedRunFixture(t, s, "squad-assignee-lifecycle")
        ctx := t.Context()
        secondLeader := createSquadExecutionMember(t, s, f, "squad-assignee-second-leader")
        squadA, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Squad A", LeaderAgentID: f.agent.ID})
        if err != nil {
            t.Fatal(err)
        }
        squadB, err := s.CreateSquad(ctx, store.Squad{ProjectID: f.project.ID, Name: "Squad B", LeaderAgentID: secondLeader.ID})
        if err != nil {
            t.Fatal(err)
        }

        backlog, err := s.CreateIssue(ctx, store.Issue{ProjectID: f.project.ID, Title: "backlog ownership", Status: "BACKLOG"})
        if err != nil {
            t.Fatal(err)
        }
        assignedA, err := s.SetIssueAssignee(ctx, f.project.ID, backlog.ID, &store.Assignee{Type: "SQUAD", ID: squadA.ID}, store.EmptyObject)
        if err != nil {
            t.Fatal(err)
        }
        assertSquadAssignmentEvent(t, assignedA.Events, squadA)
        if assignedA.Issue.Status != "BACKLOG" {
            t.Fatalf("status=%s", assignedA.Issue.Status)
        }
        assertIssueRunCount(t, s, backlog.ID, 0)

        repeatedA, err := s.SetIssueAssignee(ctx, f.project.ID, backlog.ID, &store.Assignee{Type: "SQUAD", ID: squadA.ID}, store.EmptyObject)
        if err != nil {
            t.Fatal(err)
        }
        if len(repeatedA.Events) != 0 {
            t.Fatalf("repeated assignment events=%+v", repeatedA.Events)
        }
        assertIssueRunCount(t, s, backlog.ID, 0)

        assignedB, err := s.SetIssueAssignee(ctx, f.project.ID, backlog.ID, &store.Assignee{Type: "SQUAD", ID: squadB.ID}, store.EmptyObject)
        if err != nil {
            t.Fatal(err)
        }
        assertSquadAssignmentEvent(t, assignedB.Events, squadB)
        owner := assignedB.Issue.AssignedTo()
        if owner == nil || owner.Type != "SQUAD" || owner.ID != squadB.ID {
            t.Fatalf("reassigned owner=%+v", owner)
        }
        assertIssueRunCount(t, s, backlog.ID, 0)

        eligible, err := s.CreateIssue(ctx, store.Issue{ProjectID: f.project.ID, Title: "eligible ownership", Status: "TODO"})
        if err != nil {
            t.Fatal(err)
        }
        first, err := s.SetIssueAssignee(ctx, f.project.ID, eligible.ID, &store.Assignee{Type: "SQUAD", ID: squadA.ID}, store.EmptyObject)
        if err != nil {
            t.Fatal(err)
        }
        assertSquadAssignmentEvent(t, first.Events, squadA)
        assertIssueAgentRunCount(t, s, eligible.ID, f.agent.ID, 1)
        repeated, err := s.SetIssueAssignee(ctx, f.project.ID, eligible.ID, &store.Assignee{Type: "SQUAD", ID: squadA.ID}, store.EmptyObject)
        if err != nil {
            t.Fatal(err)
        }
        if len(repeated.Events) != 0 {
            t.Fatalf("repeated eligible assignment events=%+v", repeated.Events)
        }
        assertIssueAgentRunCount(t, s, eligible.ID, f.agent.ID, 1)
    }

    func assertSquadAssignmentEvent(t *testing.T, events []store.Event, squad store.Squad) {
        t.Helper()
        if len(events) == 0 || events[0].Type != "issue.assigned" {
            t.Fatalf("assignment events=%+v", events)
        }
        var payload struct {
            AssignedTo *store.Assignee `json:"assignedTo"`
        }
        if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
            t.Fatal(err)
        }
        if payload.AssignedTo == nil || payload.AssignedTo.Type != "SQUAD" || payload.AssignedTo.ID != squad.ID || payload.AssignedTo.Name != squad.Name {
            t.Fatalf("assignedTo payload=%+v", payload.AssignedTo)
        }
    }
    '''))

# Shared UI identity helpers already exist; extend them just enough for Squad.
replace_once(
    "apps/web/app/utils/identity.ts",
    """export type IdentityKind = 'agent' | 'user'\n\nconst userColors = ['primary', 'info', 'success', 'warning', 'secondary'] as const\n""",
    """import type { Assignee } from '../types/api'\n\nexport type IdentityKind = 'agent' | 'user' | 'squad'\n\nconst userColors = ['primary', 'info', 'success', 'warning', 'secondary'] as const\n\nexport function assigneeIdentityKind(type: Assignee['type']): IdentityKind {\n  if (type === 'USER') return 'user'\n  if (type === 'SQUAD') return 'squad'\n  return 'agent'\n}\n\nexport function assigneeTypeLabel(type: Assignee['type']) {\n  if (type === 'USER') return 'User'\n  if (type === 'SQUAD') return 'Squad'\n  return 'Agent'\n}\n""",
)

replace_once(
    "apps/web/app/components/IdentityAvatar.vue",
    "const color = computed(() => props.kind === 'agent' ? 'neutral' : userIdentityColor(props.name))\n",
    "const color = computed(() => props.kind === 'user' ? userIdentityColor(props.name) : 'neutral')\n",
)
replace_once(
    "apps/web/app/components/IdentityAvatar.vue",
    """  <UAvatar\n    v-else\n    :alt=\"label\"\n    :aria-label=\"label\"\n    :text=\"identityInitial(name)\"\n    :color=\"color\"\n    :size=\"size\"\n    class=\"issue-identity\"\n  />\n""",
    """  <UAvatar\n    v-else-if=\"kind === 'squad'\"\n    :alt=\"label\"\n    :aria-label=\"label\"\n    icon=\"i-lucide-users\"\n    :color=\"color\"\n    :size=\"size\"\n    class=\"issue-identity\"\n  />\n  <UAvatar\n    v-else\n    :alt=\"label\"\n    :aria-label=\"label\"\n    :text=\"identityInitial(name)\"\n    :color=\"color\"\n    :size=\"size\"\n    class=\"issue-identity\"\n  />\n""",
)

replace_once(
    "apps/web/app/components/IssueCard.vue",
    "import { formatUpdatedLabel, isFailedIssueRun, isLiveIssueRun, issueCardRunStatus, issuePriority } from '../utils/issues'\n",
    "import { formatUpdatedLabel, isFailedIssueRun, isLiveIssueRun, issueCardRunStatus, issuePriority } from '../utils/issues'\nimport { assigneeIdentityKind, assigneeTypeLabel } from '../utils/identity'\n",
)
replace_once(
    "apps/web/app/components/IssueCard.vue",
    """const assignedLabel = computed(() => props.issue.assignedTo?.name || (props.issue.assignedTo ? 'Assignee unavailable' : ''))\nconst runStatusLabel = computed(() => issueCardRunStatus(props.runStatus))\n""",
    """const assignedLabel = computed(() => props.issue.assignedTo?.name || (props.issue.assignedTo ? 'Assignee unavailable' : ''))\nconst assignedKind = computed(() => props.issue.assignedTo ? assigneeIdentityKind(props.issue.assignedTo.type) : 'user')\nconst assignedTypeLabel = computed(() => props.issue.assignedTo ? assigneeTypeLabel(props.issue.assignedTo.type) : '')\nconst runStatusLabel = computed(() => issueCardRunStatus(props.runStatus))\n""",
)
replace_once(
    "apps/web/app/components/IssueCard.vue",
    """          <IdentityAvatar :kind=\"issue.assignedTo?.type === 'USER' ? 'user' : 'agent'\" :name=\"assignedLabel\" />\n          <span class=\"truncate text-xs text-highlighted\">{{ assignedLabel }}</span>\n""",
    """          <IdentityAvatar :kind=\"assignedKind\" :name=\"assignedLabel\" />\n          <span class=\"truncate text-xs text-highlighted\">{{ assignedLabel }} · {{ assignedTypeLabel }}</span>\n""",
)

replace_once(
    "apps/web/app/components/IssueDetail.vue",
    "import { issueRuns, statusLabel } from '../utils/issues'\n",
    "import { issueRuns, statusLabel } from '../utils/issues'\nimport { assigneeIdentityKind, assigneeTypeLabel } from '../utils/identity'\n",
)
replace_once(
    "apps/web/app/components/IssueDetail.vue",
    "    label: `${assignee.name} · ${assignee.type === 'USER' ? 'User' : 'Agent'}`,\n",
    "    label: `${assignee.name} · ${assigneeTypeLabel(assignee.type)}`,\n",
)
replace_once(
    "apps/web/app/components/IssueDetail.vue",
    "const assignedName = computed(() => issue.value?.assignedTo?.name)\n",
    """const assignedName = computed(() => issue.value?.assignedTo?.name)\nconst assignedKind = computed(() => issue.value?.assignedTo ? assigneeIdentityKind(issue.value.assignedTo.type) : 'user')\nconst assignedTypeLabel = computed(() => issue.value?.assignedTo ? assigneeTypeLabel(issue.value.assignedTo.type) : '')\n""",
)
replace_once(
    "apps/web/app/components/IssueDetail.vue",
    "The current Agent assignment is valid, but its execution configuration prevents a new Run from being created.",
    "The current ownership is valid, but its execution configuration prevents a new Run from being created.",
)
replace_once(
    "apps/web/app/components/IssueDetail.vue",
    "No active Run exists for the current Agent. You can start a Run.",
    "No active Run exists for the current execution Agent. You can start a Run.",
)
replace_once(
    "apps/web/app/components/IssueDetail.vue",
    "The current Agent already has an active Run for this Issue.",
    "The current execution Agent already has an active Run for this Issue.",
)
replace_once(
    "apps/web/app/components/IssueDetail.vue",
    """                    <IdentityAvatar :kind=\"issue.assignedTo?.type === 'USER' ? 'user' : 'agent'\" :name=\"assignedName\" size=\"xs\" />\n                    <span>{{ assignedName }}</span>\n""",
    """                    <IdentityAvatar :kind=\"assignedKind\" :name=\"assignedName\" size=\"xs\" />\n                    <span>{{ assignedName }} · {{ assignedTypeLabel }}</span>\n""",
)
replace_once(
    "apps/web/app/components/IssueDetail.vue",
    "description=\"Choose an eligible User or Agent, or leave the Issue unassigned.\"",
    "description=\"Choose an eligible User, Agent, or Squad, or leave the Issue unassigned.\"",
)
replace_once(
    "apps/web/app/components/IssueDetail.vue",
    "Assignment changes ownership only; it does not change the board status. Agent assignment may enqueue execution according to backend policy.",
    "Assignment changes ownership only; it does not change the board status. Agent or Squad ownership may enqueue execution according to backend policy.",
)

replace_once(
    "apps/web/test/identity-avatar.test.ts",
    """  it('renders the first letter for users without a bot icon', () => {\n""",
    """  it('renders a Squad icon without presenting the Squad as an Agent', () => {\n    const wrapper = mount(IdentityAvatar, {\n      props: { kind: 'squad', name: 'Backend' },\n      global\n    })\n\n    expect(wrapper.find('[data-icon=\"i-lucide-users\"]').exists()).toBe(true)\n    expect(wrapper.find('[data-icon=\"i-lucide-bot\"]').exists()).toBe(false)\n    expect(wrapper.find('[aria-label=\"Backend\"]').exists()).toBe(true)\n  })\n\n  it('renders the first letter for users without a bot icon', () => {\n""",
)

write(
    "apps/web/test/squad-ownership-ui.test.ts",
    r'''
    import { flushPromises, mount } from '@vue/test-utils'
    import { afterEach, describe, expect, it, vi } from 'vitest'
    import IdentityAvatar from '../app/components/IdentityAvatar.vue'
    import IssueCard from '../app/components/IssueCard.vue'
    import IssueDetail from '../app/components/IssueDetail.vue'
    import { uiStubs } from './ui-stubs'

    const squadIssue = {
      id: 'AB-136',
      number: 136,
      projectId: 'p',
      title: 'Squad-owned work',
      description: '',
      status: 'TODO',
      priority: 0,
      assignedTo: { type: 'SQUAD' as const, id: 'squad-1', name: 'Backend' },
      createdBy: null,
      createdAt: '',
      updatedAt: '',
      currentBranch: null,
      lastEvent: null
    }

    const global = {
      stubs: {
        ...uiStubs,
        IdentityAvatar,
        IssueRelationships: { template: '<section />' },
        QuestionPanel: { template: '<section />' },
        IssueEditor: { template: '<section />' },
        NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
      }
    }

    afterEach(() => vi.unstubAllGlobals())

    describe('Squad Issue ownership presentation', () => {
      it('shows Squad ownership on Issue cards with the Squad identity icon', () => {
        const wrapper = mount(IssueCard, { props: { issue: squadIssue }, global })
        expect(wrapper.text()).toContain('Backend · Squad')
        expect(wrapper.find('[data-icon="i-lucide-users"]').exists()).toBe(true)
        expect(wrapper.find('[data-icon="i-lucide-bot"]').exists()).toBe(false)
      })

      it('labels Squad assignment and ownership without describing it as a direct Agent assignment', async () => {
        const fetch = vi.fn(async (path: string) => {
          if (path.endsWith('/assignees')) return new Response(JSON.stringify([squadIssue.assignedTo]))
          if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
          if (path.endsWith('/execution')) return new Response(JSON.stringify({ state: 'READY', canStart: true, activeRun: null }))
          return new Response(JSON.stringify(squadIssue))
        })
        vi.stubGlobal('fetch', fetch)

        const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: squadIssue.id }, global })
        await flushPromises()

        expect(wrapper.get('[data-field=assignee]').text()).toContain('Backend · Squad')
        expect(wrapper.text()).toContain('Backend · Squad')
        expect(wrapper.text()).toContain('User, Agent, or Squad')
        expect(wrapper.text()).toContain('Agent or Squad ownership may enqueue execution')
        expect(wrapper.text()).not.toContain('current Agent assignment')
        expect(wrapper.find('[data-icon="i-lucide-users"]').exists()).toBe(true)
      })
    })
    ''',
)

# Keep the scheduler documentation aligned with resolved execution ownership.
replace_once(
    "docs/scheduler.md",
    """Configuration recovery derives work from current Agent ownership, Board status,\nactive Issue/Agent Runs and the existing Agent/Model Profile/Provider validity\nchecks. It persists no pending-execution flag or recovery history. Startup runs\nthis reconciliation before starting the scheduler; configuration updates and\nProvider recovery invoke the same reconciliation for affected assignments.\n""",
    """Configuration recovery derives work from current executable ownership (direct\nAgent ownership or Squad ownership resolved to its current leader Agent), Board\nstatus, active Issue/Agent Runs and the existing Agent/Model Profile/Provider\nvalidity checks. It persists no pending-execution flag or recovery history.\nStartup runs this reconciliation before starting the scheduler; configuration\nupdates and Provider recovery invoke the same reconciliation for affected ownership.\n""",
)

# Go formatting is part of the product patch, not test orchestration.
run("gofmt", "-w",
    "apps/server/internal/store/issue_execution.go",
    "apps/server/internal/store/postgres/issue_execution.go",
    "apps/server/internal/store/squads.go",
    "apps/server/internal/store/postgres/squads.go",
    "apps/server/internal/app/squads.go",
    "apps/server/internal/app/squads_test.go",
    "apps/server/internal/httpapi/squads_test.go",
    "apps/server/internal/store/postgres/squads_integration_test.go",
    "apps/server/internal/store/postgres/squads_read_consistency_integration_test.go",
    "apps/server/internal/store/postgres/squad_leader_reconciliation_integration_test.go",
    "apps/server/internal/store/postgres/squad_assignee_integration_test.go",
)
