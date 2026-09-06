package evidence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type EventAppender interface {
	AppendEvent(context.Context, store.Event) (store.Event, error)
}

type EventPublisher interface {
	Publish(context.Context, store.Event) error
}

type PublishErrorHandler func(error)

type Recorder struct {
	store              EventAppender
	publisher          EventPublisher
	reportPublishError PublishErrorHandler
}

func NewRecorder(store EventAppender, publisher EventPublisher, publishErrorHandlers ...PublishErrorHandler) (*Recorder, error) {
	if store == nil {
		return nil, fmt.Errorf("evidence: event store is required")
	}
	var report PublishErrorHandler
	if len(publishErrorHandlers) > 0 {
		report = publishErrorHandlers[0]
	}
	return &Recorder{store: store, publisher: publisher, reportPublishError: report}, nil
}

// Record persists before publication. Once the durable Event exists, live
// publication is only a notification and must not turn the committed write into
// a retryable error: subscribers recover from the authoritative Event sequence.
func (r *Recorder) Record(ctx context.Context, event store.Event) (store.Event, error) {
	persisted, err := r.store.AppendEvent(ctx, event)
	if err != nil {
		return store.Event{}, err
	}
	if r.publisher != nil {
		if err := r.publisher.Publish(ctx, persisted); err != nil && r.reportPublishError != nil {
			r.reportPublishError(fmt.Errorf("evidence: publish persisted event: %w", err))
		}
	}
	return persisted, nil
}

func EncodePayload(value any) (json.RawMessage, error) {
	if value == nil {
		return append(json.RawMessage(nil), store.EmptyObject...), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("evidence: encode event payload: %w", err)
	}
	return encoded, nil
}

type ToolPayload struct {
	Kind           string   `json:"kind"`
	Name           string   `json:"name"`
	Command        []string `json:"command,omitempty"`
	CWD            string   `json:"cwd,omitempty"`
	ExitCode       *int     `json:"exitCode,omitempty"`
	OutputChunkIDs []string `json:"outputChunkIds,omitempty"`
}

type TestPayload struct {
	Command        []string `json:"command"`
	Status         string   `json:"status"`
	ExitCode       *int     `json:"exitCode,omitempty"`
	OutputChunkIDs []string `json:"outputChunkIds,omitempty"`
}

type FilePayload struct {
	Path       string `json:"path"`
	OldPath    string `json:"oldPath,omitempty"`
	Staged     bool   `json:"staged,omitempty"`
	Unstaged   bool   `json:"unstaged,omitempty"`
	ArtifactID string `json:"artifactId,omitempty"`
}

type ArtifactPayload struct {
	ArtifactID string `json:"artifactId"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
}
