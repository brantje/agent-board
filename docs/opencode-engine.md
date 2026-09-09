# OpenCode Engine adapter

Agent Board v0.1 uses OpenCode as its first real interactive Engine adapter. OpenCode runs inside the selected Runtime Instance through `agent-runner`; the trusted server never starts OpenCode locally and never exposes the OpenCode HTTP service as a public Agent Board endpoint.

The official `apps/agent-runner/Dockerfile` installs OpenCode from the immutable upstream `v1.18.30` release, selecting the architecture-specific musl asset and verifying its SHA-256 digest before extraction. Engine adapters still depend only on Agent Board Engine capabilities, not on Docker or WebSocket implementation details.

## Native execution path

For an OpenCode Run, the server-side adapter:

1. starts `opencode serve --hostname 127.0.0.1 --port 4096` through the generic Execution Session launcher;
2. opens HTTP connections to that loopback service through the Engine-neutral, session-scoped runner connection capability;
3. creates one native OpenCode session for the Run attempt;
4. sends the issue/agent/review context once as the initial native prompt;
5. consumes OpenCode's SSE event stream and maps explicitly visible activity into canonical Agent Board evidence;
6. reconciles pending native Questions from OpenCode's queryable Question endpoint after an SSE disconnect;
7. stops the native service when the execution reaches its terminal result or the Run is cancelled.

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

## Evidence and reasoning boundary

The adapter may persist explicitly emitted OpenCode messages, plans, rationale, progress, discoveries, summaries, reasoning text, tool activity, files, tests, completion, and failure information when the native protocol exposes them reliably. A finalized native `reasoning` part is stored as canonical `agent.message` evidence with `kind: "reasoning"`; the Run UI presents that emitted trace as a Thought.

This boundary does not authorize Agent Board to infer reasoning from tool calls, reconstruct reasoning that OpenCode did not emit, or make a second model request to classify or manufacture timeline activity. Unclassified visible messages continue to fall back to the generic `agent.message` kind.

OpenCode tool lifecycle Events retain the native call identifier as `toolCallId` and may include sanitized `input`, `summary`, a bounded `resultPreview`, and `reason`. The UI uses `toolCallId` to collapse started/completed/failed lifecycle Events into one logical tool row. Large output remains raw/blob evidence rather than being copied into structured Events.

All structured activity still passes through Agent Board's centralized Run-scoped redacting store before persistence. Reasoning text, tool inputs, summaries, result previews, and failure reasons therefore use the same secret-redaction boundary as other Event payloads.

## Credential-gated real verification

The deterministic unit/integration tests run without provider credentials and cover protocol framing, session-local connectivity, native Question mapping, same-session reply behavior, reconnect reconciliation, stale answers, and audit-event ordering.

A separate manual/credential-gated test proves a real model-backed OpenCode Question round trip through Docker and Agent Board's scheduler:

`TestOpenCodeDockerInteractiveQuestionRoundTrip` in `apps/server/internal/runexec/opencode_integration_test.go`.

The test deliberately resets the PostgreSQL `public` schema. It refuses to do so unless the database is named `agent_board_test` and `AGENT_BOARD_TEST_DATABASE_RESET=1` is set. Never point it at a development or production database.

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

### 3. Run the gated test

Set the provider values to a model/provider that your OpenCode installation supports. `AGENT_BOARD_TEST_OPENCODE_BASE_URL` is optional for compatible/custom provider endpoints.

```sh
cd apps/server

AGENT_BOARD_TEST_OPENCODE=1 \
AGENT_BOARD_TEST_DOCKER=1 \
AGENT_BOARD_TEST_DATABASE_RESET=1 \
AGENT_BOARD_TEST_DATABASE_URL='postgres://agent_board:agent_board@127.0.0.1:5432/agent_board_test?sslmode=disable' \
AGENT_BOARD_TEST_OPENCODE_RUNTIME_IMAGE='agent-board-opencode-runtime:manual' \
AGENT_BOARD_TEST_OPENCODE_PROVIDER_KIND='anthropic' \
AGENT_BOARD_TEST_OPENCODE_MODEL='<model-id>' \
AGENT_BOARD_TEST_OPENCODE_API_KEY='<provider-api-key>' \
go test -count=1 -run '^TestOpenCodeDockerInteractiveQuestionRoundTrip$' -v ./internal/runexec
```

For a custom endpoint, also set:

```sh
AGENT_BOARD_TEST_OPENCODE_BASE_URL='https://provider.example/v1'
```

The test instructs the real OpenCode execution to ask one single-choice native Question before editing. It waits until Agent Board has a durable open Question and the Run is `WAITING_FOR_INPUT`, answers `beta` through `QuestionService`, and then requires:

- the same in-flight Run to continue to `READY_FOR_REVIEW`;
- exactly one OpenCode service Execution Session for the attempt;
- `opencode-result.txt` to contain `beta` followed by a newline;
- `question.created < run.waiting_for_input < question.answered < run.resumed` in the persisted Run Event stream.

The complementary deterministic test `TestEngineAnswersNativeQuestionWithoutSecondPrompt` counts native prompt calls and requires exactly one. Together, the two tests prove that a real OpenCode Question round trip works and that Agent Board answers it through OpenCode's native reply operation rather than paying for a synthetic continuation prompt.
