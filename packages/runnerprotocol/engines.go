package runnerprotocol

import "fmt"

// SupportedEngines is the canonical executable registry, shared by capability
// discovery and server adapter registration. No authentication probes are made.
func SupportedEngines() []EngineExecutable {
	return []EngineExecutable{{ID: "opencode", Executable: "opencode"}, {ID: "scripted", Executable: "sh"}}
}

type EngineExecutable struct {
	ID         string
	Executable string
}

func DiscoverEngines(lookPath func(string) (string, error)) []string {
	found := []string{}
	for _, engine := range SupportedEngines() {
		if _, err := lookPath(engine.Executable); err == nil {
			found = append(found, engine.ID)
		}
	}
	return found
}
func KnownEngine(id string) bool {
	for _, engine := range SupportedEngines() {
		if engine.ID == id {
			return true
		}
	}
	return false
}
func (c Capabilities) Validate() error {
	if c.RunnerVersion == "" || c.OS == "" || c.Architecture == "" || c.MaxActiveSessions < 1 {
		return fmt.Errorf("runner version, OS, architecture and positive capacity are required")
	}
	seen := map[string]bool{}
	for _, id := range c.Engines {
		if !KnownEngine(id) || seen[id] {
			return fmt.Errorf("invalid supported Engine")
		}
		seen[id] = true
	}
	return nil
}
