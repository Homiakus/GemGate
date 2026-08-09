package gateway

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"gemgate/internal/config"
)

type corsHandler struct {
	next             http.Handler
	enabled          bool
	wildcardOrigin   bool
	allowedOrigins   map[string]struct{}
	allowedMethods   map[string]struct{}
	allowedHeaders   map[string]struct{}
	methodsHeader    string
	headersHeader    string
	allowCredentials bool
	maxAgeSeconds    int
}

func newCORSHandler(next http.Handler, cfg config.CORSConfig, maxAge time.Duration) http.Handler {
	h := &corsHandler{
		next:             next,
		enabled:          cfg.IsEnabled(),
		allowedOrigins:   make(map[string]struct{}, len(cfg.AllowedOrigins)),
		allowedMethods:   make(map[string]struct{}, len(cfg.AllowedMethods)),
		allowedHeaders:   make(map[string]struct{}, len(cfg.AllowedHeaders)),
		allowCredentials: cfg.AllowCredentials,
	}
	methods := make([]string, 0, len(cfg.AllowedMethods))
	for _, method := range cfg.AllowedMethods {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method == "" {
			continue
		}
		h.allowedMethods[method] = struct{}{}
		methods = append(methods, method)
	}
	headers := make([]string, 0, len(cfg.AllowedHeaders))
	for _, header := range cfg.AllowedHeaders {
		header = http.CanonicalHeaderKey(strings.TrimSpace(header))
		if header == "" {
			continue
		}
		h.allowedHeaders[strings.ToLower(header)] = struct{}{}
		headers = append(headers, header)
	}
	for _, origin := range cfg.AllowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "*" {
			h.wildcardOrigin = true
			continue
		}
		if origin != "" {
			h.allowedOrigins[origin] = struct{}{}
		}
	}
	h.methodsHeader = strings.Join(methods, ", ")
	h.headersHeader = strings.Join(headers, ", ")
	if maxAge > 0 {
		h.maxAgeSeconds = int(maxAge.Round(time.Second).Seconds())
	}
	return h
}

func (h *corsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.enabled {
		h.next.ServeHTTP(w, r)
		return
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	preflight := r.Method == http.MethodOptions &&
		origin != "" &&
		strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")) != ""

	if origin != "" {
		if !h.allowOrigin(w, origin) {
			if preflight {
				http.Error(w, "CORS origin is not allowed", http.StatusForbidden)
				return
			}
		} else {
			h.writeCommonHeaders(w)
		}
	}

	if preflight {
		if !h.methodAllowed(r.Header.Get("Access-Control-Request-Method")) {
			http.Error(w, "CORS method is not allowed", http.StatusForbidden)
			return
		}
		if !h.headersAllowed(r.Header.Get("Access-Control-Request-Headers")) {
			http.Error(w, "CORS request header is not allowed", http.StatusForbidden)
			return
		}
		appendVary(w.Header(), "Access-Control-Request-Method")
		appendVary(w.Header(), "Access-Control-Request-Headers")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	h.next.ServeHTTP(w, r)
}

func (h *corsHandler) allowOrigin(w http.ResponseWriter, origin string) bool {
	if h.wildcardOrigin {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		return true
	}
	if _, ok := h.allowedOrigins[origin]; !ok {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	appendVary(w.Header(), "Origin")
	return true
}

func (h *corsHandler) writeCommonHeaders(w http.ResponseWriter) {
	if h.methodsHeader != "" {
		w.Header().Set("Access-Control-Allow-Methods", h.methodsHeader)
	}
	if h.headersHeader != "" {
		w.Header().Set("Access-Control-Allow-Headers", h.headersHeader)
	}
	if h.allowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	if h.maxAgeSeconds > 0 {
		w.Header().Set("Access-Control-Max-Age", strconv.Itoa(h.maxAgeSeconds))
	}
}

func (h *corsHandler) methodAllowed(method string) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	_, ok := h.allowedMethods[method]
	return ok
}

func (h *corsHandler) headersAllowed(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	for _, header := range strings.Split(value, ",") {
		header = strings.ToLower(strings.TrimSpace(header))
		if header == "" {
			continue
		}
		if _, ok := h.allowedHeaders[header]; !ok {
			return false
		}
	}
	return true
}

func appendVary(h http.Header, value string) {
	for _, existing := range h.Values("Vary") {
		for _, item := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(item), value) {
				return
			}
		}
	}
	h.Add("Vary", value)
}
