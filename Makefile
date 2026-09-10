.PHONY: install-runner uninstall-runner

install-runner:
	@set -eu; \
	printf "Agent Board URL: " > /dev/tty; \
	IFS= read -r agent_board_url < /dev/tty; \
	test -n "$$agent_board_url" || { echo "Agent Board URL is required." >&2; exit 1; }; \
	printf "One-time registration token: " > /dev/tty; \
	trap 'stty echo < /dev/tty' 0 1 2 15; \
	stty -echo < /dev/tty; \
	IFS= read -r registration_token < /dev/tty; \
	stty echo < /dev/tty; \
	trap - 0 1 2 15; \
	printf "\n" > /dev/tty; \
	test -n "$$registration_token" || { echo "Runner registration token is required." >&2; exit 1; }; \
	sudo install -o root -g root -m 0755 \
		apps/agent-runner/agent-runner \
		/usr/local/bin/agent-runner; \
	getent passwd agent-runner >/dev/null || \
		sudo useradd --system --user-group \
			--home-dir /var/lib/agent-runner \
			--shell /usr/sbin/nologin \
			agent-runner; \
	sudo install -d -o agent-runner -g agent-runner -m 0750 /var/lib/agent-runner; \
	sudo install -d -o root -g root -m 0755 /etc/agent-board; \
	sudo install -o root -g root -m 0644 \
		apps/agent-runner/deploy/agent-runner.service \
		/etc/systemd/system/agent-runner.service; \
	printf '%s\n%s\n' "$$agent_board_url" "$$registration_token" | \
		sudo /usr/local/bin/agent-runner register; \
	sudo systemctl daemon-reload; \
	sudo systemd-analyze verify /etc/systemd/system/agent-runner.service; \
	sudo systemctl enable --now agent-runner.service

uninstall-runner:
	sudo systemctl disable --now agent-runner.service
	sudo rm -f /etc/systemd/system/agent-runner.service
	sudo rm -f /etc/agent-board/agent-runner.env
	sudo systemctl daemon-reload
