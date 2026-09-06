package store

import "context"

type QuestionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type QuestionAnswer struct {
	Kind      string   `json:"kind"`
	Text      *string  `json:"text,omitempty"`
	OptionIDs []string `json:"optionIds,omitempty"`
}

type QuestionFilter struct {
	IssueID  *string
	RunID    *string
	Statuses []string
}

type AnswerQuestionCommand struct {
	ProjectID  string
	QuestionID string
	Answer     QuestionAnswer
	ActorType  string
	ActorID    *string
}

type AnswerQuestionResult struct {
	Question Question
	Decision Decision
	Run      Run
	Job      *SchedulerJob
}

type QuestionStore interface {
	CreateQuestion(context.Context, Question) (Question, error)
	GetQuestion(context.Context, string, string) (Question, error)
	ListQuestions(context.Context, string, QuestionFilter) ([]Question, error)
	GetDecisionByQuestion(context.Context, string, string) (Decision, error)
	GetOpenBlockingQuestion(context.Context, string, string) (Question, error)
	AnswerQuestion(context.Context, AnswerQuestionCommand) (AnswerQuestionResult, error)
}
