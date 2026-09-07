FROM golang:1.25-alpine AS build

WORKDIR /src
COPY packages/redact/ ./packages/redact/
COPY packages/runnerprotocol/ ./packages/runnerprotocol/
COPY apps/agent-runner/go.mod apps/agent-runner/go.sum ./apps/agent-runner/
WORKDIR /src/apps/agent-runner
RUN go mod download
COPY apps/agent-runner/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/agent-runner ./cmd/agent-runner

FROM alpine:3.22

ARG OPENCODE_VERSION=1.18.29
ARG OPENCODE_SHA256_AMD64=ca5a08bacb46283eb63645eb2a04dadb4264fbfeb2fc44b9ce689981608f7cc6
ARG OPENCODE_SHA256_ARM64=9b9044ede6449dc34888b85f0a51be5777a5886fe5e1cbd67898b13f8a5bec04
ARG AGENT_BOARD_UID

RUN apk add --no-cache curl git tar libstdc++ libgcc \
    && case "$(uname -m)" in \
         x86_64) \
           opencode_asset="opencode-linux-x64-baseline-musl.tar.gz"; \
           opencode_sha256="${OPENCODE_SHA256_AMD64}" \
           ;; \
         aarch64) \
           opencode_asset="opencode-linux-arm64-musl.tar.gz"; \
           opencode_sha256="${OPENCODE_SHA256_ARM64}" \
           ;; \
         *) \
           echo "unsupported OpenCode architecture: $(uname -m)" >&2; \
           exit 1 \
           ;; \
       esac \
    && curl -fsSL \
         "https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/${opencode_asset}" \
         -o /tmp/opencode.tar.gz \
    && echo "${opencode_sha256}  /tmp/opencode.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/opencode.tar.gz -C /usr/local/bin opencode \
    && chmod 0755 /usr/local/bin/opencode \
    && rm -f /tmp/opencode.tar.gz \
    && test "$(opencode --version)" = "${OPENCODE_VERSION}" \
    && addgroup -S agentboard \
    && if [ -n "${AGENT_BOARD_UID}" ]; then \
         adduser -S -D -H -u "${AGENT_BOARD_UID}" -G agentboard agentboard; \
       else \
         adduser -S -D -H -G agentboard agentboard; \
       fi
COPY --from=build /out/agent-runner /usr/local/bin/agent-runner

USER agentboard
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/agent-runner"]
