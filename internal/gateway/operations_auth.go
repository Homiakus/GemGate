package gateway

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func operationsAuthorized(state runtimeSnapshot, r *http.Request) bool {
	if state.operationsToken == "" {
		_, _, ok := authenticate(state, r)
		return ok
	}
	token := bearerToken(r)
	if token == "" || len(token) != len(state.operationsToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(state.operationsToken)) == 1
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func operationsAuthChallenge() string {
	return "Bearer realm=" + string(rune(34)) + "gemgate-operations" + string(rune(34))
}

func isOperationalPath(path string) bool {
	switch path {
	case "/_healthz", "/_readyz", "/_metrics", "/_config":
		return true
	default:
		return false
	}
}
