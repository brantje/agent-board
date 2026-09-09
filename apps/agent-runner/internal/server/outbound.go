package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	protocol "github.com/brantje/agent-board/packages/runnerprotocol"
	"github.com/gorilla/websocket"
)

// Version is populated with the release version at build time.
var Version = "dev"
var ErrAuthentication = errors.New("runner credentials rejected")

// Connect uses the same session dispatcher across reconnections; process trees
// and pending output outlive a transport interruption.
func (s *Server) Connect(ctx context.Context, serverURL, id, token string) error {
	endpoint, err := url.Parse(serverURL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || id == "" || token == "" {
		return errors.New("invalid runner connection configuration")
	}
	switch endpoint.Scheme {
	case "http":
		endpoint.Scheme = "ws"
	case "https":
		endpoint.Scheme = "wss"
	default:
		return errors.New("server URL must use http or https")
	}
	endpoint.Path = "/api/runner/ws"
	for {
		socket, response, err := websocket.DefaultDialer.DialContext(ctx, endpoint.String(), http.Header{"Authorization": []string{"Bearer " + token}, protocol.RunnerIDHeader: []string{id}})
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		if err == nil {
			stop := context.AfterFunc(ctx, func() { _ = socket.Close() })
			s.serveConnection(socket)
			stop()
		} else if response != nil && (response.StatusCode == 401 || response.StatusCode == 403) {
			s.signalActiveSessions(true)
			return ErrAuthentication
		}
		if ctx.Err() != nil {
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-s.shutdownCtx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
