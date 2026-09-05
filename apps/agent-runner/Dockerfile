FROM golang:1.25-alpine AS build

WORKDIR /src/apps/agent-runner
COPY apps/agent-runner/go.mod ./
COPY apps/agent-runner/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/agent-runner ./cmd/agent-runner

FROM alpine:3.22

RUN addgroup -S agentboard && adduser -S -G agentboard agentboard
COPY --from=build /out/agent-runner /usr/local/bin/agent-runner

USER agentboard
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/agent-runner"]
