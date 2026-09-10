package app

import "strings"

func cloneURLHasHTTPUserInfo(raw string) bool {
	value := strings.TrimSpace(raw)
	lower := strings.ToLower(value)

	var authority string
	switch {
	case strings.HasPrefix(lower, "http://"):
		authority = value[len("http://"):]
	case strings.HasPrefix(lower, "https://"):
		authority = value[len("https://"):]
	default:
		return false
	}
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	return strings.Contains(authority, "@")
}
