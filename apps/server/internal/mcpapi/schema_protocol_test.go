package mcpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPToolsListAdvertisesLockedInputSchemas(t *testing.T) {
	handler, _, _, token := newProtocolFixture(t)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "agent-board-test", Version: "v0.1.0"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL,
		HTTPClient: &http.Client{Transport: bearerRoundTripper{token: token}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := make(map[string]map[string]any, len(listed.Tools))
	for _, tool := range listed.Tools {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		tools[tool.Name] = schema
	}

	assertSchemaEnum(t, tools["set_issue_status"], []string{"status"}, []string{"BACKLOG", "TODO", "IN_PROGRESS", "BLOCKED", "REVIEW", "DONE"})
	assertSchemaEnum(t, tools["set_issue_assignee"], []string{"assignedTo", "type"}, []string{"USER", "AGENT"})
	assertSchemaEnum(t, tools["create_issue_relationship"], []string{"type"}, []string{"blocks", "depends_on", "related_to", "duplicates"})
	assertSchemaItemsEnum(t, tools["list_questions"], "statuses", []string{"OPEN", "ANSWERED", "CANCELLED"})
	assertSchemaEnum(t, tools["answer_question"], []string{"kind"}, []string{"TEXT", "SINGLE_CHOICE", "MULTI_CHOICE"})
	assertSchemaItemsEnum(t, tools["list_reviews"], "statuses", []string{"PENDING", "APPROVED", "CHANGES_REQUESTED", "CANCELLED"})

	for _, toolName := range []string{"create_issue", "update_issue"} {
		priority := schemaProperty(t, tools[toolName], "priority")
		if got, ok := schemaNumber(priority["minimum"]); !ok || got != 0 {
			t.Fatalf("%s priority minimum = %v (present=%t), want 0", toolName, got, ok)
		}
		if got, ok := schemaNumber(priority["maximum"]); !ok || got != 4 {
			t.Fatalf("%s priority maximum = %v (present=%t), want 4", toolName, got, ok)
		}
	}

	create := tools["create_issue"]
	assertRequired(t, create, []string{"projectId", "title"})
	if _, ok := schemaProperties(create)["status"]; ok {
		t.Fatal("create_issue unexpectedly advertises status")
	}
	for _, optional := range []string{"description", "priority"} {
		if schemaRequired(create, optional) {
			t.Fatalf("create_issue %s unexpectedly required", optional)
		}
	}

	update := tools["update_issue"]
	assertRequired(t, update, []string{"projectId", "issueId"})
	for _, optional := range []string{"title", "description", "priority"} {
		if schemaRequired(update, optional) {
			t.Fatalf("update_issue %s unexpectedly required", optional)
		}
	}

	assignee := schemaProperty(t, tools["set_issue_assignee"], "assignedTo")
	if !schemaAllowsNull(assignee) {
		t.Fatalf("set_issue_assignee assignedTo must accept null: %#v", assignee)
	}
	if !schemaRequired(tools["set_issue_assignee"], "assignedTo") {
		t.Fatal("set_issue_assignee assignedTo must be present so clear is explicit")
	}
}

func assertSchemaEnum(t *testing.T, root map[string]any, path []string, want []string) {
	t.Helper()
	current := root
	for _, name := range path {
		current = schemaPropertyResolved(t, root, current, name)
	}
	assertEnumValues(t, current, path, want)
}

func assertSchemaItemsEnum(t *testing.T, root map[string]any, property string, want []string) {
	t.Helper()
	field := schemaProperty(t, root, property)
	items, ok := field["items"].(map[string]any)
	if !ok {
		t.Fatalf("schema property %q has no object items: %#v", property, field)
	}
	assertEnumValues(t, resolveSchema(root, items), []string{property, "items"}, want)
}

func assertEnumValues(t *testing.T, schema map[string]any, path []string, want []string) {
	t.Helper()
	values, ok := schema["enum"].([]any)
	if !ok {
		t.Fatalf("schema path %v has no enum: %#v", path, schema)
	}
	got := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("schema path %v enum contains non-string %#v", path, value)
		}
		got = append(got, text)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema path %v enum = %v, want %v", path, got, want)
	}
}

func schemaProperty(t *testing.T, schema map[string]any, name string) map[string]any {
	t.Helper()
	return schemaPropertyResolved(t, schema, schema, name)
}

func schemaPropertyResolved(t *testing.T, root, schema map[string]any, name string) map[string]any {
	t.Helper()
	resolved := resolveObjectSchema(root, schema)
	properties := schemaProperties(resolved)
	value, ok := properties[name].(map[string]any)
	if !ok {
		t.Fatalf("schema property %q missing from %#v", name, resolved)
	}
	return resolveSchema(root, value)
}

func schemaProperties(schema map[string]any) map[string]any {
	properties, _ := schema["properties"].(map[string]any)
	return properties
}

func resolveObjectSchema(root, schema map[string]any) map[string]any {
	schema = resolveSchema(root, schema)
	if len(schemaProperties(schema)) > 0 {
		return schema
	}
	for _, keyword := range []string{"anyOf", "oneOf"} {
		branches, _ := schema[keyword].([]any)
		for _, branch := range branches {
			candidate, ok := branch.(map[string]any)
			if !ok {
				continue
			}
			candidate = resolveSchema(root, candidate)
			if len(schemaProperties(candidate)) > 0 {
				return candidate
			}
		}
	}
	return schema
}

func resolveSchema(root, schema map[string]any) map[string]any {
	ref, _ := schema["$ref"].(string)
	const prefix = "#/$defs/"
	if len(ref) <= len(prefix) || ref[:len(prefix)] != prefix {
		return schema
	}
	defs, _ := root["$defs"].(map[string]any)
	resolved, _ := defs[ref[len(prefix):]].(map[string]any)
	if resolved == nil {
		return schema
	}
	return resolved
}

func schemaAllowsNull(schema map[string]any) bool {
	if value, ok := schema["type"].(string); ok {
		return value == "null"
	}
	if values, ok := schema["type"].([]any); ok {
		for _, value := range values {
			if value == "null" {
				return true
			}
		}
	}
	for _, keyword := range []string{"anyOf", "oneOf"} {
		branches, _ := schema[keyword].([]any)
		for _, branch := range branches {
			candidate, ok := branch.(map[string]any)
			if ok && schemaAllowsNull(candidate) {
				return true
			}
		}
	}
	return false
}

func schemaRequired(schema map[string]any, name string) bool {
	values, _ := schema["required"].([]any)
	for _, value := range values {
		if value == name {
			return true
		}
	}
	return false
}

func assertRequired(t *testing.T, schema map[string]any, want []string) {
	t.Helper()
	got := make([]string, 0)
	values, _ := schema["required"].([]any)
	for _, value := range values {
		if text, ok := value.(string); ok {
			got = append(got, text)
		}
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("required = %v, want %v", got, want)
	}
}

func schemaNumber(value any) (float64, bool) {
	number, ok := value.(float64)
	return number, ok
}
