package httpapi

import (
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

func issueCommentTargetRequests(values []IssueCommentTargetRequest) ([]store.IssueCommentTarget, bool) {
	if len(values) > store.MaxIssueCommentMentions {
		return nil, false
	}
	result := make([]store.IssueCommentTarget, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value.Type != store.IssueCommentTargetTypeAgent && value.Type != store.IssueCommentTargetTypeSquad {
			return nil, false
		}
		if !validUUID(value.ID) {
			return nil, false
		}
		key := value.Type + ":" + value.ID
		if _, exists := seen[key]; exists {
			return nil, false
		}
		seen[key] = struct{}{}
		result = append(result, store.IssueCommentTarget{Type: value.Type, ID: value.ID})
	}
	return result, true
}

func (a *api) listIssueComments(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueKey := chi.URLParam(r, "issueID")
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}

	var values []store.IssueComment
	var err error
	viewerID := ""
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		viewerID = actor.ID
		values, err = a.projectAccess.ListIssueComments(r.Context(), actor, projectID, issueUUID)
	} else {
		values, err = a.service.ListIssueComments(r.Context(), projectID, issueUUID)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := make([]IssueCommentDTO, 0, len(values))
	for _, value := range values {
		out = append(out, issueCommentDTO(value, issueKey, viewerID))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) createIssueComment(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueKey := chi.URLParam(r, "issueID")
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	var req CreateIssueCommentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ParentCommentID != nil && !validUUID(*req.ParentCommentID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "parentCommentId must be a UUID")
		return
	}
	mentionTargets, ok := issueCommentTargetRequests(req.MentionTargets)
	mentionAgentIDs := req.MentionAgentIDs
	if len(mentionTargets) != 0 {
		mentionAgentIDs = nil
	}
	if !ok || len(mentionAgentIDs) > store.MaxIssueCommentMentions {
		writeError(w, http.StatusBadRequest, "invalid_argument", "mentionTargets must contain valid Agent or Squad UUIDs")
		return
	}
	for _, targetAgentID := range mentionAgentIDs {
		if !validUUID(targetAgentID) {
			writeError(w, http.StatusBadRequest, "invalid_argument", "mentionAgentIds must contain UUIDs")
			return
		}
	}
	if req.RequestID == "" {
		writeError(w, http.StatusBadRequest, "invalid_argument", "requestId is required")
		return
	}
	if !validUUID(req.RequestID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "requestId must be a UUID")
		return
	}
	actor, ok := a.requireIssueCommentActor(w, r)
	if !ok {
		return
	}
	value, err := a.projectAccess.CreateIssueComment(r.Context(), actor, app.CreateIssueCommentInput{
		ProjectID: projectID, IssueID: issueUUID, ParentCommentID: req.ParentCommentID, Body: req.Body,
		RequestKey: req.RequestID, MentionTargets: mentionTargets, MentionAgentIDs: mentionAgentIDs, SuppressImplicit: req.SuppressImplicitTrigger,
	})
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, issueCommentDTO(value, issueKey, actor.ID))
}

func (a *api) previewIssueCommentTriggers(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	var req PreviewIssueCommentTriggersRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ParentCommentID != nil && !validUUID(*req.ParentCommentID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "parentCommentId must be a UUID")
		return
	}
	mentionTargets, ok := issueCommentTargetRequests(req.MentionTargets)
	mentionAgentIDs := req.MentionAgentIDs
	if len(mentionTargets) != 0 {
		mentionAgentIDs = nil
	}
	if !ok || len(mentionAgentIDs) > store.MaxIssueCommentMentions {
		writeError(w, http.StatusBadRequest, "invalid_argument", "mentionTargets must contain valid Agent or Squad UUIDs")
		return
	}
	for _, targetAgentID := range mentionAgentIDs {
		if !validUUID(targetAgentID) {
			writeError(w, http.StatusBadRequest, "invalid_argument", "mentionAgentIds must contain UUIDs")
			return
		}
	}
	actor, ok := a.requireIssueCommentActor(w, r)
	if !ok {
		return
	}
	value, err := a.projectAccess.PreviewIssueCommentTriggers(
		r.Context(), actor, projectID, issueUUID, req.ParentCommentID, req.Body,
		store.IssueCommentTriggerRequest{
			MentionTargets: mentionTargets, MentionAgentIDs: mentionAgentIDs, SuppressImplicit: req.SuppressImplicitTrigger,
		},
	)
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := IssueCommentTriggerPreviewDTO{
		Mentions: make([]IssueCommentMentionPreviewDTO, 0, len(value.Mentions)),
	}
	for _, mention := range value.Mentions {
		out.Mentions = append(out.Mentions, IssueCommentMentionPreviewDTO{
			TargetType: mention.Target.Type, TargetID: mention.Target.ID, TargetName: mention.TargetName,
			ResolvedAgentID: nullableIssueCommentAgentID(mention.ResolvedAgentID), ResolvedAgentName: mention.ResolvedAgentName,
			TargetAgentID: mention.TargetAgentID, TargetAgentName: mention.TargetAgentName,
			Eligible: mention.Eligible, ReasonCode: mention.ReasonCode,
		})
	}
	if value.Implicit != nil {
		out.Implicit = &IssueCommentImplicitTriggerPreviewDTO{
			TargetType: value.Implicit.Target.Type, TargetID: value.Implicit.Target.ID, TargetName: value.Implicit.TargetName,
			ResolvedAgentID: nullableIssueCommentAgentID(value.Implicit.ResolvedAgentID), ResolvedAgentName: value.Implicit.ResolvedAgentName,
			TargetAgentID: value.Implicit.TargetAgentID, TargetAgentName: value.Implicit.TargetAgentName,
			RoutingReason: value.Implicit.RoutingReason, Eligible: value.Implicit.Eligible,
			Suppressed: value.Implicit.Suppressed, ReasonCode: value.Implicit.ReasonCode,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) previewIssueCommentMentions(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	var req PreviewIssueCommentMentionsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	mentionTargets, ok := issueCommentTargetRequests(req.MentionTargets)
	mentionAgentIDs := req.MentionAgentIDs
	if len(mentionTargets) != 0 {
		mentionAgentIDs = nil
	}
	if !ok || len(mentionAgentIDs) > store.MaxIssueCommentMentions {
		writeError(w, http.StatusBadRequest, "invalid_argument", "mentionTargets must contain valid Agent or Squad UUIDs")
		return
	}
	for _, targetAgentID := range mentionAgentIDs {
		if !validUUID(targetAgentID) {
			writeError(w, http.StatusBadRequest, "invalid_argument", "mentionAgentIds must contain UUIDs")
			return
		}
	}
	actor, ok := a.requireIssueCommentActor(w, r)
	if !ok {
		return
	}
	var values []store.IssueCommentMentionPreview
	var err error
	if len(mentionTargets) != 0 {
		values, err = a.projectAccess.PreviewIssueCommentTargets(r.Context(), actor, projectID, issueUUID, mentionTargets)
	} else {
		values, err = a.projectAccess.PreviewIssueCommentMentions(r.Context(), actor, projectID, issueUUID, mentionAgentIDs)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := make([]IssueCommentMentionPreviewDTO, 0, len(values))
	for _, value := range values {
		out = append(out, IssueCommentMentionPreviewDTO{
			TargetType: value.Target.Type, TargetID: value.Target.ID, TargetName: value.TargetName,
			ResolvedAgentID: nullableIssueCommentAgentID(value.ResolvedAgentID), ResolvedAgentName: value.ResolvedAgentName,
			TargetAgentID: value.TargetAgentID, TargetAgentName: value.TargetAgentName,
			Eligible: value.Eligible, ReasonCode: value.ReasonCode,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) updateIssueComment(w http.ResponseWriter, r *http.Request) {
	projectID, issueKey, issueUUID, commentID, ok := a.issueCommentPath(w, r)
	if !ok {
		return
	}
	var req UpdateIssueCommentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	actor, ok := a.requireIssueCommentActor(w, r)
	if !ok {
		return
	}
	value, err := a.projectAccess.UpdateIssueComment(r.Context(), actor, projectID, issueUUID, commentID, req.Body)
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, issueCommentDTO(value, issueKey, actor.ID))
}

func (a *api) deleteIssueComment(w http.ResponseWriter, r *http.Request) {
	projectID, _, issueUUID, commentID, ok := a.issueCommentPath(w, r)
	if !ok {
		return
	}
	actor, ok := a.requireIssueCommentActor(w, r)
	if !ok {
		return
	}
	if err := a.projectAccess.DeleteIssueComment(r.Context(), actor, projectID, issueUUID, commentID); err != nil {
		writeProjectAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) resolveIssueComment(w http.ResponseWriter, r *http.Request) {
	a.setIssueCommentResolution(w, r, true)
}

func (a *api) reopenIssueComment(w http.ResponseWriter, r *http.Request) {
	a.setIssueCommentResolution(w, r, false)
}

func (a *api) setIssueCommentResolution(w http.ResponseWriter, r *http.Request, resolved bool) {
	projectID, issueKey, issueUUID, commentID, ok := a.issueCommentPath(w, r)
	if !ok {
		return
	}
	actor, ok := a.requireIssueCommentActor(w, r)
	if !ok {
		return
	}
	var (
		value store.IssueComment
		err   error
	)
	if resolved {
		value, err = a.projectAccess.ResolveIssueComment(r.Context(), actor, projectID, issueUUID, commentID)
	} else {
		value, err = a.projectAccess.ReopenIssueComment(r.Context(), actor, projectID, issueUUID, commentID)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, issueCommentDTO(value, issueKey, actor.ID))
}

func (a *api) addIssueCommentReaction(w http.ResponseWriter, r *http.Request) {
	a.mutateIssueCommentReaction(w, r, true)
}

func (a *api) removeIssueCommentReaction(w http.ResponseWriter, r *http.Request) {
	a.mutateIssueCommentReaction(w, r, false)
}

func (a *api) mutateIssueCommentReaction(w http.ResponseWriter, r *http.Request, add bool) {
	projectID, _, issueUUID, commentID, ok := a.issueCommentPath(w, r)
	if !ok {
		return
	}
	reaction := chi.URLParam(r, "reaction")
	if !store.ValidIssueCommentReaction(reaction) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "unsupported comment reaction")
		return
	}
	actor, ok := a.requireIssueCommentActor(w, r)
	if !ok {
		return
	}
	var err error
	if add {
		err = a.projectAccess.AddIssueCommentReaction(r.Context(), actor, projectID, issueUUID, commentID, reaction)
	} else {
		err = a.projectAccess.RemoveIssueCommentReaction(r.Context(), actor, projectID, issueUUID, commentID, reaction)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listIssueTimeline(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueKey := chi.URLParam(r, "issueID")
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}

	var values []app.IssueTimelineEntry
	var err error
	viewerID := ""
	if a.projectAccess != nil {
		actor, ok := projectActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		viewerID = actor.ID
		values, err = a.projectAccess.ListIssueTimeline(r.Context(), actor, projectID, issueUUID)
	} else {
		values, err = a.service.ListIssueTimeline(r.Context(), projectID, issueUUID)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := make([]IssueTimelineEntryDTO, 0, len(values))
	for _, value := range values {
		out = append(out, issueTimelineEntryDTO(value, issueKey, viewerID))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) issueCommentPath(w http.ResponseWriter, r *http.Request) (projectID, issueKey, issueUUID, commentID string, ok bool) {
	projectID, ok = pathUUID(w, r, "projectID")
	if !ok {
		return "", "", "", "", false
	}
	issueKey = chi.URLParam(r, "issueID")
	issueUUID, ok = pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return "", "", "", "", false
	}
	commentID, ok = pathUUID(w, r, "commentID")
	if !ok {
		return "", "", "", "", false
	}
	return projectID, issueKey, issueUUID, commentID, true
}

func (a *api) requireIssueCommentActor(w http.ResponseWriter, r *http.Request) (app.AuthenticatedUser, bool) {
	if a.projectAccess == nil {
		writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
		return app.AuthenticatedUser{}, false
	}
	actor, ok := projectActor(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
		return app.AuthenticatedUser{}, false
	}
	return actor, true
}
