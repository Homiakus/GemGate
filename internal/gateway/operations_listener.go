package gateway

import (
	"net/http"
	"time"
)

// IsolateOperationsEndpoints removes all operational paths from the application
// listener. Call it during process setup, before ListenAndServe starts.
func (g *Gateway) IsolateOperationsEndpoints() {
	application := g.server.Handler
	g.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isOperationalPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		application.ServeHTTP(w, r)
	})
}

// OperationsHandler returns a handler that terminates operational endpoints only.
// Application/provider paths are never proxied from this handler.
func (g *Gateway) OperationsHandler() http.Handler {
	return http.HandlerFunc(g.serveOperationsListenerHTTP)
}

func (g *Gateway) serveOperationsListenerHTTP(w http.ResponseWriter, r *http.Request) {
	r, span := startGatewaySpan(r)
	defer span.End()

	state := g.currentRuntime()
	start := time.Now()
	reqID := requestID(r)
	traceRequestID(r.Context(), reqID)
	clientIP := resolveClientIP(state.cfg, r)
	w.Header().Set("X-Request-ID", reqID)

	if !isOperationalPath(r.URL.Path) {
		traceHTTPStatus(r.Context(), http.StatusNotFound)
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/_healthz" && state.cfg.Config.Server.PublicHealth {
		traceAuth(r.Context(), "public_operations", "")
		g.writeHealth(state, w)
		return
	}
	if r.URL.Path == "/_readyz" && state.cfg.Config.Server.PublicHealth {
		traceAuth(r.Context(), "public_operations", "")
		g.writeReadiness(state, w)
		return
	}
	if r.Method == http.MethodOptions {
		traceHTTPStatus(r.Context(), http.StatusNoContent)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !operationsAuthorized(state, r) {
		traceAuth(r.Context(), "operations", "")
		traceHTTPStatus(r.Context(), http.StatusUnauthorized)
		g.metrics.AuthFailures.Add(1)
		recordStatus(g.metrics, http.StatusUnauthorized)
		g.logs.Add(LogEntry{
			Time: start, Level: "warn", Client: "operations", ClientIP: clientIP,
			Method: r.Method, Path: r.URL.Path, Status: http.StatusUnauthorized,
			Duration: time.Since(start), RequestID: reqID, Message: "operations auth failed",
		})
		w.Header().Set("WWW-Authenticate", operationsAuthChallenge())
		http.Error(w, "invalid operations token", http.StatusUnauthorized)
		return
	}

	traceAuth(r.Context(), "operations", "")
	switch r.URL.Path {
	case "/_healthz":
		g.writeHealth(state, w)
	case "/_readyz":
		g.writeReadiness(state, w)
	case "/_metrics":
		traceHTTPStatus(r.Context(), http.StatusOK)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(g.Metrics().Prometheus()))
	case "/_config":
		traceHTTPStatus(r.Context(), http.StatusOK)
		writeJSON(w, http.StatusOK, safeConfig(state))
	}
}
