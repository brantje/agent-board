package mcpapi

import "github.com/google/jsonschema-go/jsonschema"

type questionToolInputSchemas struct {
	listQuestions  *jsonschema.Schema
	answerQuestion *jsonschema.Schema
}

type reviewToolInputSchemas struct {
	listReviews *jsonschema.Schema
}

func lockedQuestionToolInputSchemas() questionToolInputSchemas {
	listQuestions := mustInferToolSchema[ListQuestionsInput](nil)
	constrainStringArrayEnum(listQuestions, "statuses", []any{"OPEN", "ANSWERED", "CANCELLED"})
	answerQuestion := mustInferToolSchema[AnswerQuestionInput](nil)
	constrainStringEnum(answerQuestion, "kind", []any{"TEXT", "SINGLE_CHOICE", "MULTI_CHOICE"})
	return questionToolInputSchemas{listQuestions: listQuestions, answerQuestion: answerQuestion}
}

func lockedReviewToolInputSchemas() reviewToolInputSchemas {
	listReviews := mustInferToolSchema[ListReviewsInput](nil)
	constrainStringArrayEnum(listReviews, "statuses", []any{"PENDING", "APPROVED", "CHANGES_REQUESTED", "CANCELLED"})
	return reviewToolInputSchemas{listReviews: listReviews}
}

func constrainStringEnum(schema *jsonschema.Schema, property string, values []any) {
	field := schema.Properties[property]
	if field == nil {
		panic("mcp: inferred schema has no " + property + " property")
	}
	field.Enum = append([]any(nil), values...)
}

func constrainStringArrayEnum(schema *jsonschema.Schema, property string, values []any) {
	field := schema.Properties[property]
	if field == nil || field.Items == nil {
		panic("mcp: inferred schema has no " + property + " items")
	}
	field.Items.Enum = append([]any(nil), values...)
}
