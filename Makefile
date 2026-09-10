.PHONY: install-runner uninstall-runner

install-runner:
	@printf "Runner registration key: " > /dev/tty; \
	trap 'stty echo < /dev/tty' 0 1 2 15; \
	stty -echo < /dev/tty; \
	IFS= read -r registration_key < /dev/tty; \
	stty echo < /dev/tty; \
	trap - 0 1 2 15; \
	printf "\n" > /dev/tty; \
	test -n "$$registration_key" || { echo "Runner registration key is required." >&2; exit 1; }; \
	sudo install -o root -g root -m 0755 \
		apps/agent-runner/agent-runner \
		/usr/local/bin/agent-runner; \
	getent passwd agent-runner >/dev/null || \
		sudo useradd --system --user-group \
			--home-dir /var/lib/agent-runner \
			--shell /usr/sbin/nologin \
			agent-runner; \
	sudo install -d -o root -g root -m 0755 /etc/agent-board; \
	sudo install -o root -g root -m 0600 \
		apps/agent-runner/deploy/agent-runner.env.example \
		/etc/agent-board/agent-runner.env; \
	sudo sed -i "s|^AGENT_RUNNER_TOKEN=.*|AGENT_RUNNER_TOKEN=$$registration_key|" \
		/etc/agent-board/agent-runner.env; \
	sudo install -o root -g root -m 0644 \
		apps/agent-runner/deploy/agent-runner.service \
		/etc/systemd/system/agent-runner.service; \
	sudo systemctl daemon-reload; \
	sudo systemd-analyze verify /etc/systemd/system/agent-runner.service; \
	sudo systemctl enable --now agent-runner.service

uninstall-runner:
	sudo systemctl disable --now agent-runner.service
	sudo rm -f /etc/systemd/system/agent-runner.service
	sudo rm -f /etc/agent-board/agent-runner.env
	sudo systemctl daemon-reload
