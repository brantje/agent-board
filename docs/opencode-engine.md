# OpenCode Engine adapter

Agent Board v0.1 uses OpenCode as its first real interactive Engine adapter. OpenCode runs on the selected Runner through `agent-runner`; the trusted server never starts OpenCode locally and never exposes the OpenCode HTTP service as a public Agent Board endpoint.

The official `apps/agent-runner/Dockerfile` installs OpenCode from the immutable upstream `v1.18.31` release, selecting the architecture-specific musl asset and verifying its SHA-256 digest before extraction. Engine adapters still depend only on Agent Board Engine capabilities, not on Docker or WebSocket implementation details.

## Native execution path

For an OpenCode Run, the server-side adapter:

1. starts `opencode serve` through the generic Execution Session launcher on a deterministic Run-scoped address inside Linux `127/8`, using a high Run-scoped port so multiple OpenCode services can coexist on the same Runner host without sharing a listener;
2. sets `OPENCODE_DB=:memory:` for that child process so concurrent OpenCode services do not share OpenCode's global SQLite state; Agent Board's durable Run, Execution Session, Question and evidence stores remain authoritative;
3. opens HTTP connections to that loopback service through the Engine-neutral, session-scoped runner connection capability;
4. creates one native OpenCode session in the Execution Session working directory so host runners and Docker share the same OpenCode project as `opencode serve` (a hardcoded `/workspace` path is a different OpenCode project on persistent hosts);
5. sends the issue/agent/review context once as the initial native prompt;
6. consumes OpenCode's SSE event stream and maps explicitly visible activity into canonical Agent Board evidence;
7. reconciles pending native Questions from OpenCode's queryable Question endpoint after an SSE disconnect;
8. stops the native service when the execution reaches its terminal result or the Run is cancelled.

The Run-scoped loopback endpoint is derived from the durable Run ID, so reconnecting the same live Execution Session computes the same endpoint again. The in-memory OpenCode database is intentionally process-local: live native Question handling and Runner reconnect keep the same OpenCode process alive, while Agent Board does not depend on OpenCode's global database as a durability boundary.

The adapter does not use `opencode run --format json` as its protocol boundary.

## Interactive Questions

A native `question.v2.asked` request is mapped to one or more durable Agent Board Questions. Native session/request identifiers are retained only as opaque correlation keys in `engine_question_bindings`; Agent Board exposes its own Question IDs.

The healthy live path is:

```text
OpenCode question.v2.asked
  -> canonical question.created
  -> Run WAITING_FOR_INPUT
  -> human answers through Agent Board
  -> durable question.answered + decision.recorded
  -> OpenCode native question reply API
  -> same OpenCode session continues
  -> binding resolved + run.resumed
```

Answering a live OpenCode Question does not queue a recovery Run and does not manufacture a follow-up prompt such as "the user answered ...". The current scheduler claim remains live while the Run is `WAITING_FOR_INPUT`; `ResolveInteractiveQuestion` returns the same Run to `RUNNING` only after the native reply succeeds and no other native Question remains unresolved.

Text, single-choice, and multi-choice Questions round-trip when they are representable by Agent Board's canonical Question contract. OpenCode can also represent a Question that simultaneously offers fixed options and arbitrary custom text; the v0.1 Agent Board contract intentionally treats choice and text answers as distinct kinds, so that combined shape is not exposed as a single mixed Question.

Duplicate/stale Agent Board answers are rejected by the durable Question state. If a native reply fails after OpenCode may already have accepted it, the adapter checks the authoritative pending-Question list before deciding whether a retry is necessary.

## Agent-authored Issue comments

Every executing OpenCode Run receives the trusted `publish_issue_comment(body)` capability. The model controls only the concise user-visible body. Project, Issue, Agent and source Run identity come from Agent Board's trusted execution context, and the durable native tool-part ID is used as the Run-scoped idempotency key.

Publication is explicit: ordinary assistant output, reasoning, command/test output, files, logs, Run Events and status transitions are never copied into Issue comments. Completed native comment-tool parts are reconciled after attach, reconnect and normal completion; replay returns the same canonical comment rather than multiplying comments. Blocking human input continues to use OpenCode's native Question capability.

A delegated Run may publish a finding or handoff tied to its own Run, but it still receives neither `set_issue_status` nor `delegate_task`. Comment publication does not transfer Issue ownership, mutate Board status, create another Run, or add mention/routing semantics. Plain `@name` text remains ordinary comment content.

## Issue discussion reads

Executing OpenCode Runs receive the trusted `read_issue_discussion` capability backed by Agent Board's canonical Issue-comment application queries. The model can ask for `recent` discussion roots, a bounded `thread` from any comment anchor, or incremental `updates` from an opaque cursor. Project and Issue identity are never tool arguments: run execution derives both from the trusted execution context and delegates to the same server-owned queries used by HTTP and available to a future MCP adapter.

The read tool returns its canonical result in the same native tool turn. Agent Board does not copy the full Issue history into the bootstrap prompt and does not issue a second model prompt to deliver query results. Instead, the tool module hosts a tiny Run-scoped loopback request queue inside the existing OpenCode process. The trusted adapter reaches that queue only through the Execution Session's existing `SessionConnector`, executes the shared `IssueDiscussionReader`, and posts the result back to the exact pending tool call. The Runtime receives no database credentials, Project/Issue selector, or general Agent Board API token.

Discussion reads are explicitly bounded. Recent orientation is thread-first and includes reply counts plus last-activity metadata. Thread reads restore the root/ancestor context needed to avoid orphan-looking replies. Incremental reads use an opaque composite chronological cursor and restore required ancestors without advancing the cursor past omitted newer comments. Resolved discussions may expose a compact root-to-latest-activity path for orientation, while the full durable thread remains fetchable. The loopback queue retains a request until its response is acknowledged so control-plane reattachment can safely replay an in-flight read.

## Delegation capability

OpenCode receives Agent Board tools from trusted Engine capabilities rather than from static adapter configuration. An authoritative Run receives `delegate_task(targetAgentId, task)` only when its Agent has `Allow delegation` enabled. A delegated Run receives neither `delegate_task` nor `set_issue_status`; it may still use `publish_issue_comment(body)` for bounded collaboration tied to its own Run, while nested delegation and authoritative Issue status mutation remain absent from the model-visible tool surface.

The OpenCode adapter is only a transport bridge. It does not validate target eligibility, create Runs, inspect capacity, or schedule work. When OpenCode completes a native `delegate_task` part, the adapter forwards the target Agent and bounded task to the shared delegation capability. The durable native tool-part ID becomes the canonical request key; parent Project/Run/Agent identity is supplied by trusted execution context and cannot be forged by tool input.

Completed delegation tool parts are reconciled from native OpenCode history after attach/reconnect/completion. Replaying a completed part is safe because the canonical backend command is idempotent for the same parent Run + request key. A request-key reuse with different target/task content is a conflict rather than a second delegation.

Delegated prompts identify the bounded delegated task and the fact that the parent Run remains authoritative. They do not include Issue-status or delegation guidance. The delegated Run still executes through the normal Run/scheduler/Workspace pipeline; OpenCode has no separate delegation queue or direct Agent-to-Agent connection.

Delegation acceptance alone is not a Workspace handoff. The ordinary child Run and START job remain held until the accepted native `delegate_task` reaches completed state with matching call ID, target Agent and task. The parent then crosses the same Workspace/Git hand-back boundary used by ordinary execution. After that hand-back is durable, the parent's fenced scheduler transition to `PAUSED` atomically yields execution ownership and releases the existing child START job.

The delegate writes through the same Issue Workspace and Issue branch. A successful delegate completes as a `COMPLETED` subtask rather than `READY_FOR_REVIEW`; failure/cancellation are likewise persisted as bounded delegation outcomes. Terminalization records a concise result linked to existing child Run evidence and atomically queues one normal `RESUME` for the same parent Run when the parent is still eligible to continue. The resumed OpenCode request receives explicit bounded delegation context plus the current authoritative Workspace revision. Parent cancellation propagates through canonical Run cancellation, while uncertain Runner/Workspace recovery remains fail-closed.

For an authoritative Run with effective delegation capability, OpenCode receives a bounded server-owned target directory of currently runnable Agent IDs/names. If the Issue is Squad-owned, the prompt also identifies the current Squad, authoritative leader, and Agent members with optional descriptive roles. These values are selection context only: the adapter does not validate membership or scheduling, and the canonical delegation command revalidates the chosen target. Human Squad members are not `delegate_task` targets and no automatic fan-out occurs.

## Evidence and reasoning boundary

The adapter may persist explicitly emitted OpenCode messages, plans, rationale, progress, discoveries, summaries, reasoning text, tool activity, files, tests, completion, and failure information when the native protocol exposes them reliably. A finalized native `reasoning` part is stored as canonical `agent.message` evidence with `kind: "reasoning"`; the Run UI presents that emitted trace as a Thought.

This boundary does not authorize Agent Board to infer reasoning from tool calls, reconstruct reasoning that OpenCode did not emit, or make a second model request to classify or manufacture timeline activity. Unclassified visible messages continue to fall back to the generic `agent.message` kind.

The injected task bootstrap includes the persisted Issue Board status plus explicit status guidance for authoritative Runs. Activity suppression intentionally removes only the exact literal `issueStatusPromptGuidance` paragraph from visible OpenCode text before persistence. The remaining bootstrap prompt may stay visible. v0.1 does not use structural, fuzzy, or generalized prompt suppression; changing that behavior would be a separate product decision.

OpenCode tool lifecycle Events retain the native call identifier as `toolCallId` and may include sanitized `input`, `summary`, a bounded `resultPreview`, and `reason`. The UI uses `toolCallId` to collapse started/completed/failed lifecycle Events into one logical tool row. Large output remains raw/blob evidence rather than being copied into structured Events.

All structured activity still passes through Agent Board's centralized Run-scoped redacting store before persistence. Reasoning text, tool inputs, summaries, result previews, and failure reasons therefore use the same secret-redaction boundary as other Event payloads.

## Credential-gated real verification

The deterministic unit/integration tests run without provider credentials and cover protocol framing, session-local connectivity, native Question mapping, same-session reply behavior, reconnect reconciliation, stale answers, audit-event ordering, Run-scoped OpenCode endpoints, process-local database isolation, capability-gated Agent Board tools, delegation request-key derivation, and delegation recovery replay.

Credential-gated tests prove a real model-backed OpenCode path through Agent Board's scheduler. Both Docker Runtime and persistent Runner variants reset the PostgreSQL `public` schema where their fixture requires exclusive state. They refuse to do so unless the database is named `agent_board_test` and `AGENT_BOARD_TEST_DATABASE_RESET=1` is set. Never point them at a development or production database.

`TestOpenCodeRunnerNormalCodingRun` in `apps/server/internal/runexec/opencode_runner_integration_test.go` is the preferred v0.1 path: scheduler selects a live `agent-runner`, transfers the Issue Workspace, starts OpenCode on that runner, talks to OpenRouter, and syncs untracked results back without a Runtime Instance. `TestOpenCodeInternalRunnerNormalCodingRun` covers the same coding path through `SuperviseInternalRunner`. `opencode` must be on the runner host `$PATH`. `AGENT_BOARD_TEST_OPENCODE_API_KEY` / `AGENT_BOARD_TEST_OPENCODE_MODEL` may fall back to `OPENROUTER_API_KEY` / the first `OPENROUTER_MODELS` entry.

`TestOpenCodeConcurrentRunnerSessionsNormalAndQuestions` is the concurrency regression. It starts three real capacity-1 host-process Runners, holds two real OpenCode Runs simultaneously in `WAITING_FOR_INPUT` on separate native Questions, requires a third normal coding Run to finish while those Questions remain blocked, then answers both Questions concurrently. The test verifies distinct Runner ownership, zero Question leakage into the normal Run, correct Workspace results for each Question answer, and the expected durable Question lifecycle for both sessions.

`TestOpenCodeDockerOpenRouterDelegationEndToEnd` in `apps/server/internal/runexec/opencode_delegation_integration_test.go` is the live delegation lifecycle proof. A real OpenRouter-backed Squad leader OpenCode Agent invokes `delegate_task` itself after selecting the member by descriptive role from trusted server-owned context rather than from a UUID embedded in role instructions. The test requires one durable delegation record and ordinary target Run, canonical parent/child lineage, bounded child work, safe serialized Workspace handoff, child `COMPLETED` without an independent Review, persisted bounded result/evidence, automatic continuation of the same parent Run, and final Review readiness only from the parent. It also verifies that the resumed parent sees delegate Workspace changes through the authoritative Issue Workspace rather than a delegation-specific copy or branch.

```sh
cd apps/server

AGENT_BOARD_TEST_OPENCODE=1 \
AGENT_BOARD_TEST_DATABASE_RESET=1 \
AGENT_BOARD_TEST_DATABASE_URL='postgres://agent_board:agent_board@127.0.0.1:5432/agent_board_test?sslmode=disable' \
AGENT_BOARD_TEST_OPENCODE_PROVIDER_KIND='openrouter' \
AGENT_BOARD_TEST_OPENCODE_MODEL='<model-id>' \
AGENT_BOARD_TEST_OPENCODE_API_KEY='<provider-api-key>' \
go test -count=1 -timeout 6m -run '^TestOpenCodeRunnerNormalCodingRun$' -v ./internal/runexec
```

The Docker-backed tests remain for Runtime isolation:

- `TestOpenCodeDockerInteractiveQuestionRoundTrip` in `apps/server/internal/runexec/opencode_integration_test.go`;
- `TestOpenCodeDockerOpenRouterDelegationEndToEnd` in `apps/server/internal/runexec/opencode_delegation_integration_test.go`.

### 1. Build the Runtime image

When the test server runs directly on the host, align the non-root Runtime user with the host workspace owner so the bind-mounted `/workspace` remains writable:

```sh
docker build \
  --build-arg AGENT_BOARD_UID="$(id -u)" \
  --file apps/agent-runner/Dockerfile \
  --tag agent-board-opencode-runtime:manual \
  .
```

The UID build argument is optional; normal Compose builds retain the image's existing non-root default identity.

### 2. Provide a disposable PostgreSQL test database

For example:

```sh
docker run --rm --detach \
  --name agent-board-opencode-postgres \
  -e POSTGRES_DB=agent_board_test \
  -e POSTGRES_USER=agent_board \
  -e POSTGRES_PASSWORD=agent_board \
  -p 5432:5432 \
  pgvector/pgvector:pg18
```

### 3. Run a gated Docker test

Set the provider values to a model/provider that your OpenCode installation supports. `AGENT_BOARD_TEST_OPENCODE_BASE_URL` is optional for compatible/custom provider endpoints. The delegation E2E specifically requires `openrouter`.

```sh
cd apps/server

AGENT_BOARD_TEST_OPENCODE=1 \
AGENT_BOARD_TEST_DOCKER=1 \
AGENT_BOARD_TEST_DATABASE_RESET=1 \
AGENT_BOARD_TEST_DATABASE_URL='postgres://agent_board:agent_board@127.0.0.1:5432/agent_board_test?sslmode=disable' \
AGENT_BOARD_TEST_OPENCODE_RUNTIME_IMAGE='agent-board-opencode-runtime:manual' \
AGENT_BOARD_TEST_OPENCODE_PROVIDER_KIND='openrouter' \
AGENT_BOARD_TEST_OPENCODE_MODEL='<model-id>' \
AGENT_BOARD_TEST_OPENCODE_API_KEY='<provider-api-key>' \
go test -count=1 -run '^TestOpenCodeDockerOpenRouterDelegationEndToEnd$' -v ./internal/runexec
```

For a custom endpoint, also set:

```sh
AGENT_BOARD_TEST_OPENCODE_BASE_URL='https://provider.example/v1'
```

The Question round-trip test instructs the real OpenCode execution to ask one single-choice native Question before editing. It waits until Agent Board has a durable open Question and the Run is `WAITING_FOR_INPUT`, answers `beta` through `QuestionService`, and then requires:

- the same in-flight Run to continue to `READY_FOR_REVIEW`;
- exactly one OpenCode service Execution Session for the attempt;
- `opencode-result.txt` to contain `beta` followed by a newline;
- `question.created < run.waiting_for_input < question.answered < run.resumed` in the persisted Run Event stream.

The complementary deterministic test `TestEngineAnswersNativeQuestionWithoutSecondPrompt` counts native prompt calls and requires exactly one. Together, the Docker Question test and that unit test prove that a real OpenCode Question round trip works and that Agent Board answers it through OpenCode's native reply operation rather than paying for a synthetic continuation prompt.
