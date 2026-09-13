from pathlib import Path
import sys


def read(path):
    return Path(path).read_text()


def write(path, text):
    Path(path).write_text(text)


def replace(path, old, new, count=1):
    text = read(path)
    found = text.count(old)
    if found < count:
        raise RuntimeError(f"{path}: expected {count} occurrence(s), found {found}: {old[:120]!r}")
    write(path, text.replace(old, new, count))


def tests():
    replace(
        "apps/web/test/auth.test.ts",
        "  it('requires authentication and keeps deployment settings admin-only', () => {\n",
        """  it('routes a zero-user deployment directly to first administrator registration', () => {
    expect(authRedirect('/projects', false, false, false, true)).toBe('/auth/register')
    expect(authRedirect('/auth/login', false, false, false, true)).toBe('/auth/register')
    expect(authRedirect('/auth/register', false, false, false, true)).toBeNull()
    expect(authRedirect('/auth/register', false, false, false, false)).toBe('/auth/login')
  })

  it('requires authentication and keeps deployment settings admin-only', () => {
""",
    )

    replace(
        "apps/web/test/components.test.ts",
        "  it('pins Settings above the sidebar footer divider', () => {\n",
        """  it('renders authentication routes without the application sidebar', async () => {
    const route = reactive({ path: '/auth/login', params: {} as Record<string, string> })
    vi.stubGlobal('useRoute', () => route)
    vi.stubGlobal('useState', () => ({ value: null }))
    const wrapper = mount(Shell, { slots: { default: '<main>Sign in</main>' }, global })
    expect(wrapper.text()).toContain('Sign in')
    expect(wrapper.find('[aria-label=\"Primary navigation\"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Agent Board')

    route.path = '/projects'
    await flushPromises()
    expect(wrapper.find('[aria-label=\"Primary navigation\"]').exists()).toBe(true)
  })

  it('pins Settings above the sidebar footer divider', () => {
""",
    )

    replace(
        "apps/server/internal/app/project_access_test.go",
        "\tlistAllCalls  int\n\tlistUserCalls int\n}\n",
        "\tlistAllCalls  int\n\tlistUserCalls int\n\tcreatedIssue  store.Issue\n}\n",
    )
    replace(
        "apps/server/internal/app/project_access_test.go",
        """func (s *projectAccessServiceStore) ListProjectsForUser(context.Context, string) ([]store.Project, error) {
\ts.listUserCalls++
\treturn append([]store.Project(nil), s.visible...), nil
}
""",
        """func (s *projectAccessServiceStore) ListProjectsForUser(context.Context, string) ([]store.Project, error) {
\ts.listUserCalls++
\treturn append([]store.Project(nil), s.visible...), nil
}

func (s *projectAccessServiceStore) CreateIssue(_ context.Context, input store.Issue) (store.Issue, error) {
\ts.createdIssue = input
\tinput.ID = \"issue-1\"
\tinput.Key = \"AB-1\"
\treturn input, nil
}
""",
    )
    replace(
        "apps/server/internal/app/project_access_test.go",
        "func TestProjectAccessDeploymentAdminHasImplicitAdminRole(t *testing.T) {\n",
        """func TestProjectAccessCreateIssueAttributesAuthenticatedHuman(t *testing.T) {
\tproject := store.Project{ID: \"project-1\", Name: \"Project\"}
\tfake := &projectAccessServiceStore{
\t\tprojects: []store.Project{project},
\t\troles:    map[string]string{project.ID + \":user-1\": store.ProjectRoleMember},
\t}
\tservice := newProjectAccessServiceForTest(t, fake)
\tactor := activeProjectActor(\"user-1\", store.DeploymentRoleMember)

\tcreated, err := service.CreateIssue(t.Context(), actor, store.Issue{ProjectID: project.ID, Title: \"Attributed\", Status: \"TODO\"})
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tfor name, issue := range map[string]store.Issue{\"input\": fake.createdIssue, \"result\": created} {
\t\tif issue.CreatedByType == nil || *issue.CreatedByType != store.ActorTypeHuman || issue.CreatedByID == nil || *issue.CreatedByID != actor.ID {
\t\t\tt.Fatalf(\"%s creator = type=%v id=%v\", name, issue.CreatedByType, issue.CreatedByID)
\t\t}
\t}
}

func TestProjectAccessDeploymentAdminHasImplicitAdminRole(t *testing.T) {
""",
    )

    replace(
        "apps/server/internal/store/postgres/controlplane_integration_test.go",
        '\tissue, err := s.CreateIssue(ctx, store.Issue{ProjectID: p1.ID, Title: "Issue", Status: "TODO"})\n',
        '\tcreatorType := store.ActorTypeAgent\n\tissue, err := s.CreateIssue(ctx, store.Issue{ProjectID: p1.ID, Title: "Issue", Status: "TODO", CreatedByType: &creatorType, CreatedByID: &agent.ID})\n',
    )
    replace(
        "apps/server/internal/store/postgres/controlplane_integration_test.go",
        """\tif err != nil || len(issues) != 1 {
\t\tt.Fatalf(\"issues=%d err=%v\", len(issues), err)
\t}
\tissue.Description = \"updated\"
\tif _, err = s.UpdateIssue(ctx, issue); err != nil {
\t\tt.Fatal(err)
\t}
""",
        """\tif err != nil || len(issues) != 1 {
\t\tt.Fatalf(\"issues=%d err=%v\", len(issues), err)
\t}
\tif issues[0].CreatedByType == nil || *issues[0].CreatedByType != store.ActorTypeAgent || issues[0].CreatedByID == nil || *issues[0].CreatedByID != agent.ID {
\t\tt.Fatalf(\"persisted issue creator = type=%v id=%v\", issues[0].CreatedByType, issues[0].CreatedByID)
\t}
\tissue.Description = \"updated\"
\tissue, err = s.UpdateIssue(ctx, issue)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif issue.CreatedByType == nil || *issue.CreatedByType != store.ActorTypeAgent || issue.CreatedByID == nil || *issue.CreatedByID != agent.ID {
\t\tt.Fatalf(\"updated issue creator = type=%v id=%v\", issue.CreatedByType, issue.CreatedByID)
\t}
""",
    )

    replace(
        "apps/server/internal/app/issue_events_test.go",
        "\tcreated, err := svc.CreateIssue(context.Background(), coverageIssue())\n",
        """\tcreatorType, creatorID := store.ActorTypeAgent, \"agent-creator\"
\tcreateInput := coverageIssue()
\tcreateInput.CreatedByType, createInput.CreatedByID = &creatorType, &creatorID
\tcreated, err := svc.CreateIssue(context.Background(), createInput)
""",
    )
    replace(
        "apps/server/internal/app/issue_events_test.go",
        """\tif created.LastEvent == nil || created.LastEvent.Type != \"issue.created\" {
\t\tt.Fatalf(\"create lastEvent=%+v\", created.LastEvent)
\t}
""",
        """\tif created.LastEvent == nil || created.LastEvent.Type != \"issue.created\" {
\t\tt.Fatalf(\"create lastEvent=%+v\", created.LastEvent)
\t}
\tvar createActor map[string]string
\tif err := json.Unmarshal(created.LastEvent.Actor, &createActor); err != nil {
\t\tt.Fatal(err)
\t}
\tif createActor[\"type\"] != store.ActorTypeAgent || createActor[\"id\"] != creatorID {
\t\tt.Fatalf(\"create actor=%s\", created.LastEvent.Actor)
\t}
""",
    )


def production():
    write(
        "apps/web/app/utils/auth-route.ts",
        """const publicAuthPaths = new Set(['/auth/login', '/auth/register', '/auth/setup', '/auth/reset'])

export function isPublicAuthPath(path: string) {
  return publicAuthPaths.has(path)
}

export function authRedirect(path: string, authenticated: boolean, forcePasswordChange: boolean, admin: boolean, bootstrapAvailable = false) {
  if (!authenticated) {
    if (bootstrapAvailable) return path === '/auth/register' ? null : '/auth/register'
    if (path === '/auth/register') return '/auth/login'
    return isPublicAuthPath(path) ? null : '/auth/login'
  }
  if (forcePasswordChange && path !== '/account') return '/account'
  if ((path === '/settings' || path.startsWith('/settings/')) && !admin) return '/account'
  if (isPublicAuthPath(path)) return '/account'
  return null
}
""",
    )

    write(
        "apps/web/app/middleware/auth.global.ts",
        """import { authRedirect } from '../utils/auth-route'

export default defineNuxtRouteMiddleware(async (to) => {
  if (import.meta.server) return
  const auth = useAuth()
  await auth.initialize()

  let bootstrapAvailable = false
  if (!auth.isAuthenticated.value) {
    try {
      bootstrapAvailable = await auth.bootstrapAvailable()
    } catch {
      // Keep the normal login boundary when bootstrap status is temporarily unavailable.
    }
  }

  const redirect = authRedirect(
    to.path,
    auth.isAuthenticated.value,
    Boolean(auth.user.value?.forcePasswordChange),
    auth.isAdmin.value,
    bootstrapAvailable
  )
  if (redirect) return navigateTo(redirect)
})
""",
    )

    replace(
        "apps/web/app/pages/auth/[mode].vue",
        """onMounted(async () => {
  if (!allowed.has(mode.value)) return navigateTo('/auth/login')
  if (mode.value === 'login') {
    try {
      if (await auth.bootstrapAvailable()) return navigateTo('/auth/register')
    } catch {
      // Login remains available when bootstrap status cannot be loaded.
    }
  }
})
""",
        """onMounted(() => {
  if (!allowed.has(mode.value)) return navigateTo('/auth/login')
})
""",
    )

    replace(
        "apps/web/app/components/AppShell.vue",
        """import { navigation } from '../utils/navigation'

const route = useRoute()
""",
        """import { navigation } from '../utils/navigation'
import { isPublicAuthPath } from '../utils/auth-route'

const route = useRoute()
const publicAuthPage = computed(() => isPublicAuthPath(route.path))
""",
    )
    replace(
        "apps/web/app/components/AppShell.vue",
        """<template>
  <UDashboardGroup unit=\"px\">
""",
        """<template>
  <slot v-if=\"publicAuthPage\" />
  <UDashboardGroup v-else unit=\"px\">
""",
    )

    replace(
        "apps/server/internal/store/types.go",
        """const (
\tProjectSourceLocal = \"local\"
\tProjectSourceGit   = \"git\"
)
""",
        """const (
\tProjectSourceLocal = \"local\"
\tProjectSourceGit   = \"git\"
\tActorTypeHuman     = \"HUMAN\"
\tActorTypeAgent     = \"AGENT\"
)

func ValidActorType(value string) bool {
\treturn value == ActorTypeHuman || value == ActorTypeAgent
}
""",
    )
    replace(
        "apps/server/internal/store/types.go",
        """\tPriority        int
\tAssignedAgentID *string
\tCreatedAt       time.Time
""",
        """\tPriority        int
\tAssignedAgentID *string
\tCreatedByType   *string
\tCreatedByID     *string
\tCreatedAt       time.Time
""",
    )

    replace(
        "apps/server/internal/app/questions.go",
        '\t\tActorType:  "HUMAN",\n',
        "\t\tActorType:  store.ActorTypeHuman,\n",
    )
    replace(
        "apps/server/internal/store/postgres/questions.go",
        'input.ActorType != "HUMAN"',
        "input.ActorType != store.ActorTypeHuman",
    )

    replace(
        "apps/server/internal/app/service.go",
        """func validateIssue(v store.Issue) error {
\tif strings.TrimSpace(v.Title) == \"\" {
\t\treturn invalid(\"issue title is required\")
\t}
""",
        """func validateIssue(v store.Issue) error {
\tif strings.TrimSpace(v.Title) == \"\" {
\t\treturn invalid(\"issue title is required\")
\t}
\tif (v.CreatedByType == nil) != (v.CreatedByID == nil) {
\t\treturn invalid(\"issue creator type and id must be provided together\")
\t}
\tif v.CreatedByType != nil && (!store.ValidActorType(*v.CreatedByType) || strings.TrimSpace(*v.CreatedByID) == \"\") {
\t\treturn invalid(\"issue creator must be a HUMAN or AGENT with an id\")
\t}
""",
    )
    replace(
        "apps/server/internal/app/service.go",
        '\tevent, err := s.recordIssueEvent(ctx, "issue.created", value, issueMutationPayload(value))\n',
        """\tcreatorActor, err := issueCreatorActor(value)
\tif err != nil {
\t\treturn store.Issue{}, err
\t}
\tevent, err := s.recordIssueEvent(ctx, \"issue.created\", value, creatorActor, issueMutationPayload(value))
""",
    )
    replace(
        "apps/server/internal/app/service.go",
        "\tevent, err := s.recordIssueEvent(ctx, eventType, value, payload)\n",
        "\tevent, err := s.recordIssueEvent(ctx, eventType, value, store.EmptyObject, payload)\n",
    )
    replace(
        "apps/server/internal/app/assignment.go",
        's.recordIssueEvent(ctx, "issue.assigned", assigned, map[string]any{"agentId": agentID})',
        's.recordIssueEvent(ctx, "issue.assigned", assigned, store.EmptyObject, map[string]any{"agentId": agentID})',
    )

    write(
        "apps/server/internal/app/issue_events.go",
        """package app

import (
\t\"context\"
\t\"encoding/json\"

\t\"github.com/brantje/agent-board/apps/server/internal/evidence\"
\t\"github.com/brantje/agent-board/apps/server/internal/store\"
)

func issueCreatorActor(issue store.Issue) (json.RawMessage, error) {
\tif issue.CreatedByType == nil || issue.CreatedByID == nil {
\t\treturn append(json.RawMessage(nil), store.EmptyObject...), nil
\t}
\treturn evidence.EncodePayload(map[string]string{\"type\": *issue.CreatedByType, \"id\": *issue.CreatedByID})
}

func (s *Service) recordIssueEvent(ctx context.Context, eventType string, issue store.Issue, actor json.RawMessage, payload any) (store.Event, error) {
\tif s == nil || s.events == nil {
\t\treturn store.Event{}, nil
\t}
\tencoded, err := evidence.EncodePayload(payload)
\tif err != nil {
\t\treturn store.Event{}, err
\t}
\tif len(actor) == 0 {
\t\tactor = store.EmptyObject
\t}
\tissueID := issue.ID
\treturn s.events.Record(ctx, store.Event{
\t\tType:      eventType,
\t\tProjectID: issue.ProjectID,
\t\tIssueID:   &issueID,
\t\tActor:     actor,
\t\tPayload:   encoded,
\t})
}

func attachIssueEvent(issue store.Issue, event store.Event) store.Issue {
\tif event.ID == \"\" {
\t\treturn issue
\t}
\tissue.LastEvent = &event
\treturn issue
}

func issueMutationPayload(issue store.Issue) map[string]any {
\treturn map[string]any{
\t\t\"title\":    issue.Title,
\t\t\"status\":   issue.Status,
\t\t\"priority\": issue.Priority,
\t}
}
""",
    )

    replace(
        "apps/server/internal/app/project_access.go",
        """func (s *ProjectAccessService) CreateIssue(ctx context.Context, actor AuthenticatedUser, input store.Issue) (store.Issue, error) {
\tif err := s.AuthorizeWorkflowMutation(ctx, actor, input.ProjectID); err != nil {
\t\treturn store.Issue{}, err
\t}
\treturn s.controlPlane.CreateIssue(ctx, input)
}
""",
        """func (s *ProjectAccessService) CreateIssue(ctx context.Context, actor AuthenticatedUser, input store.Issue) (store.Issue, error) {
\tif err := s.AuthorizeWorkflowMutation(ctx, actor, input.ProjectID); err != nil {
\t\treturn store.Issue{}, err
\t}
\tactorType, actorID := store.ActorTypeHuman, actor.ID
\tinput.CreatedByType, input.CreatedByID = &actorType, &actorID
\treturn s.controlPlane.CreateIssue(ctx, input)
}
""",
    )

    replace(
        "packages/database/schema.sql",
        """    assigned_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
""",
        """    assigned_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL,
    created_by_type text CHECK (created_by_type IS NULL OR created_by_type IN ('HUMAN', 'AGENT')),
    created_by_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
""",
    )
    replace(
        "packages/database/schema.sql",
        """    UNIQUE (project_id, id),
    UNIQUE (project_id, number)
);

CREATE INDEX issues_project_status_idx""",
        """    UNIQUE (project_id, id),
    UNIQUE (project_id, number),
    CHECK ((created_by_type IS NULL) = (created_by_id IS NULL))
);

CREATE INDEX issues_project_status_idx""",
    )

    replace(
        "apps/server/internal/store/postgres/core.go",
        """\ti.assigned_agent_id::text,
\ti.number,
\tp.issue_prefix,
\ti.created_at,
""",
        """\ti.assigned_agent_id::text,
\ti.number,
\tp.issue_prefix,
\ti.created_by_type,
\ti.created_by_id::text,
\ti.created_at,
""",
    )
    replace(
        "apps/server/internal/store/postgres/core.go",
        """\t\tINSERT INTO issues (project_id, number, title, description, status, priority, assigned_agent_id)
\t\tVALUES ($1, $2, $3, $4, $5, $6, $7)
\t\tRETURNING id::text, project_id::text, title, description, status, priority, assigned_agent_id::text, number, created_at, updated_at
\t`, input.ProjectID, number, input.Title, input.Description, status, input.Priority, input.AssignedAgentID))
""",
        """\t\tINSERT INTO issues (project_id, number, title, description, status, priority, assigned_agent_id, created_by_type, created_by_id)
\t\tVALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
\t\tRETURNING id::text, project_id::text, title, description, status, priority, assigned_agent_id::text, number, created_by_type, created_by_id::text, created_at, updated_at
\t`, input.ProjectID, number, input.Title, input.Description, status, input.Priority, input.AssignedAgentID, input.CreatedByType, input.CreatedByID))
""",
    )
    replace(
        "apps/server/internal/store/postgres/core.go",
        """\tif err := row.Scan(&value.ID, &value.ProjectID, &value.Title, &value.Description, &value.Status, &value.Priority, &value.AssignedAgentID, &value.Number, &value.CreatedAt, &value.UpdatedAt); err != nil {
""",
        """\tif err := row.Scan(&value.ID, &value.ProjectID, &value.Title, &value.Description, &value.Status, &value.Priority, &value.AssignedAgentID, &value.Number, &value.CreatedByType, &value.CreatedByID, &value.CreatedAt, &value.UpdatedAt); err != nil {
""",
    )
    replace(
        "apps/server/internal/store/postgres/core.go",
        """\t\t&value.AssignedAgentID, &value.Number, &prefix, &value.CreatedAt, &value.UpdatedAt,
""",
        """\t\t&value.AssignedAgentID, &value.Number, &prefix, &value.CreatedByType, &value.CreatedByID, &value.CreatedAt, &value.UpdatedAt,
""",
        count=2,
    )
    replace(
        "apps/server/internal/store/postgres/assignment.go",
        """\t\t&issue.AssignedAgentID, &issue.Number, &prefix, &issue.CreatedAt, &issue.UpdatedAt,
""",
        """\t\t&issue.AssignedAgentID, &issue.Number, &prefix, &issue.CreatedByType, &issue.CreatedByID, &issue.CreatedAt, &issue.UpdatedAt,
""",
    )

    replace(
        "apps/server/internal/httpapi/contracts.go",
        """type IssueDTO struct {
\tID              string            `json:\"id\"`
""",
        """type IssueCreatorDTO struct {
\tType string `json:\"type\"`
\tID   string `json:\"id\"`
}

type IssueDTO struct {
\tID              string            `json:\"id\"`
""",
    )
    replace(
        "apps/server/internal/httpapi/contracts.go",
        """\tAssignedAgentID *string           `json:\"assignedAgentId\"`
\tCreatedAt       time.Time         `json:\"createdAt\"`
""",
        """\tAssignedAgentID *string           `json:\"assignedAgentId\"`
\tCreatedBy       *IssueCreatorDTO  `json:\"createdBy\"`
\tCreatedAt       time.Time         `json:\"createdAt\"`
""",
    )

    replace(
        "apps/server/internal/httpapi/mapping.go",
        """func issueDTO(v store.Issue) IssueDTO {
\tdto := IssueDTO{v.Key, v.ProjectID, v.Number, v.Title, v.Description, v.Status, v.Priority, v.AssignedAgentID, v.CreatedAt, v.UpdatedAt, v.CurrentBranch, nil}
\tif v.LastEvent != nil {
\t\tevent := eventEvidenceDTO(*v.LastEvent)
\t\tdto.LastEvent = &event
\t}
\treturn dto
}
""",
        """func issueDTO(v store.Issue) IssueDTO {
\tdto := IssueDTO{
\t\tID:              v.Key,
\t\tProjectID:       v.ProjectID,
\t\tNumber:          v.Number,
\t\tTitle:           v.Title,
\t\tDescription:     v.Description,
\t\tStatus:          v.Status,
\t\tPriority:        v.Priority,
\t\tAssignedAgentID: v.AssignedAgentID,
\t\tCreatedAt:       v.CreatedAt,
\t\tUpdatedAt:       v.UpdatedAt,
\t\tCurrentBranch:   v.CurrentBranch,
\t}
\tif v.CreatedByType != nil && v.CreatedByID != nil {
\t\tdto.CreatedBy = &IssueCreatorDTO{Type: *v.CreatedByType, ID: *v.CreatedByID}
\t}
\tif v.LastEvent != nil {
\t\tevent := eventEvidenceDTO(*v.LastEvent)
\t\tdto.LastEvent = &event
\t}
\treturn dto
}
""",
    )

    replace(
        "apps/web/app/types/api.ts",
        "export interface Issue {\n",
        """export interface IssueCreator {
  type: 'HUMAN' | 'AGENT'
  id: string
}

export interface Issue {
""",
    )
    replace(
        "apps/web/app/types/api.ts",
        """  assignedAgentId: string | null
  createdAt: string
""",
        """  assignedAgentId: string | null
  createdBy: IssueCreator | null
  createdAt: string
""",
    )

    replace(
        "packages/api/schemas/control-plane.yaml",
        """Issue:
  type: object
  required: [id, projectId, number, title, description, status, priority, assignedAgentId, createdAt, updatedAt, lastEvent]
""",
        """IssueCreator:
  type: object
  additionalProperties: false
  required: [type, id]
  properties:
    type: {type: string, enum: [HUMAN, AGENT]}
    id: {type: string, format: uuid}
Issue:
  type: object
  required: [id, projectId, number, title, description, status, priority, assignedAgentId, createdBy, createdAt, updatedAt, lastEvent]
""",
    )
    replace(
        "packages/api/schemas/control-plane.yaml",
        """    assignedAgentId: {type: [string, 'null'], format: uuid}
    createdAt: {type: string, format: date-time}
""",
        """    assignedAgentId: {type: [string, 'null'], format: uuid}
    createdBy:
      oneOf:
        - $ref: '#/IssueCreator'
        - type: 'null'
    createdAt: {type: string, format: date-time}
""",
    )

    replace(
        "docs/authorization.md",
        """Existing durable domain records that support human attribution continue using their existing `actor_type = HUMAN` shape. New authenticated human actions populate the existing `actor_id` with the durable authenticated User ID. Question answers and Review approve/request-changes paths use this identity.

Historical records are preserved when a User is renamed, disabled or re-enabled. This is not a generalized deployment-wide audit-log product.
""",
        """Existing durable domain records that support human attribution continue using their existing `actor_type = HUMAN` shape. New authenticated human actions populate the existing `actor_id` with the durable authenticated User ID. Question answers and Review approve/request-changes paths use this identity.

Issues store their creator as the same durable actor reference (`HUMAN` or `AGENT` plus actor ID). Authenticated browser/API Issue creation is stamped as `HUMAN` with the authenticated User ID by the shared Project authorization application boundary; callers cannot choose a different human creator. The same Issue domain shape supports Agent-created follow-up Issues without introducing a user-only creator model.

Historical records are preserved when a User is renamed, disabled or re-enabled. This is not a generalized deployment-wide audit-log product.
""",
    )


if __name__ == "__main__":
    if len(sys.argv) != 2 or sys.argv[1] not in {"tests", "production"}:
        raise SystemExit("usage: apply_fixes.py tests|production")
    (tests if sys.argv[1] == "tests" else production)()
