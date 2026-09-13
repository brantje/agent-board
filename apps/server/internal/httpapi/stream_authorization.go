package httpapi

import "net/http"

// projectStreamAccessCurrent revalidates long-lived stream access against the
// same authoritative authentication and Project authorization services used by
// normal requests. Test/legacy routers without authentication keep their
// existing behavior.
func (a *api) projectStreamAccessCurrent(r *http.Request, projectID string) bool {
	if a.auth == nil || a.projectAccess == nil {
		return true
	}
	token, ok := bearerToken(r)
	if !ok {
		return false
	}
	actor, err := a.auth.AuthenticateNormalAccess(r.Context(), token)
	if err != nil {
		return false
	}
	return a.projectAccess.AuthorizeRead(r.Context(), actor, projectID) == nil
}
