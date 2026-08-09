package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthRemainsLocalWhenPrivate(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.WriteHeader(http.StatusTeapot)
	}))
	defer upstream.Close()

	gw, err := New(reloadRuntime(upstream.URL, "key", "token"))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_healthz", nil)
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated health status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/_healthz", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()
	gw.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated health status = %d", rec.Code)
	}
	if upstreamCalls != 0 {
		t.Fatalf("health endpoint leaked upstream: calls=%d", upstreamCalls)
	}
}
