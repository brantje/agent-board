# External Runner setup on Linux

This is the operator guide for running `agent-runner` as a persistent systemd service on a user-managed Linux host.

An external Runner makes one outbound connection to the Agent Board control plane. It does not need an inbound listening port, PostgreSQL credentials, server encryption keys, or SSH access from Agent Board. The host is a trusted execution environment: coding CLIs run with the permissions of the `agent-runner` service account.

## Prerequisites

The host needs:

- Linux with systemd
- outbound HTTPS/WSS access to the Agent Board server
- Git
- the coding CLI required by the Engine, currently OpenCode for the v0.1 real Engine path
- a standalone `agent-runner` binary built for the host architecture

The repository does not yet publish standalone Runner release artifacts. Until the release pipeline does that, build the binary from the repository source.

## 1. Build or obtain `agent-runner`

From the repository root:

```bash
cd apps/agent-runner
VERSION=v0.1.0
CGO_ENABLED=0 GOOS=linux go build -trimpath \
  -ldflags="-s -w -X github.com/brantje/agent-board/apps/agent-runner/internal/server.Version=${VERSION}" \
  -o agent-runner ./cmd/agent-runner
```

Use the real release version for `VERSION` in production. Development builds may use the built-in `dev` version.

Install the binary root-owned and executable:

```bash
sudo install -o root -g root -m 0755 agent-runner /usr/local/bin/agent-runner
```

## 2. Create the service account

Create one non-login account for Runner processes:

```bash
getent passwd agent-runner >/dev/null || \
  sudo useradd --system --user-group \
    --home-dir /var/lib/agent-runner \
    --shell /usr/sbin/nologin \
    agent-runner
```

The checked-in systemd unit uses this account. It also asks systemd to create `/var/lib/agent-runner` and gives the service write access there.

Do not run the external Runner as root unless the host is intentionally disposable and you accept that all coding CLI processes will also have root-equivalent access.

## 3. Install the coding CLI

Install every CLI that this Runner is expected to execute somewhere on the service `PATH`. The example unit uses:

```text
/usr/local/bin:/usr/bin:/bin
```

For the current OpenCode Engine, verify that the CLI is visible to the service account:

```bash
sudo -u agent-runner env \
  HOME=/var/lib/agent-runner \
  PATH=/usr/local/bin:/usr/bin:/bin \
  opencode --version
```

Keep the installed OpenCode version aligned with `apps/agent-runner/opencode.env`, which is the repository's pinned compatibility version.

Do not put provider API keys in the systemd unit or Runner env file for the normal Agent Board OpenCode path. Agent Board resolves the configured Provider credential on the trusted server and supplies only the execution-scoped secret required by the session.

If a future Engine requires host-local configuration, install that configuration for the `agent-runner` user under `/var/lib/agent-runner`. Grant extra host permissions only when that Engine actually needs them. For example, adding the service account to the Docker group effectively grants root-level control of the host Docker daemon and should not be a default Runner requirement.

## 4. Create the Runner in Agent Board

Runner management is exposed by the control-plane API at `/api/runners`.

Create the Runner once and capture the returned immutable Runner ID and one-time plaintext token:

```bash
export AGENT_BOARD_URL='https://agent-board.example.com'
umask 077
credentials="$(mktemp)"

curl -fsS \
  -H 'Content-Type: application/json' \
  -d '{"name":"build-host-01"}' \
  "$AGENT_BOARD_URL/api/runners" > "$credentials"

RUNNER_ID="$(jq -r '.runner.id' "$credentials")"
RUNNER_TOKEN="$(jq -r '.token' "$credentials")"
```

The token cannot be retrieved later. Keep it secret and remove the temporary credential file after the service environment has been written.

If a Project has an explicit external Runner allowlist, add this Runner through `PUT /api/projects/{projectID}/runners`. An empty Project allowlist means any connected external Runner is eligible.

## 5. Create the Runner environment file

Create the configuration directory and copy the checked-in example:

```bash
sudo install -d -o root -g root -m 0755 /etc/agent-board
sudo install -o root -g root -m 0600 \
  deploy/agent-runner.env.example \
  /etc/agent-board/agent-runner.env
```

Edit `/etc/agent-board/agent-runner.env` so it contains:

```bash
AGENT_BOARD_URL=https://agent-board.example.com
AGENT_RUNNER_ID=<runner-id>
AGENT_RUNNER_TOKEN=<runner-token>
AGENT_RUNNER_WORKSPACE_ROOT=/var/lib/agent-runner/workspaces
```

`AGENT_BOARD_URL` is the Agent Board base HTTP(S) URL. `agent-runner` converts it to WebSocket and connects to `/api/runner/ws` itself.

When creating the file from the shell variables above, avoid printing the token to terminal logs. After the file is safely written, delete the temporary response:

```bash
rm -f "$credentials"
unset RUNNER_TOKEN
```

Keep `/etc/agent-board/agent-runner.env` root-owned and mode `0600`. The systemd manager reads the file and passes the values to the service; the service account does not need direct read permission on the file.

## 6. Install the systemd unit

The repository contains the production example at `apps/agent-runner/deploy/agent-runner.service`.

From the repository root:

```bash
sudo install -o root -g root -m 0644 \
  apps/agent-runner/deploy/agent-runner.service \
  /etc/systemd/system/agent-runner.service

sudo systemctl daemon-reload
sudo systemd-analyze verify /etc/systemd/system/agent-runner.service
sudo systemctl enable --now agent-runner.service
```

The example unit is:

```ini
[Unit]
Description=Agent Board external agent-runner
Documentation=https://github.com/brantje/agent-board/blob/main/docs/external-runner-setup.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=agent-runner
Group=agent-runner
WorkingDirectory=/var/lib/agent-runner
Environment=HOME=/var/lib/agent-runner
Environment=PATH=/usr/local/bin:/usr/bin:/bin
EnvironmentFile=/etc/agent-board/agent-runner.env
ExecStart=/usr/local/bin/agent-runner
Restart=on-failure
RestartSec=5
KillMode=mixed
TimeoutStopSec=15
UMask=0027
StateDirectory=agent-runner
StateDirectoryMode=0750
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/agent-runner

[Install]
WantedBy=multi-user.target
```

Keep credentials in the environment file rather than embedding them in the unit.

## 7. Verify the connection

Check systemd first:

```bash
sudo systemctl status agent-runner.service
sudo journalctl -u agent-runner.service -n 100 --no-pager
```

Follow logs while testing:

```bash
sudo journalctl -u agent-runner.service -f
```

Then confirm the Runner is reported as connected by Agent Board:

```bash
curl -fsS "$AGENT_BOARD_URL/api/runners" | jq \
  --arg id "$RUNNER_ID" \
  '.[] | select(.id == $id) | {id, name, connected, lastSeenAt, capabilities}'
```

A healthy external Runner should report `connected: true`. Its capabilities include the embedded Runner version and supported Engines.

## 8. Test a real Run

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

Stop the service, replace the binary atomically with a new versioned build, then start it again:

```bash
sudo systemctl stop agent-runner.service
sudo install -o root -g root -m 0755 ./agent-runner /usr/local/bin/agent-runner
sudo systemctl start agent-runner.service
sudo systemctl status agent-runner.service
```

### Rotate a token

Rotate the credential through:

```text
POST /api/runners/{runnerID}/rotate-token
```

The response contains the new plaintext token once. Replace `AGENT_RUNNER_TOKEN` in `/etc/agent-board/agent-runner.env` and restart the service.

### Revoke or remove a Runner

To revoke without deleting its historical identity:

```text
POST /api/runners/{runnerID}/revoke
```

Deleting the Runner through `DELETE /api/runners/{runnerID}` soft-deletes and revokes it while retaining historical provenance.

After decommissioning the host:

```bash
sudo systemctl disable --now agent-runner.service
sudo rm -f /etc/systemd/system/agent-runner.service
sudo rm -f /etc/agent-board/agent-runner.env
sudo systemctl daemon-reload
```

Remove `/var/lib/agent-runner` only after confirming no retained session Workspace is needed for recovery.

## Troubleshooting

If the service repeatedly restarts, inspect `journalctl`. The most common setup problems are an invalid base URL, Runner ID/token mismatch, missing coding CLI on the service `PATH`, or permissions on the Runner state directory.

Verify the exact execution environment with:

```bash
sudo -u agent-runner env \
  HOME=/var/lib/agent-runner \
  PATH=/usr/local/bin:/usr/bin:/bin \
  sh -lc 'id; command -v git; command -v opencode; opencode --version'
```

The Runner requires outbound access to the Agent Board server and any endpoints used by the selected coding CLI/provider. It should not expose a public Runner port and Agent Board does not SSH into the host for execution.
