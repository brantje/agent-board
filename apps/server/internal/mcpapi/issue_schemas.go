package mcpapi

import (
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

type issueToolInputSchemas struct {
	createIssue        *jsonschema.Schema
	updateIssue        *jsonschema.Schema
	setIssueStatus     *jsonschema.Schema
	setIssueAssignee   *jsonschema.Schema
	createRelationship *jsonschema.Schema
}

func lockedIssueToolInputSchemas() issueToolInputSchemas {
	statusTypes := map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[IssueStatus](): {
			Type: "string",
			Enum: []any{"BACKLOG", "TODO", "IN_PROGRESS", "BLOCKED", "REVIEW", "DONE"},
		},
	}
	assigneeTypes := map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[AssigneeType](): {Type: "string", Enum: []any{"USER", "AGENT"}},
	}
	relationshipTypes := map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[RelationshipType](): {Type: "string", Enum: []any{"blocks", "depends_on", "related_to", "duplicates"}},
	}

	createIssue := mustInferToolSchema[CreateIssueInput](nil)
	updateIssue := mustInferToolSchema[UpdateIssueInput](nil)
	constrainIssuePriority(createIssue)
	constrainIssuePriority(updateIssue)

	return issueToolInputSchemas{
		createIssue:        createIssue,
		updateIssue:        updateIssue,
		setIssueStatus:     mustInferToolSchema[SetIssueStatusInput](statusTypes),
		setIssueAssignee:   mustInferToolSchema[SetIssueAssigneeInput](assigneeTypes),
		createRelationship: mustInferToolSchema[CreateRelationshipInput](relationshipTypes),
	}
}

func mustInferToolSchema[T any](typeSchemas map[reflect.Type]*jsonschema.Schema) *jsonschema.Schema {
	schema, err := jsonschema.For[T](&jsonschema.ForOptions{TypeSchemas: typeSchemas})
	if err != nil {
		panic(fmt.Sprintf("mcp: infer tool schema: %v", err))
	}
	return schema
}

func constrainIssuePriority(schema *jsonschema.Schema) {
	priority := schema.Properties["priority"]
	if priority == nil {
		panic("mcp: inferred Issue schema has no priority property")
	}
	priority.Minimum = jsonschema.Ptr(0.0)
	priority.Maximum = jsonschema.Ptr(4.0)
}
