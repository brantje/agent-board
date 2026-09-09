# agent-runner

`agent-runner` is the Engine-neutral execution-plane binary for Agent Board. External persistent hosts run it as a systemd service and connect outbound to the control plane over protocol v2.

External hosts are user-trusted execution environments. Agent Board does not provide or claim Docker isolation on those hosts.

## Install on Linux

1. Build or download the versioned `agent-runner` binary for your architecture.
2. Install it to `/usr/local/bin/agent-runner`.
3. Create the workspace root and service user as needed:

```bash
sudo install -d -m 0750 -o agent-runner -g agent-runner /var/lib/agent-runner/workspaces
```

4. Create an external Runner in Agent Board. Creation returns the immutable Runner ID and a plaintext token once. Store the token immediately; it cannot be retrieved later.
5. Create `/etc/agent-board/agent-runner.env`:

```bash
AGENT_BOARD_URL=https://agent-board.example.invalid
AGENT_RUNNER_ID=<immutable-runner-id-from-agent-board>
AGENT_RUNNER_TOKEN=<one-time-token-from-runner-creation>
AGENT_RUNNER_WORKSPACE_ROOT=/var/lib/agent-runner/workspaces
```

6. Install a supported coding CLI on the host `$PATH` before the runner becomes eligible for that Engine. For OpenCode:

```bash
command -v opencode
opencode --version
```

Authenticate the CLI with your provider on that host. Agent Board sends only execution-scoped secrets for a session; the runner host does not receive PostgreSQL credentials, encryption keys or other control-plane secrets.

7. Install and enable the unit from `deploy/agent-runner.service`:

```bash
sudo install -m 0644 deploy/agent-runner.service /etc/systemd/system/agent-runner.service
sudo systemctl daemon-reload
sudo systemctl enable --now agent-runner.service
```

## Operate

```bash
sudo systemctl start agent-runner.service
sudo systemctl stop agent-runner.service
sudo systemctl restart agent-runner.service
sudo systemctl status agent-runner.service
journalctl -u agent-runner.service -f
```

The runner connects outbound to `$AGENT_BOARD_URL/api/runner/ws`. Do not expose an inbound runner listen port; systemd hosts should not publish `/v1/ws`.

## Upgrade

1. `sudo systemctl stop agent-runner.service`
2. Replace `/usr/local/bin/agent-runner` with the new versioned binary.
3. `sudo systemctl start agent-runner.service`
4. Confirm `systemctl status` and recent `journalctl` output.

Rotating or revoking a runner on the server invalidates the previous token immediately. After rotation, update `AGENT_RUNNER_TOKEN` and restart the service.

## Requirements

- outbound HTTPS/WSS access to the Agent Board server
- Git and the selected Engine CLIs on `$PATH` (for example `opencode` or `sh` for scripted tests)
- a writable workspace root for per-session directories

SSH may be used to administer the host, but Agent Board does not use SSH as an execution transport.
