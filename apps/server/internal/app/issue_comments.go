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
	ProjectID        string
	IssueID          string
	ParentCommentID  *string
	Body             string
	RequestKey       string
	MentionTargets   []store.IssueCommentTarget
	MentionAgentIDs  []string
	SuppressImplicit bool
}

type issueCommentCreateInput struct {
	CreateIssueCommentInput
	AuthorType      string
	AuthorID        string
	SourceRunID     *string
	SourceActionKey *string
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

func (s *Service) PreviewIssueCommentMentions(ctx context.Context, projectID, issueID string, targetAgentIDs []string) ([]store.IssueCommentMentionPreview, error) {
	if _, err := s.GetIssue(ctx, projectID, issueID); err != nil {
		return nil, err
	}
	mentions, ok := any(s.issueComments).(store.IssueCommentMentionStore)
	if !ok {
		return nil, errors.New("issue comment mentions are unavailable")
	}
	values, err := mentions.PreviewIssueCommentMentions(ctx, projectID, issueID, targetAgentIDs)
	if err != nil {
		return nil, translateStoreError(err, "issue_comment_mention")
	}
	return values, nil
}

func (s *Service) PreviewIssueCommentTargets(ctx context.Context, projectID, issueID string, targets []store.IssueCommentTarget) ([]store.IssueCommentMentionPreview, error) {
	if _, err := s.GetIssue(ctx, projectID, issueID); err != nil {
		return nil, err
	}
	mentions, ok := any(s.issueComments).(store.TypedIssueCommentMentionStore)
	if !ok {
		return nil, errors.New("typed issue comment mentions are unavailable")
	}
	values, err := mentions.PreviewIssueCommentTargets(ctx, projectID, issueID, targets)
	if err != nil {
		return nil, translateStoreError(err, "issue_comment_mention")
	}
	return values, nil
}

func (s *Service) PreviewIssueCommentTriggers(
	ctx context.Context,
	projectID, issueID string,
	parentCommentID *string,
	body string,
	request store.IssueCommentTriggerRequest,
) (store.IssueCommentTriggerPreview, error) {
	if _, err := s.GetIssue(ctx, projectID, issueID); err != nil {
		return store.IssueCommentTriggerPreview{}, err
	}
	triggers, ok := any(s.issueComments).(store.IssueCommentTriggerStore)
	if !ok {
		return store.IssueCommentTriggerPreview{}, errors.New("issue comment triggers are unavailable")
	}
	value, err := triggers.PreviewIssueCommentTriggers(ctx, projectID, issueID, parentCommentID, body, request)
	if err != nil {
		return store.IssueCommentTriggerPreview{}, translateStoreError(err, "issue_comment_trigger")
	}
	return value, nil
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

func (s *Service) CreateHumanIssueComment(ctx context.Context, input CreateIssueCommentInput, authorID string) (store.IssueComment, error) {
	requestKey := strings.TrimSpace(input.RequestKey)
	if requestKey == "" {
		return store.IssueComment{}, invalid("comment request key is required")
	}
	sourceActionKey := &requestKey
	return s.createIssueComment(ctx, issueCommentCreateInput{
		CreateIssueCommentInput: input,
		AuthorType:              store.ActorTypeHuman,
		AuthorID:                authorID,
		SourceActionKey:         sourceActionKey,
	})
}

func (s *Service) PublishAgentIssueComment(ctx context.Context, projectID, runID, requestKey, body string, mentionAgentIDs []string) (store.IssueComment, error) {
	requestKey = strings.TrimSpace(requestKey)
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(runID) == "" || requestKey == "" {
		return store.IssueComment{}, invalid("project and Run are required")
	}
	if len(mentionAgentIDs) > 1 {
		return store.IssueComment{}, invalid("Agent-authored Issue comments support at most one structured Agent mention")
	}
	run, err := s.GetRun(ctx, projectID, runID)
	if err != nil {
		return store.IssueComment{}, err
	}
	if run.AgentID == nil || strings.TrimSpace(*run.AgentID) == "" {
		return store.IssueComment{}, NewError("invalid_argument", "Run has no Agent identity", store.ErrInvalidArgument)
	}
	sourceRunID := run.ID
	sourceActionKey := requestKey
	return s.createIssueComment(ctx, issueCommentCreateInput{
		CreateIssueCommentInput: CreateIssueCommentInput{
			ProjectID:       projectID,
			IssueID:         run.IssueID,
			Body:            body,
			MentionAgentIDs: append([]string(nil), mentionAgentIDs...),
		},
		AuthorType:      store.ActorTypeAgent,
		AuthorID:        *run.AgentID,
		SourceRunID:     &sourceRunID,
		SourceActionKey: &sourceActionKey,
	})
}

func (s *Service) PublishAgentIssueCommentTargets(ctx context.Context, projectID, runID, requestKey, body string, targets []store.IssueCommentTarget) (store.IssueComment, error) {
	requestKey = strings.TrimSpace(requestKey)
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(runID) == "" || requestKey == "" {
		return store.IssueComment{}, invalid("project and Run are required")
	}
	if len(targets) > 1 {
		return store.IssueComment{}, invalid("Agent-authored Issue comments support at most one structured target")
	}
	run, err := s.GetRun(ctx, projectID, runID)
	if err != nil {
		return store.IssueComment{}, err
	}
	if run.AgentID == nil || strings.TrimSpace(*run.AgentID) == "" {
		return store.IssueComment{}, NewError("invalid_argument", "Run has no Agent identity", store.ErrInvalidArgument)
	}
	sourceRunID := run.ID
	sourceActionKey := requestKey
	return s.createIssueComment(ctx, issueCommentCreateInput{
		CreateIssueCommentInput: CreateIssueCommentInput{
			ProjectID: projectID, IssueID: run.IssueID, Body: body,
			MentionTargets: append([]store.IssueCommentTarget(nil), targets...),
		},
		AuthorType: store.ActorTypeAgent, AuthorID: *run.AgentID,
		SourceRunID: &sourceRunID, SourceActionKey: &sourceActionKey,
	})
}

func (s *Service) createIssueComment(ctx context.Context, input issueCommentCreateInput) (store.IssueComment, error) {
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
	commentInput := store.IssueComment{
		IssueID:         input.IssueID,
		ParentCommentID: input.ParentCommentID,
		AuthorType:      input.AuthorType,
		AuthorID:        input.AuthorID,
		SourceRunID:     input.SourceRunID,
		SourceActionKey: input.SourceActionKey,
		Body:            input.Body,
	}
	var result store.IssueCommentMutationResult
	var err error
	if input.AuthorType == store.ActorTypeHuman {
		triggers, ok := any(s.issueComments).(store.IssueCommentTriggerStore)
		if !ok {
			return store.IssueComment{}, errors.New("issue comment triggers are unavailable")
		}
		result, err = triggers.CreateIssueCommentWithTriggers(ctx, input.ProjectID, commentInput, store.IssueCommentTriggerRequest{
			MentionTargets: input.MentionTargets, MentionAgentIDs: input.MentionAgentIDs, SuppressImplicit: input.SuppressImplicit,
		})
	} else if len(input.MentionTargets) != 0 || len(input.MentionAgentIDs) != 0 {
		if input.SourceActionKey == nil {
			return store.IssueComment{}, invalid("structured Agent mentions require a stable request key")
		}
		mentions, ok := any(s.issueComments).(store.IssueCommentMentionStore)
		if !ok {
			return store.IssueComment{}, errors.New("issue comment mentions are unavailable")
		}
		if typed, ok := any(s.issueComments).(store.TypedIssueCommentMentionStore); ok && len(input.MentionTargets) != 0 {
			result, err = typed.CreateIssueCommentWithTargets(ctx, input.ProjectID, commentInput, input.MentionTargets)
		} else {
			result, err = mentions.CreateIssueCommentWithMentions(ctx, input.ProjectID, commentInput, input.MentionAgentIDs)
		}
	} else {
		result, err = s.issueComments.CreateIssueComment(ctx, input.ProjectID, commentInput)
	}
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
	if comment.DeletedAt != nil {
		return store.IssueComment{}, NewError("conflict", "deleted comments cannot be changed", store.ErrConflict)
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
	if err := requireIssueCommentAuthor(comment, actorID); err != nil {
		return err
	}
	if comment.DeletedAt != nil {
		return nil
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
