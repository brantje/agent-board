package httpapi

import (
	"context"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func (s *authHTTPStore) CreatePendingUserWithSetupToken(_ context.Context, user store.User, token store.PasswordToken) (store.User, store.PasswordToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	created, err := s.createUserLocked(user)
	if err != nil {
		return store.User{}, store.PasswordToken{}, err
	}
	token.UserID = created.ID
	s.nextID++
	token.ID = "token-" + time.Unix(int64(s.nextID), 0).UTC().Format("150405")
	token.CreatedAt = time.Unix(1, 0).UTC()
	s.tokens[authHTTPHashKey(token.TokenHash)] = token
	return created, token, nil
}
