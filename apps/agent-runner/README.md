# agent-runner

`agent-runner` is the Engine-neutral execution-plane binary for Agent Board. External persistent hosts run it as a systemd service and connect outbound to the control plane over protocol v2.

## Install on Linux

1. Build or download the `agent-runner` binary for your architecture.
2. Install it to `/usr/local/bin/agent-runner`.
3. Create the workspace root:

```bash
sudo install -d -m 0750 -o agent-runner -g agent-runner /var/lib/agent-runner/workspaces
```

4. Create `/etc/agent-board/agent-runner.env`:

```bash
AGENT_BOARD_URL=https://agent-board.example.invalid
AGENT_RUNNER_ID=<immutable-runner-id-from-agent-board>
AGENT_RUNNER_TOKEN=<one-time-token-from-runner-creation>
AGENT_RUNNER_WORKSPACE_ROOT=/var/lib/agent-runner/workspaces
```

5. Install and enable the unit from `deploy/agent-runner.service`:

```bash
sudo install -m 0644 deploy/agent-runner.service /etc/systemd/system/agent-runner.service
sudo systemctl daemon-reload
sudo systemctl enable --now agent-runner.service
```

## Upgrade

1. Stop the service.
2. Replace `/usr/local/bin/agent-runner`.
3. Start the service again.

Runner credentials are server-managed. Rotating or revoking a runner invalidates the previous token immediately.

## Requirements

- outbound HTTPS/WSS access to the Agent Board server
- Git and the selected Engine CLIs on `$PATH` (for example `opencode` or `sh` for scripted tests)
- a writable workspace root for per-session directories

SSH may be used to administer the host, but Agent Board does not use SSH as an execution transport.
