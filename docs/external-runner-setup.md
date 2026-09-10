# External Runner setup on Linux

This is the operator guide for running `agent-runner` as a persistent systemd service on a user-managed Linux host.

An external Runner makes one outbound connection to the Agent Board control plane. It does not need an inbound listening port, PostgreSQL credentials, server encryption keys, or SSH access from Agent Board. The host is a trusted execution environment: coding CLIs run with the permissions of the `agent-runner` service account.

All repository-relative commands below assume the repository root as the current directory.

## Prerequisites

The host needs:

- Linux with systemd
- outbound HTTPS/WSS access to the Agent Board server
- Git
- the coding CLI required by the Engine, currently OpenCode for the v0.1 real Engine path
- a standalone `agent-runner` binary built for the host architecture

The repository does not yet publish standalone Runner release artifacts. Until the release pipeline does that, build the binary from the repository source.

## 1. Build `agent-runner`

From the repository root:

```bash
(
  cd apps/agent-runner
  VERSION=v0.1.0
  CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X github.com/brantje/agent-board/apps/agent-runner/internal/server.Version=${VERSION}" \
    -o agent-runner ./cmd/agent-runner
)
```

Use the real release version for `VERSION` in production. Development builds may use the built-in `dev` version.

## 2. Install the coding CLI

Install every CLI that this Runner is expected to execute somewhere on the service `PATH`. The checked-in systemd unit uses:

```text
/usr/local/bin:/usr/bin:/bin
```

For the current OpenCode Engine, after creating the service user verify that the CLI is visible with:

```bash
sudo -u agent-runner env \
  HOME=/var/lib/agent-runner \
  PATH=/usr/local/bin:/usr/bin:/bin \
  opencode --version
```

Keep the installed OpenCode version aligned with `apps/agent-runner/opencode.env`, which is the repository's pinned compatibility version.

Do not put provider API keys in the systemd unit or Runner env file for the normal Agent Board OpenCode path. Agent Board resolves the configured Provider credential on the trusted server and supplies only the execution-scoped secret required by the session.

## 3. Create a pending Runner and one-time registration token

In Agent Board, open **Settings > Runners** and choose **Create runner**. Creation does not ask for a name. It immediately creates the durable pending external Runner with its immutable Runner ID, but it does not create a permanent Runner credential yet. The one-time registration token is displayed once.

The same operation is available through the API:

```bash
export AGENT_BOARD_URL='https://agent-board.example.com'
curl -fsS -X POST "$AGENT_BOARD_URL/api/runners"
```

The response is shaped like:

```json
{
  "runner": { "id": "..." },
  "registrationToken": "..."
}
```

Agent Board stores only the registration-token hash on that pending Runner. Copy the plaintext registration token directly to the host that will be enrolled; do not put it in source control, scripts, examples, or logs.

Registration updates this same Runner identity. It does not create a second Runner. Reusing the registration token after successful enrollment is rejected.

## 4. Install and enroll the Runner

The repository Make target is the normal source-tree installation path:

```bash
make install-runner
```

It prompts for:

```text
Agent Board URL:
One-time registration token:
```

The registration token is read without terminal echo. The installer then:

1. installs the already-built `apps/agent-runner/agent-runner` binary;
2. creates the non-login `agent-runner` service account when needed;
3. installs the checked-in systemd unit;
4. invokes `agent-runner register` to enroll the already-created pending Runner; and
5. enables and starts `agent-runner.service` only after enrollment succeeds.

`agent-runner register` obtains the machine hostname with `os.Hostname()` and sends it with the one-time registration token. In one transaction the server consumes the registration token, sets that hostname as the initial display name, creates the permanent Runner credential, and marks the same Runner ID registered. The server returns:

```json
{
  "runnerId": "...",
  "runnerToken": "..."
}
```

The binary persists the normal runtime environment configuration in:

```text
/etc/agent-board/agent-runner.env
```

The file contains:

```text
AGENT_BOARD_URL=...
AGENT_RUNNER_ID=...
AGENT_RUNNER_TOKEN=...
AGENT_RUNNER_WORKSPACE_ROOT=...
```

`AGENT_RUNNER_TOKEN` always means the permanent post-registration Runner credential. The environment file is written mode `0600`. The one-time registration token is never persisted. If enrollment fails, the installer exits before enabling or starting the service.

The initial hostname is only the display name. An administrator can rename a registered external Runner later from **Settings > Runners** or with `PATCH /api/runners/{runnerID}`. Renaming does not change Runner identity or credentials, and reconnecting does not overwrite the administrator's name.

### Manual installation

If you do not use the Make target, install the binary, service account, configuration directory and unit first:

```bash
sudo install -o root -g root -m 0755 \
  apps/agent-runner/agent-runner \
  /usr/local/bin/agent-runner

getent passwd agent-runner >/dev/null || \
  sudo useradd --system --user-group \
    --home-dir /var/lib/agent-runner \
    --shell /usr/sbin/nologin \
    agent-runner

sudo install -d -o agent-runner -g agent-runner -m 0750 /var/lib/agent-runner
sudo install -d -o root -g root -m 0755 /etc/agent-board
sudo install -o root -g root -m 0644 \
  apps/agent-runner/deploy/agent-runner.service \
  /etc/systemd/system/agent-runner.service
```

Then enroll the pending Runner:

```bash
sudo /usr/local/bin/agent-runner register
```

Enter the Agent Board URL and one-time registration token when prompted. Direct terminal registration also disables echo while reading the registration token. After registration succeeds:

```bash
sudo systemctl daemon-reload
sudo systemd-analyze verify /etc/systemd/system/agent-runner.service
sudo systemctl enable --now agent-runner.service
```

Normal startup is environment-only: `configFromEnv()` supplies `AGENT_BOARD_URL`, `AGENT_RUNNER_ID`, `AGENT_RUNNER_TOKEN` and the optional Workspace root to `Server.Connect()`. There is no second persisted Runner state/configuration model.

The server-managed internal Runner supplies the same normal runtime environment variables directly and does not use the external registration-token flow.

## 5. Verify the connection

Check systemd first:

```bash
sudo systemctl status agent-runner.service
sudo journalctl -u agent-runner.service -n 100 --no-pager
```

Follow logs while testing:

```bash
sudo journalctl -u agent-runner.service -f
```

Then inspect **Settings > Runners** or query the API:

```bash
curl -fsS "$AGENT_BOARD_URL/api/runners"
```

A healthy registered external Runner should report `connected: true`. Its capabilities include the embedded Runner version and supported Engines. Before enrollment, the pending Runner is visible with its immutable ID but has no display name or permanent credential and cannot authenticate the Runner WebSocket.

If a Project has an explicit external Runner allowlist, add the registered Runner through `PUT /api/projects/{projectID}/runners`. An empty Project allowlist means any connected external Runner is eligible.

## 6. Test a real Run

Assign an Issue to an Agent whose Engine is available on this Runner. For OpenCode, a successful Run should cause the server scheduler to select the connected Runner, transfer the Issue Workspace, start OpenCode inside that transferred Workspace, and sync the resulting filesystem changes back to the authoritative server Workspace.

No Runtime Instance should be created for this external Runner path.

If the Project uses an explicit Runner allowlist, make sure this Runner ID is included before testing.

## Operations

Start, stop, restart and inspect the service with:

```bash
sudo systemctl start agent-runner.service
sudo systemctl stop agent-runner.service
sudo systemctl restart agent-runner.service
sudo systemctl status agent-runner.service
sudo journalctl -u agent-runner.service -f
```

The Runner reconnects outbound after transient transport failures. The server-side reconnect grace is configured with `AGENT_BOARD_RUNNER_RECONNECT_TIMEOUT` and defaults to `5m`.

### Upgrade

Stop the service, replace the binary atomically with a new versioned build, then start it again. The environment-based Runner identity and credential configuration survives binary upgrades:

```bash
sudo systemctl stop agent-runner.service
sudo install -o root -g root -m 0755 \
  apps/agent-runner/agent-runner \
  /usr/local/bin/agent-runner
sudo systemctl start agent-runner.service
sudo systemctl status agent-runner.service
```

### Rotate a Runner credential

The existing administrative endpoint remains:

```text
POST /api/runners/{runnerID}/rotate-token
```

Its credential-bearing response contains `runnerToken`, the new plaintext permanent credential, once. Rotation is valid only for a registered external Runner. An operator must securely replace the `AGENT_RUNNER_TOKEN` value in `/etc/agent-board/agent-runner.env`, preserve mode `0600`, and restart the service. Rotation never creates a new registration token.

### Revoke or remove a Runner

To revoke without deleting its historical identity:

```text
POST /api/runners/{runnerID}/revoke
```

For a pending Runner, revoke permanently invalidates the registration token. For a registered Runner, revoke invalidates its permanent credential and disconnects it.

Deleting the Runner through `DELETE /api/runners/{runnerID}` soft-deletes and revokes it while retaining historical provenance. A deleted pending Runner can never enroll.

To uninstall the service files installed by this repository:

```bash
make uninstall-runner
```

`make uninstall-runner` removes the service and `/etc/agent-board/agent-runner.env` but intentionally does not remove `/var/lib/agent-runner`, which may contain retained session Workspaces.

## Troubleshooting

If the service repeatedly restarts, inspect `journalctl`. Common setup problems are an invalid Agent Board URL, missing `AGENT_BOARD_URL`/`AGENT_RUNNER_ID`/`AGENT_RUNNER_TOKEN` configuration, a revoked/rotated credential, a missing coding CLI on the service `PATH`, or permissions on the Runner Workspace directory.

Verify the exact execution environment with:

```bash
sudo -u agent-runner env \
  HOME=/var/lib/agent-runner \
  PATH=/usr/local/bin:/usr/bin:/bin \
  sh -lc 'id; command -v git; command -v opencode; opencode --version'
```

The Runner requires outbound access to the Agent Board server and any endpoints used by the selected coding CLI/provider. It should not expose a public Runner port and Agent Board does not SSH into the host for execution.
