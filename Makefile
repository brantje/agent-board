.PHONY: install-runner uninstall-runner

install-runner:
	sudo install -o root -g root -m 0755 \
		apps/agent-runner/agent-runner \
		/usr/local/bin/agent-runner
	getent passwd agent-runner >/dev/null || \
		sudo useradd --system --user-group \
			--home-dir /var/lib/agent-runner \
			--shell /usr/sbin/nologin \
			agent-runner
	sudo install -d -o root -g root -m 0755 /etc/agent-board
	sudo install -o root -g root -m 0600 \
		apps/agent-runner/deploy/agent-runner.env.example \
		/etc/agent-board/agent-runner.env
	sudo install -o root -g root -m 0644 \
		apps/agent-runner/deploy/agent-runner.service \
		/etc/systemd/system/agent-runner.service
	sudo systemctl daemon-reload
	sudo systemd-analyze verify /etc/systemd/system/agent-runner.service
	sudo systemctl enable --now agent-runner.service

uninstall-runner:
	sudo systemctl disable --now agent-runner.service
	sudo rm -f /etc/systemd/system/agent-runner.service
	sudo rm -f /etc/agent-board/agent-runner.env
	sudo systemctl daemon-reload
