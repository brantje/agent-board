FROM golang:1.25-alpine AS build
WORKDIR /src
COPY packages/runnerprotocol/ ./packages/runnerprotocol/
COPY apps/server/go.mod apps/server/go.sum ./apps/server/
WORKDIR /src/apps/server
RUN go mod download
COPY apps/server/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/agent-board-server ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache git \
    && addgroup -S agentboard \
    && adduser -S -G agentboard agentboard
COPY --from=build /out/agent-board-server /usr/local/bin/agent-board-server
USER agentboard
ENTRYPOINT ["/usr/local/bin/agent-board-server"]
