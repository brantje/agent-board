package evidence

import "github.com/brantje/agent-board/apps/server/internal/store"

func (s *RedactingStore) SetEngineRegistered(registered func(string) bool) {
	if s == nil {
		return
	}
	if target, ok := s.ControlPlaneStore.(store.EngineRegistrationStore); ok {
		target.SetEngineRegistered(registered)
	}
}
