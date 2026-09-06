package store

import "context"

type QuestionFilter struct {
	IssueID  *string
	RunID    *string
	Statuses []string
}

type QuestionStore interface {
	CreateQuestion(context.Context, Question) (Question, error)
	GetQuestion(context.Context, string, string) (Question, error)
	ListQuestions(context.Context, string, QuestionFilter) ([]Question, error)
	GetDecisionByQuestion(context.Context, string, string) (Decision, error)
	GetOpenBlockingQuestion(context.Context, string, string) (Question, error)
}
