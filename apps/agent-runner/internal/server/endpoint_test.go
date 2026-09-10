package server

import "testing"

func TestAgentBoardEndpointNormalizesAndBuildsHTTPAndWebSocketURLs(t *testing.T) {
	normalized, registration, err := AgentBoardEndpoint(" https://agent-board.example.com/ ", "/api/runner/register", false)
	if err != nil {
		t.Fatal(err)
	}
	if normalized != "https://agent-board.example.com" || registration.String() != "https://agent-board.example.com/api/runner/register" {
		t.Fatalf("registration normalized=%q endpoint=%q", normalized, registration.String())
	}

	normalized, socket, err := AgentBoardEndpoint("http://127.0.0.1:8080/", "/api/runner/ws", true)
	if err != nil {
		t.Fatal(err)
	}
	if normalized != "http://127.0.0.1:8080" || socket.String() != "ws://127.0.0.1:8080/api/runner/ws" {
		t.Fatalf("websocket normalized=%q endpoint=%q", normalized, socket.String())
	}
}

func TestAgentBoardEndpointRejectsAmbiguousOrCredentialBearingURLs(t *testing.T) {
	for _, raw := range []string{
		"",
		"agent-board.example.com",
		"ftp://agent-board.example.com",
		"ws://agent-board.example.com",
		"https://user@agent-board.example.com",
		"https://agent-board.example.com/base",
		"https://agent-board.example.com?token=secret",
		"https://agent-board.example.com/#fragment",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, _, err := AgentBoardEndpoint(raw, "/api/runner/register", false); err == nil {
				t.Fatalf("accepted invalid Agent Board URL %q", raw)
			}
		})
	}
}
