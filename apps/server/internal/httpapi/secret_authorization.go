package httpapi

import (
	"crypto/subtle"
	"errors"
	"net/http"
)

const (
	SecretWriteCapabilityHeader = "X-Agent-Board-Secret-Write-Token"
	minimumSecretWriteTokenBytes = 32
)

var ErrSecretWriteCapabilityInvalid = errors.New("secret write capability token must be at least 32 bytes")

// SecretWriteAuthorizer protects the privileged secret-write surface without
// coupling handlers to a future users/roles model. projectID is nil for global
// writes and identifies the requested Project for scoped writes.
type SecretWriteAuthorizer interface {
	AuthorizeSecretWrite(*http.Request, *string) bool
}

type deploymentSecretWriteAuthorizer struct {
	token []byte
}

// NewDeploymentSecretWriteAuthorizer creates the v0.1 deployment-admin
// capability check. An empty token leaves secret writes unauthorized; a
// configured token must be long enough to resist trivial guessing.
func NewDeploymentSecretWriteAuthorizer(token string) (SecretWriteAuthorizer, error) {
	if token == "" {
		return nil, nil
	}
	if len(token) < minimumSecretWriteTokenBytes {
		return nil, ErrSecretWriteCapabilityInvalid
	}
	return &deploymentSecretWriteAuthorizer{token: []byte(token)}, nil
}

func (a *deploymentSecretWriteAuthorizer) AuthorizeSecretWrite(r *http.Request, _ *string) bool {
	if a == nil || r == nil {
		return false
	}
	provided := []byte(r.Header.Get(SecretWriteCapabilityHeader))
	return len(provided) == len(a.token) && subtle.ConstantTimeCompare(provided, a.token) == 1
}
