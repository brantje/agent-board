package opencode

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

const openCodeIsolationRoot = "/tmp/agent-board-opencode"

// openCodeProcessIsolationEnvironment keeps OpenCode's process-global state
// private to one durable Run. OpenCode uses XDG state for file locks and the
// other XDG roots for process-global data, config, and caches, so isolating
// only its SQLite database is not sufficient when several capacity-1 Runners
// share one Linux host.
func openCodeProcessIsolationEnvironment(safe executioncontext.SafeContext) map[string]string {
	env := map[string]string{"OPENCODE_DB": ":memory:"}
	runID := strings.TrimSpace(safe.Run.ID)
	if runID == "" {
		return env
	}

	digest := sha256.Sum256([]byte(runID))
	root := path.Join(openCodeIsolationRoot, hex.EncodeToString(digest[:]))
	env["XDG_DATA_HOME"] = path.Join(root, "data")
	env["XDG_CONFIG_HOME"] = path.Join(root, "config")
	env["XDG_CACHE_HOME"] = path.Join(root, "cache")
	env["XDG_STATE_HOME"] = path.Join(root, "state")
	return env
}
