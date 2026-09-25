package httpapi

import (
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

func (a *api) registerNotificationRoutes(r chi.Router) {
	r.Get("/notifications", a.listNotifications)
	r.Patch("/notifications/{notificationID}", a.setNotificationRead)
	r.Post("/notifications/read-all", a.markAllNotificationsRead)
}

func (a *api) listNotifications(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authActor(w, r)
	if !ok {
		return
	}
	page, err := a.service.ListNotifications(r.Context(), actor.ID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := UserNotificationsDTO{
		Notifications: make([]UserNotificationDTO, 0, len(page.Notifications)),
		UnreadCount:   page.UnreadCount,
	}
	for _, value := range page.Notifications {
		out.Notifications = append(out.Notifications, userNotificationDTO(value))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) setNotificationRead(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authActor(w, r)
	if !ok {
		return
	}
	notificationID, ok := pathUUID(w, r, "notificationID")
	if !ok {
		return
	}
	var request NotificationReadRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.Read == nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "read is required")
		return
	}
	if err := a.service.SetNotificationRead(r.Context(), actor.ID, notificationID, *request.Read); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) markAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authActor(w, r)
	if !ok {
		return
	}
	updated, err := a.service.MarkAllNotificationsRead(r.Context(), actor.ID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, NotificationReadAllDTO{Updated: updated})
}

func userNotificationDTO(value store.UserNotification) UserNotificationDTO {
	return UserNotificationDTO{
		ID: value.ID, Kind: value.Kind, ProjectID: value.ProjectID, ProjectName: value.ProjectName,
		IssueID: value.IssueID, IssueKey: value.IssueKey, IssueTitle: value.IssueTitle,
		SourceCommentID: value.SourceCommentID, CommentAuthorType: value.CommentAuthorType,
		CommentAuthorID: value.CommentAuthorID, CommentAuthorName: value.CommentAuthorName,
		Preview: value.Preview, CreatedAt: value.CreatedAt, ReadAt: value.ReadAt,
	}
}

func (a *api) getIssueSubscription(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	actor, ok := projectActor(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
		return
	}
	subscribed, err := a.projectAccess.GetIssueSubscription(r.Context(), actor, projectID, issueUUID)
	if err != nil {
		writeProjectAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, IssueSubscriptionDTO{Subscribed: subscribed})
}

func (a *api) setIssueSubscription(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	issueUUID, ok := pathIssueKey(w, r, projectID, a.service.ResolveIssueUUID)
	if !ok {
		return
	}
	actor, ok := projectActor(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_failed", "authentication failed")
		return
	}
	if err := a.projectAccess.SetIssueSubscription(r.Context(), actor, projectID, issueUUID, r.Method == http.MethodPut); err != nil {
		writeProjectAccessError(w, err)
		return
	}
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, IssueSubscriptionDTO{Subscribed: true})
}
