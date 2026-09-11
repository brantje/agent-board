package app

import "strings"

func cloneURLHasEmbeddedCredentials(cloneURL string) bool {
	schemeEnd := strings.Index(cloneURL, "://")
	if schemeEnd <= 0 {
		return false
	}

	scheme := cloneURL[:schemeEnd]
	authority := cloneURL[schemeEnd+3:]
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return false
	}

	userinfo := authority[:at]
	return strings.Contains(userinfo, ":") || strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https")
}
