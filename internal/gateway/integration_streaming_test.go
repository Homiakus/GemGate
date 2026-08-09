package gateway

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gemgate/internal/config"
)

func TestStreamingFlushesBeforeUpstreamCompletes(t *testing.T) {
	firstWritten := make(chan struct{})
	releaseSecond := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer does not support flush")
		}
		_, _ = w.Write([]byte("data: first\n\n"))
		flusher.Flush()
		close(firstWritten)
		<-releaseSecond
		_, _ = w.Write([]byte("data: second\n\n"))
		flusher.Flush()
	}))
	defer upstream.Close()

	gw, err := New(runtimeForTests([]config.ProviderConfig{{
		Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "key",
	}}, "openai"))
	if err != nil {
		t.Fatal(err)
	}

	recorder := newStreamingRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	req.Header.Set("Authorization", "Bearer client-token")
	done := make(chan struct{})
	go func() {
		gw.ServeHTTP(recorder, req)
		close(done)
	}()

	select {
	case <-firstWritten:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not write first event")
	}

	var firstFlush string
	select {
	case firstFlush = <-recorder.flushes:
	case <-time.After(2 * time.Second):
		t.Fatal("gateway did not flush first event downstream")
	}
	if !strings.Contains(firstFlush, "data: first") || strings.Contains(firstFlush, "data: second") {
		t.Fatalf("unexpected first flushed payload: %q", firstFlush)
	}
	metrics := gw.Metrics()
	if len(metrics.Providers) != 1 || metrics.Providers[0].InFlight != 1 {
		t.Fatalf("provider should remain in-flight during stream: %#v", metrics.Providers)
	}

	close(releaseSecond)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("gateway did not finish stream")
	}
	if body := recorder.Body(); !strings.Contains(body, "data: second") {
		t.Fatalf("final body missing second event: %q", body)
	}
	metrics = gw.Metrics()
	if metrics.Providers[0].InFlight != 0 || metrics.Providers[0].Requests != 1 || metrics.Providers[0].TotalDuration <= 0 {
		t.Fatalf("unexpected completed provider metrics: %#v", metrics.Providers[0])
	}
}

func TestDownstreamCancellationDoesNotTripProviderCircuit(t *testing.T) {
	started := make(chan struct{})
	upstreamCanceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(upstreamCanceled)
	}))
	defer upstream.Close()

	gw, err := New(runtimeForTests([]config.ProviderConfig{{
		Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "key",
	}}, "openai"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/responses", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer client-token")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		gw.ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request did not start")
	}
	cancel()
	select {
	case <-upstreamCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("downstream cancellation did not reach upstream")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("gateway did not finish canceled request")
	}

	metrics := gw.Metrics()
	if metrics.UpstreamErrors != 0 {
		t.Fatalf("client cancellation counted as upstream error: %d", metrics.UpstreamErrors)
	}
	if len(metrics.Providers) != 1 || metrics.Providers[0].TransportErrors != 0 || metrics.Providers[0].ConsecutiveFailures != 0 {
		t.Fatalf("client cancellation polluted provider failures: %#v", metrics.Providers)
	}
	if len(metrics.Circuits) != 1 || metrics.Circuits[0].State != string(circuitClosed) || metrics.Circuits[0].Failures != 0 {
		t.Fatalf("client cancellation changed circuit: %#v", metrics.Circuits)
	}
	logs := gw.Logs()
	if len(logs) == 0 || logs[len(logs)-1].Message != "client canceled request" || logs[len(logs)-1].Status != 0 {
		t.Fatalf("canceled request log = %#v", logs)
	}
}

func TestProviderTimeoutCountsAsProviderFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer upstream.Close()

	rt := runtimeForTests([]config.ProviderConfig{{
		Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "key",
	}}, "openai")
	rt.ProviderTimeouts["openai"] = 50 * time.Millisecond
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/responses", nil)
	req.Header.Set("Authorization", "Bearer client-token")
	rec := httptest.NewRecorder()
	start := time.Now()
	gw.ServeHTTP(rec, req)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("provider timeout took too long: %s", elapsed)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), upstream.URL) || strings.Contains(strings.ToLower(rec.Body.String()), "deadline") {
		t.Fatalf("provider timeout details leaked to client: %q", rec.Body.String())
	}

	metrics := gw.Metrics()
	if metrics.UpstreamErrors != 1 || len(metrics.Providers) != 1 || metrics.Providers[0].TransportErrors != 1 || metrics.Providers[0].ConsecutiveFailures != 1 {
		t.Fatalf("provider timeout metrics = global=%d providers=%#v", metrics.UpstreamErrors, metrics.Providers)
	}
	if len(metrics.Circuits) != 1 || metrics.Circuits[0].State != string(circuitClosed) || metrics.Circuits[0].Failures != 1 {
		t.Fatalf("provider timeout circuit = %#v", metrics.Circuits)
	}
}

type streamingRecorder struct {
	mu      sync.Mutex
	header  http.Header
	status  int
	body    bytes.Buffer
	flushes chan string
}

func newStreamingRecorder() *streamingRecorder {
	return &streamingRecorder{header: make(http.Header), flushes: make(chan string, 8)}
}

func (r *streamingRecorder) Header() http.Header { return r.header }

func (r *streamingRecorder) WriteHeader(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == 0 {
		r.status = status
	}
}

func (r *streamingRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(p)
}

func (r *streamingRecorder) Flush() {
	r.mu.Lock()
	snapshot := r.body.String()
	r.mu.Unlock()
	select {
	case r.flushes <- snapshot:
	default:
	}
}

func (r *streamingRecorder) Body() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.String()
}
