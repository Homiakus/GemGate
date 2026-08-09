package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"host":                {},
	"content-length":      {},
}

var upstreamCredentialHeaders = map[string]struct{}{
	"authorization":       {},
	"x-goog-api-key":      {},
	"x-goog-user-project": {},
	"x-api-key":           {},
	"api-key":             {},
	"anthropic-version":   {},
}

var forwardingHeaders = map[string]struct{}{
	"forwarded":         {},
	"x-forwarded-for":   {},
	"x-forwarded-host":  {},
	"x-forwarded-port":  {},
	"x-forwarded-proto": {},
	"x-real-ip":         {},
}

func copyRequestHeaders(dst, src http.Header) {
	connectionHeaders := connectionHeaderNames(src)
	for k, values := range src {
		lk := strings.ToLower(k)
		if _, skip := hopByHopHeaders[lk]; skip {
			continue
		}
		if _, skip := connectionHeaders[lk]; skip {
			continue
		}
		if _, secret := upstreamCredentialHeaders[lk]; secret {
			continue
		}
		if _, forwarding := forwardingHeaders[lk]; forwarding {
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	connectionHeaders := connectionHeaderNames(src)
	for k, values := range src {
		lk := strings.ToLower(k)
		if _, skip := hopByHopHeaders[lk]; skip {
			continue
		}
		if _, skip := connectionHeaders[lk]; skip {
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func connectionHeaderNames(h http.Header) map[string]struct{} {
	out := make(map[string]struct{})
	for _, value := range h.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
				out[name] = struct{}{}
			}
	}
	return out
}

func copyAndFlush(w http.ResponseWriter, r io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	var written int64
	flusher, _ := w.(http.Flusher)
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			wn, writeErr := w.Write(buf[:n])
			written += int64(wn)
			if flusher != nil {
				flusher.Flush()
			}
			if writeErr != nil {
				return written, writeErr
			}
			if wn != n {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func requestID(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get("X-Request-ID")); validRequestID(id) {
		return id
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return "req-" + hex.EncodeToString(b[:])
}

func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}

func publicProxyError(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid proxy request"
	case http.StatusNotFound:
		return "provider not found"
	case http.StatusRequestEntityTooLarge:
		return "request body too large"
	case http.StatusInternalServerError:
		return "gateway configuration error"
	default:
		return "upstream provider request failed"
	}
}

func levelForStatus(status int) string {
	switch {
	case status >= 500:
		return "error"
	case status >= 400:
		return "warn"
	default:
		return "info"
	}
}

func redact(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "…" + s[len(s)-4:]
}

func minDuration(values ...time.Duration) time.Duration {
	var out time.Duration
	for _, v := range values {
		if v <= 0 {
			continue
		}
		if out == 0 || v < out {
			out = v
		}
	}
	return out
}
