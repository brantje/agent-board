# agent-runner

`agent-runner` is the Engine-neutral execution-plane binary for Agent Board. External persistent hosts run it as a systemd service and connect outbound to the control plane over protocol v2.

External hosts are user-trusted execution environments. Agent Board does not provide or claim Docker isolation on those hosts.

## External Linux installation

The authoritative operator guide is [`docs/external-runner-setup.md`](../../docs/external-runner-setup.md). It covers:

- building/installing the standalone binary
- creating the non-root `agent-runner` service account
- installing OpenCode or another required coding CLI on the service `PATH`
- creating a Runner through `/api/runners` and safely storing its one-time token
- configuring `/etc/agent-board/agent-runner.env`
- installing the checked-in systemd unit
- verifying connectivity and a real Run
- upgrades, token rotation, revocation, decommissioning, and troubleshooting

Deployment examples live in `deploy/`:

```text
deploy/agent-runner.service
deploy/agent-runner.env.example
```

## Build a versioned Linux binary

Local development builds advertise `runner_version=dev`. A release/production build embeds its concrete version into the capability handshake:

```bash
VERSION=v0.1.0
CGO_ENABLED=0 GOOS=linux go build -trimpath \
  -ldflags="-s -w -X github.com/brantje/agent-board/apps/agent-runner/internal/server.Version=${VERSION}" \
  -o agent-runner ./cmd/agent-runner
```

The Docker build accepts the same value through `--build-arg AGENT_RUNNER_VERSION=<version>`. The repository does not yet publish standalone release artifacts; the release pipeline that publishes them must supply a concrete version. This PR intentionally does not add a package repository or shell installer.

## Runtime configuration

The standalone binary reads:

```text
AGENT_BOARD_URL
AGENT_RUNNER_ID
AGENT_RUNNER_TOKEN
AGENT_RUNNER_WORKSPACE_ROOT
```

`AGENT_RUNNER_WORKSPACE_ROOT` is optional and defaults to `/var/lib/agent-runner/workspaces`. `AGENT_BOARD_URL` is the control-plane base HTTP(S) URL; the Runner converts it to WebSocket and connects outbound to `/api/runner/ws`.

The Runner does not listen for Agent Board execution traffic on an inbound host port. SSH may be used by an operator to administer the host, but Agent Board does not use SSH as the execution transport.

## Requirements

- outbound HTTPS/WSS access to the Agent Board server
- Git and the selected Engine CLIs on the service `$PATH`
- a writable workspace/state root for per-session directories

For the current real OpenCode path, keep the host OpenCode version aligned with `opencode.env`. Provider credentials for Agent Board's OpenCode adapter remain server-managed and are supplied to the Runner only for the authorized Execution Session; do not store them in the systemd Runner environment file.
