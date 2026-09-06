module github.com/brantje/agent-board/apps/agent-runner

go 1.25.0

require (
	github.com/brantje/agent-board/packages/redact v0.0.0
	github.com/brantje/agent-board/packages/runnerprotocol v0.0.0
	github.com/gorilla/websocket v1.5.3
)

replace github.com/brantje/agent-board/packages/redact => ../../packages/redact
replace github.com/brantje/agent-board/packages/runnerprotocol => ../../packages/runnerprotocol
