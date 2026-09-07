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

RUN apk add --no-cache bash curl git tar \
    && curl -fsSL https://opencode.ai/install | bash -s -- --version "${OPENCODE_VERSION}" --no-modify-path \
    && cp /root/.opencode/bin/opencode /usr/local/bin/opencode \
    && chmod 0755 /usr/local/bin/opencode \
    && rm -rf /root/.opencode \
    && test "$(opencode --version)" = "${OPENCODE_VERSION}" \
    && addgroup -S agentboard \
    && adduser -S -G agentboard agentboard
COPY --from=build /out/agent-runner /usr/local/bin/agent-runner

USER agentboard
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/agent-runner"]
