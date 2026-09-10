FROM golang:1.25-alpine AS build

WORKDIR /src
COPY packages/redact/ ./packages/redact/
COPY packages/runnerprotocol/ ./packages/runnerprotocol/
COPY packages/workspacegit/ ./packages/workspacegit/
COPY apps/agent-runner/go.mod apps/agent-runner/go.sum ./apps/agent-runner/
WORKDIR /src/apps/agent-runner
RUN go mod download
COPY apps/agent-runner/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/agent-runner ./cmd/agent-runner

FROM alpine:3.22

ARG AGENT_BOARD_UID
COPY apps/agent-runner/opencode.env /tmp/opencode.env

RUN apk add --no-cache curl git tar libstdc++ libgcc \
    && . /tmp/opencode.env \
    && case "$(uname -m)" in \
         x86_64) \
           opencode_asset="opencode-linux-x64-baseline-musl.tar.gz"; \
           opencode_sha256="${OPENCODE_SHA256_AMD64_MUSL}" \
           ;; \
         aarch64) \
           opencode_asset="opencode-linux-arm64-musl.tar.gz"; \
           opencode_sha256="${OPENCODE_SHA256_ARM64_MUSL}" \
           ;; \
         *) \
           echo "unsupported OpenCode architecture: $(uname -m)" >&2; \
           exit 1 \
           ;; \
       esac \
    && curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors \
         "https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/${opencode_asset}" \
         -o /tmp/opencode.tar.gz \
    && echo "${opencode_sha256}  /tmp/opencode.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/opencode.tar.gz -C /usr/local/bin opencode \
    && chmod 0755 /usr/local/bin/opencode \
    && rm -f /tmp/opencode.tar.gz /tmp/opencode.env \
    && test "$(opencode --version)" = "${OPENCODE_VERSION}" \
    && addgroup -S agentboard \
    && if [ -n "${AGENT_BOARD_UID}" ]; then \
         adduser -S -D -h /home/agentboard -u "${AGENT_BOARD_UID}" -G agentboard agentboard; \
       else \
         adduser -S -D -h /home/agentboard -G agentboard agentboard; \
       fi
COPY --from=build /out/agent-runner /usr/local/bin/agent-runner

ENV HOME=/home/agentboard \
    SHELL=/bin/sh
USER agentboard
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/agent-runner"]
