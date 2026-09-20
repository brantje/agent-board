package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

const (
	issueDiscussionToolName      = "read_issue_discussion"
	issueDiscussionBridgePortEnv = "AGENT_BOARD_ISSUE_DISCUSSION_BRIDGE_PORT"
	issueDiscussionBridgePoll    = 100 * time.Millisecond

	issueDiscussionToolSource = `import { createServer } from "node:http"
import { tool } from "@opencode-ai/plugin"

const port = Number.parseInt(process.env.AGENT_BOARD_ISSUE_DISCUSSION_BRIDGE_PORT ?? "", 10)
if (!Number.isInteger(port) || port <= 0 || port > 65535) {
  throw new Error("read_issue_discussion bridge port is unavailable")
}

const globalKey = "__agentBoardIssueDiscussionBridgeV1"
const root = globalThis as any

function createBridge() {
  const queued = []
  const pending = new Map()

  const server = createServer(async (request, response) => {
    try {
      if (request.method === "GET" && request.url === "/health") {
        response.writeHead(200, { "content-type": "application/json" })
        response.end(JSON.stringify({ ok: true }))
        return
      }
      if (request.method === "GET" && request.url === "/next") {
        if (queued.length === 0) {
          response.writeHead(204)
          response.end()
          return
        }
        response.writeHead(200, { "content-type": "application/json" })
        response.end(JSON.stringify(queued.shift()))
        return
      }
      if (request.method === "POST" && request.url === "/respond") {
        let raw = ""
        for await (const chunk of request) raw += chunk
        const payload = JSON.parse(raw)
        const id = String(payload.id ?? "")
        const waiting = pending.get(id)
        if (!waiting) {
          response.writeHead(404)
          response.end()
          return
        }
        pending.delete(id)
        if (payload.error) waiting.reject(new Error(String(payload.error)))
        else waiting.resolve(JSON.stringify(payload.result))
        response.writeHead(204)
        response.end()
        return
      }
      response.writeHead(404)
      response.end()
    } catch (error) {
      response.writeHead(400, { "content-type": "text/plain" })
      response.end(String(error))
    }
  })
  server.listen(port, "127.0.0.1")

  return {
    enqueue(id, request) {
      if (pending.has(id)) throw new Error("duplicate read_issue_discussion call identity")
      return new Promise((resolve, reject) => {
        pending.set(id, { resolve, reject })
        queued.push({ id, request })
      })
    },
  }
}

const bridge = root[globalKey] ?? (root[globalKey] = createBridge())

export default tool({
  description: "Read bounded trusted discussion context for the current Agent Board Issue. Use recent to orient around active threads, thread to inspect the discussion containing a comment, and updates to fetch collaboration newer than an opaque cursor. Project and Issue scope are server-owned.",
  args: {
    mode: tool.schema.enum(["recent", "thread", "updates"]).describe("Discussion read mode"),
    anchorCommentId: tool.schema.string().optional().describe("Comment ID anchoring thread mode"),
    cursor: tool.schema.string().optional().describe("Opaque cursor returned by an earlier updates read"),
    limit: tool.schema.number().int().positive().optional().describe("Optional bounded result limit"),
  },
  async execute({ mode, anchorCommentId, cursor, limit }, context) {
    const callID = String(context.callID ?? "").trim()
    if (!callID) throw new Error("read_issue_discussion requires a stable call identity")
    return await bridge.enqueue(callID, {
      mode,
      anchorCommentId: String(anchorCommentId ?? "").trim(),
      cursor: String(cursor ?? "").trim(),
      limit: limit ?? 0,
    })
  },
})
`
)

type issueDiscussionBridgeRequest struct {
	ID      string                            `json:"id"`
	Request engine.IssueDiscussionReadRequest `json:"request"`
}

type issueDiscussionBridgeResponse struct {
	ID     string                            `json:"id"`
	Result *engine.IssueDiscussionReadResult `json:"result,omitempty"`
	Error  string                            `json:"error,omitempty"`
}

type issueDiscussionBridge struct {
	http    *http.Client
	baseURL string
	reader  engine.IssueDiscussionReader
}

func issueDiscussionBridgeAddress(nativeAddress, runID string) (string, error) {
	if _, _, err := net.SplitHostPort(nativeAddress); err != nil {
		return "", fmt.Errorf("opencode engine: parse native server address for Issue discussion bridge: %w", err)
	}
	identity := strings.TrimSpace(runID)
	if identity == "" {
		identity = nativeAddress
	}
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte("issue-discussion:"))
	_, _ = hasher.Write([]byte(identity))
	port := nativeServerPortBase + int(hasher.Sum64()%nativeServerPortSpan)
	_, nativePortText, _ := net.SplitHostPort(nativeAddress)
	nativePort, _ := strconv.Atoi(nativePortText)
	if port == nativePort {
		port = nativeServerPortBase + ((port-nativeServerPortBase+1)%nativeServerPortSpan)
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), nil
}

func newIssueDiscussionBridge(connector engine.SessionConnector, address string, reader engine.IssueDiscussionReader) (*issueDiscussionBridge, error) {
	if connector == nil || reader == nil {
		return nil, fmt.Errorf("opencode engine: Issue discussion bridge requires session connector and reader")
	}
	httpClient, err := client.NewSessionHTTPClient(connector, address)
	if err != nil {
		return nil, err
	}
	return &issueDiscussionBridge{http: httpClient, baseURL: "http://" + address, reader: reader}, nil
}

func (b *issueDiscussionBridge) CloseIdleConnections() {
	if b != nil && b.http != nil {
		b.http.CloseIdleConnections()
	}
}

func waitIssueDiscussionBridgeHealthy(ctx context.Context, bridge *issueDiscussionBridge) error {
	deadline, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	var lastErr error
	for {
		attempt, attemptCancel := context.WithTimeout(deadline, startupAttemptTimeout)
		req, err := http.NewRequestWithContext(attempt, http.MethodGet, bridge.baseURL+"/health", nil)
		if err == nil {
			var response *http.Response
			response, err = bridge.http.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				if response.StatusCode == http.StatusOK {
					attemptCancel()
					return nil
				}
				err = fmt.Errorf("HTTP %d", response.StatusCode)
			}
		}
		attemptCancel()
		lastErr = err
		select {
		case <-deadline.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("opencode engine: Issue discussion bridge did not become healthy: %w", lastErr)
		case <-time.After(startupRetryDelay):
		}
	}
}

func (b *issueDiscussionBridge) Serve(ctx context.Context) error {
	for {
		request, found, err := b.next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("opencode engine: read Issue discussion bridge request: %w", err)
		}
		if !found {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(issueDiscussionBridgePoll):
			}
			continue
		}
		result, readErr := b.reader.ReadIssueDiscussion(ctx, request.Request)
		response := issueDiscussionBridgeResponse{ID: request.ID}
		if readErr != nil {
			response.Error = boundedIssueDiscussionToolError(readErr)
		} else {
			response.Result = &result
		}
		if err := b.respond(ctx, response); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("opencode engine: respond to Issue discussion bridge request: %w", err)
		}
	}
}

func (b *issueDiscussionBridge) next(ctx context.Context) (issueDiscussionBridgeRequest, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/next", nil)
	if err != nil {
		return issueDiscussionBridgeRequest{}, false, err
	}
	response, err := b.http.Do(req)
	if err != nil {
		return issueDiscussionBridgeRequest{}, false, err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusNoContent:
		_, _ = io.Copy(io.Discard, response.Body)
		return issueDiscussionBridgeRequest{}, false, nil
	case http.StatusOK:
		var request issueDiscussionBridgeRequest
		if err := json.NewDecoder(response.Body).Decode(&request); err != nil {
			return issueDiscussionBridgeRequest{}, false, err
		}
		if strings.TrimSpace(request.ID) == "" {
			return issueDiscussionBridgeRequest{}, false, fmt.Errorf("request is missing call identity")
		}
		return request, true, nil
	default:
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return issueDiscussionBridgeRequest{}, false, fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
}

func (b *issueDiscussionBridge) respond(ctx context.Context, response issueDiscussionBridgeResponse) error {
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/respond", bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	result, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer result.Body.Close()
	if result.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, result.Body)
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(result.Body, 4<<10))
	return fmt.Errorf("HTTP %d: %s", result.StatusCode, strings.TrimSpace(string(data)))
}

func boundedIssueDiscussionToolError(err error) string {
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "Issue discussion read failed"
	}
	if len(message) > 1024 {
		message = message[:1024]
	}
	return message
}
