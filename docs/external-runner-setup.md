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

## 3. Create a one-time registration token

In Agent Board, open **Settings > Runners** and choose **Create runner**. Creation does not ask for a name and does not create the Runner identity yet. It returns one registration token which is displayed once.

The same operation is available through the API:

```bash
export AGENT_BOARD_URL='https://agent-board.example.com'
curl -fsS -X POST "$AGENT_BOARD_URL/api/runners"
```

The response contains only the one-time `token`. Agent Board stores only its hash. Copy the token directly to the host that will be enrolled; do not put it in source control, scripts, examples, or logs.

A registration token creates exactly one Runner. The Runner identity and normal long-lived Runner credential are created only when the host successfully exchanges that token at `POST /api/runner/register`. Reusing the registration token is rejected.

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

The installer then:

1. installs the already-built `apps/agent-runner/agent-runner` binary;
2. creates the non-login `agent-runner` service account when needed;
3. runs `agent-runner register` as that service account;
4. installs the checked-in environment and systemd files; and
5. enables and starts `agent-runner.service`.

`agent-runner register` obtains the machine hostname with `os.Hostname()` and submits it as the initial Runner name. The server returns the immutable Runner ID and a normal long-lived Runner credential. The binary persists those values together with the Agent Board URL in:

```text
/var/lib/agent-runner/runner.json
```

The state file is written mode `0600` inside the private Runner state directory. The one-time registration token is not persisted. After successful registration it cannot be used again.

The initial hostname is only the display name. An administrator can rename a registered external Runner later from **Settings > Runners** or with `PATCH /api/runners/{runnerID}`. Renaming does not change Runner identity or credentials.

### Manual installation

If you do not use the Make target, install the binary and service account first:

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
sudo -u agent-runner /usr/local/bin/agent-runner register
```

Enter the Agent Board URL and one-time registration token when prompted. Then install the environment file and unit:

```bash
sudo install -d -o root -g root -m 0755 /etc/agent-board
sudo install -o root -g root -m 0644 \
  apps/agent-runner/deploy/agent-runner.env.example \
  /etc/agent-board/agent-runner.env
sudo install -o root -g root -m 0644 \
  apps/agent-runner/deploy/agent-runner.service \
  /etc/systemd/system/agent-runner.service
sudo systemctl daemon-reload
sudo systemd-analyze verify /etc/systemd/system/agent-runner.service
sudo systemctl enable --now agent-runner.service
```

The environment file contains only optional runtime configuration such as `AGENT_RUNNER_WORKSPACE_ROOT`; enrolled identity credentials live in the Runner-owned state file instead.

For compatibility and the server-managed internal Runner, supplying all three values `AGENT_BOARD_URL`, `AGENT_RUNNER_ID`, and `AGENT_RUNNER_TOKEN` still overrides persisted enrollment state. External installations should normally use registration instead.

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

A healthy external Runner should report `connected: true`. Its capabilities include the embedded Runner version and supported Engines.

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

Stop the service, replace the binary atomically with a new versioned build, then start it again. Enrollment state survives binary upgrades:

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

Its response contains the new plaintext long-lived credential once. An operator rotating an external Runner must securely replace the `token` value in `/var/lib/agent-runner/runner.json` while preserving ownership and mode `0600`, then restart the service. Rotation is separate from one-time registration; do not create or reuse a registration token for credential rotation.

### Revoke or remove a Runner

To revoke without deleting its historical identity:

```text
POST /api/runners/{runnerID}/revoke
```

Deleting the Runner through `DELETE /api/runners/{runnerID}` soft-deletes and revokes it while retaining historical provenance.

To uninstall the service files installed by this repository:

```bash
make uninstall-runner
```

`make uninstall-runner` intentionally does not remove `/var/lib/agent-runner`. Remove the Runner state directory only after confirming no retained session Workspace or identity state is needed.

## Troubleshooting

If the service repeatedly restarts, inspect `journalctl`. Common setup problems are an invalid Agent Board URL, missing or invalid persisted Runner state, a revoked/rotated credential, a missing coding CLI on the service `PATH`, or permissions on the Runner state directory.

Verify the exact execution environment with:

```bash
sudo -u agent-runner env \
  HOME=/var/lib/agent-runner \
  PATH=/usr/local/bin:/usr/bin:/bin \
  sh -lc 'id; command -v git; command -v opencode; opencode --version'
```

The Runner requires outbound access to the Agent Board server and any endpoints used by the selected coding CLI/provider. It should not expose a public Runner port and Agent Board does not SSH into the host for execution.
