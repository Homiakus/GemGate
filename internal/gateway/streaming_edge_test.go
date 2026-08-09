package gateway

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gemgate/internal/config"
)

func TestUnknownLengthBodyLimitRejectsBeforeUpstream(t *testing.T) {
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "provider-key"}}, "openai")
	rt.Config.Server.RequestBodyLimit = "16B"
	rt.RequestBodyLimit = 16
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	body := io.NopCloser(strings.NewReader(strings.Repeat("x", 17)))
	req := httptest.NewRequest(http.MethodPost, "/responses", body)
	req.ContentLength = -1 // exercise streaming/chunked request body enforcement
	req.Header.Set("Authorization", "Bearer client-token")
	resp := httptest.NewRecorder()
	gw.ServeHTTP(resp, req)

	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if upstreamCalls.Load() != 0 {
		t.Fatalf("oversized request reached upstream %d times", upstreamCalls.Load())
	}
}

func TestLargeSSEFlushesBeforeUpstreamCompletes(t *testing.T) {
	releaseUpstream := make(chan struct{})
	upstreamStarted := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: "+strings.Repeat("x", 96*1024)+"\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(upstreamStarted)
		<-releaseUpstream
		_, _ = io.WriteString(w, "data: done\n\n")
	}))
	defer upstream.Close()

	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "provider-key"}}, "openai")
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(gw)
	defer proxy.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, proxy.URL+"/responses", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer client-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	select {
	case <-upstreamStarted:
	case <-ctx.Done():
		t.Fatal("upstream did not emit first SSE event")
	}

	firstChunk := make(chan []byte, 1)
	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 1024)
		n, err := io.ReadFull(resp.Body, buf)
		if err != nil {
			readErr <- err
			return
		}
		firstChunk <- buf[:n]
	}()

	select {
	case chunk := <-firstChunk:
		if !strings.HasPrefix(string(chunk), "data: ") {
			t.Fatalf("unexpected first streamed bytes %q", string(chunk[:min(len(chunk), 32)]))
		}
	case err := <-readErr:
		t.Fatalf("read first streamed bytes: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("gateway did not flush large SSE response before upstream completion")
	}

	close(releaseUpstream)
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
}

func TestUpstreamDisconnectAfterHeadersDoesNotRewriteResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "64")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "partial")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "provider-key"}}, "openai")
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer client-token")
	resp := httptest.NewRecorder()
	gw.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", resp.Code, resp.Body.String())
	}
	if resp.Body.String() != "partial" {
		t.Fatalf("body was rewritten after response start: %q", resp.Body.String())
	}
	if got := gw.Metrics().UpstreamErrors; got != 1 {
		t.Fatalf("upstream errors=%d, want 1", got)
	}
	metrics := gw.Metrics().Providers
	if len(metrics) != 1 {
		t.Fatalf("provider metrics=%#v", metrics)
	}
	if metrics[0].TransportErrors != 1 {
		t.Fatalf("provider transport errors=%d, want 1 after truncated response body", metrics[0].TransportErrors)
	}
}

func TestMalformedChunkedProviderResponseKeepsPartialStreamAndRecordsFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverErr := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverErr <- acceptErr
			return
		}
		defer conn.Close()
		req, readErr := http.ReadRequest(bufio.NewReader(conn))
		if readErr != nil {
			serverErr <- readErr
			return
		}
		_ = req.Body.Close()
		// One complete SSE chunk is sent, but the terminating zero-length chunk is
		// deliberately omitted. net/http must surface unexpected EOF to GemGate.
		_, writeErr := io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\nA\r\ndata: hi\n\n\r\n")
		serverErr <- writeErr
	}()

	rt := runtimeForTests([]config.ProviderConfig{{
		Name: "openai", Type: "openai", BaseURL: "http://" + listener.Addr().String(), APIKey: "provider-key",
	}}, "openai")
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer client-token")
	resp := httptest.NewRecorder()
	gw.ServeHTTP(resp, req)

	if err := <-serverErr; err != nil {
		t.Fatalf("raw upstream: %v", err)
	}
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", resp.Code, resp.Body.String())
	}
	if resp.Body.String() != "data: hi\n\n" {
		t.Fatalf("partial chunked stream changed: %q", resp.Body.String())
	}
	if got := gw.Metrics().UpstreamErrors; got != 1 {
		t.Fatalf("upstream errors=%d, want 1", got)
	}
	providers := gw.Metrics().Providers
	if len(providers) != 1 || providers[0].TransportErrors != 1 {
		t.Fatalf("provider transport accounting=%#v", providers)
	}
	if providers[0].Health == "healthy" {
		t.Fatalf("truncated chunked stream must not look healthy: %#v", providers[0])
	}
}
