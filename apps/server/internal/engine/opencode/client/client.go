package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL             = "http://opencode.local"
	defaultSessionAddress      = "127.0.0.1:4096"
	maxErrorBody               = 4 << 10
	waitAvailabilityRetryDelay = 100 * time.Millisecond
)

type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type HTTPError struct {
	StatusCode int
	Method     string
	Path       string
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("opencode: %s %s returned HTTP %d", e.Method, e.Path, e.StatusCode)
	}
	return fmt.Sprintf("opencode: %s %s returned HTTP %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

type Client struct {
	http    *http.Client
	baseURL string
}

func New(httpClient *http.Client, baseURL string) (*Client, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("opencode: HTTP client is required")
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("opencode: valid base URL is required")
	}
	return &Client{http: httpClient, baseURL: baseURL}, nil
}

// NewSession creates an HTTP client whose TCP connections are opened through
// the Execution Session-local connector. The URL host is deliberately
// synthetic; every dial is redirected to the explicitly allowed loopback
// address inside the Runtime.
func NewSession(dialer Dialer, address string) (*Client, error) {
	if dialer == nil {
		return nil, fmt.Errorf("opencode: session dialer is required")
	}
	if strings.TrimSpace(address) == "" {
		address = defaultSessionAddress
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},
		ForceAttemptHTTP2: false,
	}
	return New(&http.Client{Transport: transport}, defaultBaseURL)
}

func (c *Client) CloseIdleConnections() {
	if c == nil || c.http == nil {
		return
	}
	c.http.CloseIdleConnections()
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var health Health
	err := c.doJSON(ctx, http.MethodGet, "/api/health", nil, &health)
	var httpErr *HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
		err = c.doJSON(ctx, http.MethodGet, "/global/health", nil, &health)
	}
	if err != nil {
		return Health{}, err
	}
	if !health.Healthy {
		return Health{}, fmt.Errorf("opencode: server reported unhealthy")
	}
	return health, nil
}

func (c *Client) CreateSession(ctx context.Context, request CreateSessionRequest) (Session, error) {
	if strings.TrimSpace(request.Directory) == "" || strings.TrimSpace(request.Model.ID) == "" || strings.TrimSpace(request.Model.ProviderID) == "" {
		return Session{}, fmt.Errorf("opencode: session directory, provider and model are required")
	}
	payload := struct {
		Model    ModelRef `json:"model"`
		Location struct {
			Directory string `json:"directory"`
		} `json:"location"`
	}{Model: request.Model}
	payload.Location.Directory = request.Directory
	var response struct {
		Data Session `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/session", payload, &response); err != nil {
		return Session{}, err
	}
	if strings.TrimSpace(response.Data.ID) == "" {
		return Session{}, fmt.Errorf("opencode: create session response has no id")
	}
	return response.Data, nil
}

func (c *Client) Prompt(ctx context.Context, sessionID, text string) error {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(text) == "" {
		return fmt.Errorf("opencode: session id and prompt are required")
	}
	payload := struct {
		Prompt struct {
			Text string `json:"text"`
		} `json:"prompt"`
		Delivery string `json:"delivery"`
	}{}
	payload.Prompt.Text = text
	// OpenCode v1.18.29 accepts only "steer" or "queue". "steer" is also
	// the native default and starts an idle session immediately without adding
	// a second synthetic user turn later.
	payload.Delivery = "steer"
	return c.doJSON(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/prompt", payload, nil)
}

func (c *Client) WaitSession(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("opencode: session id is required")
	}
	path := "/api/session/" + url.PathEscape(sessionID) + "/wait"
	for {
		err := c.doJSON(ctx, http.MethodPost, path, nil, nil)
		if err == nil || !isWaitOperationUnavailable(err) {
			return err
		}

		timer := time.NewTimer(waitAvailabilityRetryDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func isWaitOperationUnavailable(err error) bool {
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusServiceUnavailable {
		return false
	}
	var body struct {
		Tag     string `json:"_tag"`
		Service string `json:"service"`
	}
	if json.Unmarshal([]byte(httpErr.Body), &body) != nil {
		return false
	}
	return body.Tag == "ServiceUnavailableError" && body.Service == "session.wait"
}

func (c *Client) InterruptSession(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("opencode: session id is required")
	}
	return c.doJSON(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/interrupt", nil, nil)
}

func (c *Client) ListQuestions(ctx context.Context, sessionID string) ([]QuestionRequest, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("opencode: session id is required")
	}
	var response struct {
		Data []QuestionRequest `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/session/"+url.PathEscape(sessionID)+"/question", nil, &response); err != nil {
		return nil, err
	}
	return response.Data, nil
}

func (c *Client) ReplyQuestion(ctx context.Context, sessionID, requestID string, answers [][]string) error {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(requestID) == "" {
		return fmt.Errorf("opencode: session id and question request id are required")
	}
	payload := struct {
		Answers [][]string `json:"answers"`
	}{Answers: answers}
	return c.doJSON(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/question/"+url.PathEscape(requestID)+"/reply", payload, nil)
}

func (c *Client) RejectQuestion(ctx context.Context, sessionID, requestID string) error {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(requestID) == "" {
		return fmt.Errorf("opencode: session id and question request id are required")
	}
	return c.doJSON(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/question/"+url.PathEscape(requestID)+"/reject", nil, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload, output any) error {
	if c == nil || c.http == nil {
		return fmt.Errorf("opencode: client is unavailable")
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("opencode: encode %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("opencode: build %s %s: %w", method, path, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("opencode: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return &HTTPError{StatusCode: resp.StatusCode, Method: method, Path: path, Body: strings.TrimSpace(string(data))}
	}
	if output == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
		return fmt.Errorf("opencode: decode %s %s response: %w", method, path, err)
	}
	return nil
}
