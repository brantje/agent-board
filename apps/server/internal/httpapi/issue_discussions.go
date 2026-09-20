package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

type IssueDiscussionRootDTO struct {
	Root            IssueCommentDTO   `json:"root"`
	ReplyCount      int               `json:"replyCount"`
	LastActivityAt  time.Time         `json:"lastActivityAt"`
	CompactComments []IssueCommentDTO `json:"compactComments"`
	Truncated       bool              `json:"truncated"`
}

type IssueDiscussionThreadDTO struct {
	RootID    string            `json:"rootId"`
	AnchorID  string            `json:"anchorId"`
	Comments  []IssueCommentDTO `json:"comments"`
	Truncated bool              `json:"truncated"`
}

type IssueDiscussionUpdateDTO struct {
	Comment IssueCommentDTO `json:"comment"`
	IsNew   bool            `json:"isNew"`
}

type IssueDiscussionUpdatesDTO struct {
	Comments   []IssueDiscussionUpdateDTO `json:"comments"`
	NextCursor *string                    `json:"nextCursor"`
	HasMore    bool                       `json:"hasMore"`
}

func (a *api) listRecentIssueDiscussions(w http.ResponseWriter, r *http.Request) {
	projectID, issueKey, issueUUID, ok := a.issueDiscussionPath(w, r)
	if !ok {
		return
	}
	limit, ok := issueDiscussionLimit(w, r)
	if !ok {
		return
	}
	viewerID := ""
	var values []store.IssueDiscussionRoot
	var err error
	if a.projectAccess != nil {
		actor, authenticated := projectActor(r)
		if !authenticated {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		viewerID = actor.ID
		values, err = a.projectAccess.ListRecentIssueDiscussions(r.Context(), actor, projectID, issueUUID, limit)
	} else {
		values, err = a.service.ListRecentIssueDiscussions(r.Context(), projectID, issueUUID, limit)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	out := make([]IssueDiscussionRootDTO, 0, len(values))
	for _, value := range values {
		compact := make([]IssueCommentDTO, 0, len(value.CompactComments))
		for _, comment := range value.CompactComments {
			compact = append(compact, issueCommentDTO(comment, issueKey, viewerID))
		}
		out = append(out, IssueDiscussionRootDTO{
			Root:            issueCommentDTO(value.Root, issueKey, viewerID),
			ReplyCount:      value.ReplyCount,
			LastActivityAt:  value.LastActivityAt,
			CompactComments: compact,
			Truncated:       value.Truncated,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) getIssueDiscussionThread(w http.ResponseWriter, r *http.Request) {
	projectID, issueKey, issueUUID, commentID, ok := a.issueCommentPath(w, r)
	if !ok {
		return
	}
	limit, ok := issueDiscussionLimit(w, r)
	if !ok {
		return
	}
	viewerID := ""
	var value store.IssueDiscussionThread
	var err error
	if a.projectAccess != nil {
		actor, authenticated := projectActor(r)
		if !authenticated {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		viewerID = actor.ID
		value, err = a.projectAccess.GetIssueDiscussionThread(r.Context(), actor, projectID, issueUUID, commentID, limit)
	} else {
		value, err = a.service.GetIssueDiscussionThread(r.Context(), projectID, issueUUID, commentID, limit)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	comments := make([]IssueCommentDTO, 0, len(value.Comments))
	for _, comment := range value.Comments {
		comments = append(comments, issueCommentDTO(comment, issueKey, viewerID))
	}
	writeJSON(w, http.StatusOK, IssueDiscussionThreadDTO{
		RootID: value.RootID, AnchorID: value.AnchorID, Comments: comments, Truncated: value.Truncated,
	})
}

func (a *api) listIssueDiscussionUpdates(w http.ResponseWriter, r *http.Request) {
	projectID, issueKey, issueUUID, ok := a.issueDiscussionPath(w, r)
	if !ok {
		return
	}
	limit, ok := issueDiscussionLimit(w, r)
	if !ok {
		return
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	viewerID := ""
	var value app.IssueDiscussionUpdatePage
	var err error
	if a.projectAccess != nil {
		actor, authenticated := projectActor(r)
		if !authenticated {
			writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
			return
		}
		viewerID = actor.ID
		value, err = a.projectAccess.ListIssueDiscussionUpdates(r.Context(), actor, projectID, issueUUID, cursor, limit)
	} else {
		value, err = a.service.ListIssueDiscussionUpdates(r.Context(), projectID, issueUUID, cursor, limit)
	}
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	comments := make([]IssueDiscussionUpdateDTO, 0, len(value.Comments))
	for _, item := range value.Comments {
		comments = append(comments, IssueDiscussionUpdateDTO{Comment: issueCommentDTO(item.Comment, issueKey, viewerID), IsNew: item.IsNew})
	}
	var nextCursor *string
	if value.NextCursor != "" {
		next := value.NextCursor
		nextCursor = &next
	}
	writeJSON(w, http.StatusOK, IssueDiscussionUpdatesDTO{Comments: comments, NextCursor: nextCursor, HasMore: value.HasMore})
}

func (a *api) issueDiscussionPath(w http.ResponseWriter, r *http.Request) (projectID, issueKey, issueUUID string, ok bool) {
	projectID, ok = pathUUID(w, r, "projectID")
	if !ok {
		return "", "", "", false
	}
	issueKey = chi.URLParam(r, "issueID")
	issueUUID, ok = pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return "", "", "", false
	}
	return projectID, issueKey, issueUUID, true
}

func issueDiscussionLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "limit must be a positive integer")
		return 0, false
	}
	return value, true
}
