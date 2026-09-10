package server

import (
	"errors"
	"net/url"
	"strings"
)

// AgentBoardEndpoint validates the configured Agent Board base URL and builds a
// concrete API endpoint. It is shared by enrollment and normal runner connect.
func AgentBoardEndpoint(rawURL, path string, websocket bool) (string, *url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", nil, errors.New("Agent Board URL must be an absolute http or https URL without userinfo, path, query, or fragment")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", nil, errors.New("Agent Board URL must use http or https")
	}
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.ForceQuery = false
	normalized := parsed.String()
	endpoint := *parsed
	endpoint.Path = path
	if websocket {
		if endpoint.Scheme == "http" {
			endpoint.Scheme = "ws"
		} else {
			endpoint.Scheme = "wss"
		}
	}
	return normalized, &endpoint, nil
}
