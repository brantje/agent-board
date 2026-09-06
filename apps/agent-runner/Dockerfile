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

RUN addgroup -S agentboard && adduser -S -G agentboard agentboard
COPY --from=build /out/agent-runner /usr/local/bin/agent-runner

USER agentboard
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/agent-runner"]
