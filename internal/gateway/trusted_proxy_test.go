package gateway

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"gemgate/internal/config"
)

func TestResolveClientIPIgnoresSpoofedForwardingFromUntrustedPeer(t *testing.T) {
	rt := config.Runtime{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	req := httptest.NewRequest(http.MethodGet, "http://gemgate.test/", nil)
	req.RemoteAddr = "198.51.100.25:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	req.Header.Set("X-Real-IP", "203.0.113.8")
	if got := resolveClientIP(rt, req); got != "198.51.100.25" {
		t.Fatalf("client ip = %q", got)
	}
}

func TestResolveClientIPWalksTrustedProxySuffixRightToLeft(t *testing.T) {
	rt := config.Runtime{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	req := httptest.NewRequest(http.MethodGet, "http://gemgate.test/", nil)
	req.RemoteAddr = "10.2.0.2:443"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.1.0.1")
	if got := resolveClientIP(rt, req); got != "203.0.113.7" {
		t.Fatalf("client ip = %q", got)
	}
}

func TestResolveClientIPStopsAtUntrustedIntermediateProxy(t *testing.T) {
	rt := config.Runtime{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	req := httptest.NewRequest(http.MethodGet, "http://gemgate.test/", nil)
	req.RemoteAddr = "10.2.0.2:443"
	req.Header.Set("X-Forwarded-For", "192.0.2.50, 198.51.100.8")
	if got := resolveClientIP(rt, req); got != "198.51.100.8" {
		t.Fatalf("client ip = %q", got)
	}
}

func TestGatewayRebuildsForwardingHeadersBeforeUpstream(t *testing.T) {
	var gotXFF, gotRealIP, gotForwarded string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotXFF = r.Header.Get("X-Forwarded-For")
		gotRealIP = r.Header.Get("X-Real-IP")
		gotForwarded = r.Header.Get("Forwarded")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	rt := reloadRuntime(upstream.URL, "key", "token")
	rt.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	req.RemoteAddr = "10.2.0.2:443"
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.1.0.1")
	req.Header.Set("X-Real-IP", "192.0.2.99")
	req.Header.Set("Forwarded", "for=192.0.2.123;proto=https")
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if gotXFF != "203.0.113.7" || gotRealIP != "203.0.113.7" {
		t.Fatalf("forwarding headers xff=%q real=%q", gotXFF, gotRealIP)
	}
	if gotForwarded != "" {
		t.Fatalf("spoofed Forwarded header leaked upstream: %q", gotForwarded)
	}

	logs := gw.Logs()
	if len(logs) == 0 || logs[len(logs)-1].ClientIP != "203.0.113.7" {
		t.Fatalf("trusted client ip not recorded in logs: %#v", logs)
	}
}
