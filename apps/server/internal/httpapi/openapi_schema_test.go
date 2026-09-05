package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopedResourceSchemasRequireProjectID(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "packages", "api", "schemas", "control-plane.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	for _, schema := range []string{"ModelProfile", "Runtime", "ExecutorProfile", "Agent"} {
		t.Run(schema, func(t *testing.T) {
			block := topLevelYAMLBlock(doc, schema)
			if block == "" {
				t.Fatalf("schema %s not found", schema)
			}
			if !strings.Contains(block, "required: [") || !strings.Contains(requiredLine(block), "projectId") {
				t.Fatalf("schema %s must require projectId", schema)
			}
			if !strings.Contains(block, "projectId: {type: [string, 'null'], format: uuid}") {
				t.Fatalf("schema %s projectId must remain nullable UUID", schema)
			}
		})
	}
}

func topLevelYAMLBlock(doc, name string) string {
	start := strings.Index(doc, "\n"+name+":\n")
	if start < 0 {
		if strings.HasPrefix(doc, name+":\n") {
			start = -1
		} else {
			return ""
		}
	}
	start++
	rest := doc[start:]
	lines := strings.Split(rest, "\n")
	if len(lines) == 0 || lines[0] != name+":" {
		return ""
	}
	end := len(lines)
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" && lines[i][0] != ' ' {
			end = i
			break
		}
	}
	return strings.Join(lines[:end], "\n")
}

func requiredLine(block string) string {
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "required: [") {
			return line
		}
	}
	return ""
}
