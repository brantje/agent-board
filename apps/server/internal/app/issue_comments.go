package app

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type CreateIssueCommentInput struct {
	ProjectID       string
	IssueID         string
	ParentCommentID *string
	AuthorType      string
	AuthorID        string
	Body            string
}

type IssueTimelineEntry struct {
	Kind       string
	ID         string
	OccurredAt time.Time
	Comment    *store.IssueComment
	Event      *store.Event
}

func (s *Service) ListIssueComments(ctx context.Context, projectID, issueID string) ([]store.IssueComment, error) {
	if _, err := s.GetIssue(ctx, projectID, issueID); err != nil {
		return nil, err
	}
	if s.issueComments == nil {
		return nil, errors.New("issue comments are unavailable")
	}
	values, err := s.issueComments.ListIssueComments(ctx, projectID, issueID)
	if err != nil {
		return nil, translateStoreError(err, "issue_comment")
	}
	return values, nil
}

func (s *Service) CreateIssueComment(ctx context.Context, input CreateIssueCommentInput) (store.IssueComment, error) {
	if _, err := s.GetIssue(ctx, input.ProjectID, input.IssueID); err != nil {
		return store.IssueComment{}, err
	}
	if s.issueComments == nil {
		return store.IssueComment{}, errors.New("issue comments are unavailable")
	}
	if strings.TrimSpace(input.Body) == "" || strings.TrimSpace(input.AuthorID) == "" || !store.ValidActorType(input.AuthorType) {
		return store.IssueComment{}, invalid("comment body and author are required")
	}
	if input.ParentCommentID != nil && strings.TrimSpace(*input.ParentCommentID) == "" {
		return store.IssueComment{}, invalid("parent comment id must not be blank")
	}
	result, err := s.issueComments.CreateIssueComment(ctx, input.ProjectID, store.IssueComment{
		IssueID:         input.IssueID,
		ParentCommentID: input.ParentCommentID,
		AuthorType:      input.AuthorType,
		AuthorID:        input.AuthorID,
		Body:            input.Body,
	})
	if err != nil {
		return store.IssueComment{}, translateStoreError(err, "issue_comment")
	}
	publisher, _ := s.events.(persistedEventPublisher)
	publishPersistedEvents(ctx, publisher, result.Events)
	return result.Comment, nil
}

func (s *Service) ListIssueTimeline(ctx context.Context, projectID, issueID string) ([]IssueTimelineEntry, error) {
	comments, err := s.ListIssueComments(ctx, projectID, issueID)
	if err != nil {
		return nil, err
	}
	if s.issueActivity == nil {
		return nil, errors.New("issue activity is unavailable")
	}
	events, err := s.issueActivity.ListIssueTimelineEvents(ctx, projectID, issueID)
	if err != nil {
		return nil, translateStoreError(err, "event")
	}

	entries := make([]IssueTimelineEntry, 0, len(comments)+len(events))
	for index := range comments {
		comment := comments[index]
		entries = append(entries, IssueTimelineEntry{
			Kind:       store.IssueTimelineKindComment,
			ID:         comment.ID,
			OccurredAt: comment.CreatedAt,
			Comment:    &comment,
		})
	}
	for index := range events {
		event := events[index]
		entries = append(entries, IssueTimelineEntry{
			Kind:       store.IssueTimelineKindActivity,
			ID:         event.ID,
			OccurredAt: event.OccurredAt,
			Event:      &event,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].OccurredAt.Equal(entries[j].OccurredAt) {
			return entries[i].OccurredAt.Before(entries[j].OccurredAt)
		}
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, nil
}

