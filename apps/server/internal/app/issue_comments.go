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

func (s *Service) GetIssueComment(ctx context.Context, projectID, issueID, commentID string) (store.IssueComment, error) {
	if _, err := s.GetIssue(ctx, projectID, issueID); err != nil {
		return store.IssueComment{}, err
	}
	if s.issueComments == nil {
		return store.IssueComment{}, errors.New("issue comments are unavailable")
	}
	value, err := s.issueComments.GetIssueComment(ctx, projectID, issueID, commentID)
	return value, translateStoreError(err, "issue_comment")
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
	s.publishIssueCommentEvents(ctx, result.Events)
	return result.Comment, nil
}

func (s *Service) UpdateIssueComment(ctx context.Context, projectID, issueID, commentID, actorID, body string) (store.IssueComment, error) {
	if strings.TrimSpace(body) == "" {
		return store.IssueComment{}, invalid("comment body is required")
	}
	comment, err := s.GetIssueComment(ctx, projectID, issueID, commentID)
	if err != nil {
		return store.IssueComment{}, err
	}
	if err := requireIssueCommentAuthor(comment, actorID); err != nil {
		return store.IssueComment{}, err
	}
	result, err := s.issueComments.UpdateIssueComment(ctx, projectID, issueID, commentID, actorID, body)
	if err != nil {
		return store.IssueComment{}, translateStoreError(err, "issue_comment")
	}
	s.publishIssueCommentEvents(ctx, result.Events)
	return result.Comment, nil
}

func (s *Service) DeleteIssueComment(ctx context.Context, projectID, issueID, commentID, actorID string) error {
	comment, err := s.GetIssueComment(ctx, projectID, issueID, commentID)
	if err != nil {
		return err
	}
	if comment.DeletedAt != nil {
		return nil
	}
	if err := requireIssueCommentAuthor(comment, actorID); err != nil {
		return err
	}
	result, err := s.issueComments.DeleteIssueComment(ctx, projectID, issueID, commentID, actorID)
	if err != nil {
		return translateStoreError(err, "issue_comment")
	}
	s.publishIssueCommentEvents(ctx, result.Events)
	return nil
}

func (s *Service) ResolveIssueComment(ctx context.Context, projectID, issueID, commentID, actorID string) (store.IssueComment, error) {
	comment, err := s.GetIssueComment(ctx, projectID, issueID, commentID)
	if err != nil {
		return store.IssueComment{}, err
	}
	if err := validateIssueCommentThreadMutation(comment, actorID); err != nil {
		return store.IssueComment{}, err
	}
	result, err := s.issueComments.ResolveIssueComment(ctx, projectID, issueID, commentID, actorID)
	if err != nil {
		return store.IssueComment{}, translateStoreError(err, "issue_comment")
	}
	s.publishIssueCommentEvents(ctx, result.Events)
	return result.Comment, nil
}

func (s *Service) ReopenIssueComment(ctx context.Context, projectID, issueID, commentID, actorID string) (store.IssueComment, error) {
	comment, err := s.GetIssueComment(ctx, projectID, issueID, commentID)
	if err != nil {
		return store.IssueComment{}, err
	}
	if err := validateIssueCommentThreadMutation(comment, actorID); err != nil {
		return store.IssueComment{}, err
	}
	result, err := s.issueComments.ReopenIssueComment(ctx, projectID, issueID, commentID, actorID)
	if err != nil {
		return store.IssueComment{}, translateStoreError(err, "issue_comment")
	}
	s.publishIssueCommentEvents(ctx, result.Events)
	return result.Comment, nil
}

func (s *Service) AddIssueCommentReaction(ctx context.Context, projectID, issueID, commentID, actorID, reaction string) error {
	if err := validateIssueCommentReactionActor(actorID, reaction); err != nil {
		return err
	}
	comment, err := s.GetIssueComment(ctx, projectID, issueID, commentID)
	if err != nil {
		return err
	}
	if comment.DeletedAt != nil {
		return NewError("conflict", "deleted comments cannot receive reactions", store.ErrConflict)
	}
	events, err := s.issueComments.AddIssueCommentReaction(ctx, projectID, issueID, commentID, actorID, reaction)
	if err != nil {
		return translateStoreError(err, "issue_comment")
	}
	s.publishIssueCommentEvents(ctx, events)
	return nil
}

func (s *Service) RemoveIssueCommentReaction(ctx context.Context, projectID, issueID, commentID, actorID, reaction string) error {
	if err := validateIssueCommentReactionActor(actorID, reaction); err != nil {
		return err
	}
	comment, err := s.GetIssueComment(ctx, projectID, issueID, commentID)
	if err != nil {
		return err
	}
	if comment.DeletedAt != nil {
		return NewError("conflict", "deleted comments cannot receive reactions", store.ErrConflict)
	}
	events, err := s.issueComments.RemoveIssueCommentReaction(ctx, projectID, issueID, commentID, actorID, reaction)
	if err != nil {
		return translateStoreError(err, "issue_comment")
	}
	s.publishIssueCommentEvents(ctx, events)
	return nil
}

func requireIssueCommentAuthor(comment store.IssueComment, actorID string) error {
	if strings.TrimSpace(actorID) == "" {
		return invalid("comment actor is required")
	}
	if comment.DeletedAt != nil {
		return NewError("conflict", "deleted comments cannot be changed", store.ErrConflict)
	}
	if comment.AuthorType != store.ActorTypeHuman || comment.AuthorID != actorID {
		return NewError("forbidden", "comment can only be changed by its original human author", store.ErrInvalidArgument)
	}
	return nil
}

func validateIssueCommentThreadMutation(comment store.IssueComment, actorID string) error {
	if strings.TrimSpace(actorID) == "" {
		return invalid("comment actor is required")
	}
	if comment.DeletedAt != nil {
		return NewError("conflict", "deleted discussions cannot be resolved or reopened", store.ErrConflict)
	}
	if comment.ParentCommentID != nil {
		return invalid("only a top-level discussion can be resolved or reopened")
	}
	return nil
}

func validateIssueCommentReactionActor(actorID, reaction string) error {
	if strings.TrimSpace(actorID) == "" {
		return invalid("comment actor is required")
	}
	if !store.ValidIssueCommentReaction(reaction) {
		return invalid("unsupported comment reaction")
	}
	return nil
}

func (s *Service) publishIssueCommentEvents(ctx context.Context, events []store.Event) {
	publisher, _ := s.events.(persistedEventPublisher)
	publishPersistedEvents(ctx, publisher, events)
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
