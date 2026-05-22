package tui

import "testing"

func TestAccessTokenAddsGemGatePrefix(t *testing.T) {
	got := AccessToken("client-token")
	if got != "gg-client-token" {
		t.Fatalf("AccessToken() = %q, want %q", got, "gg-client-token")
	}
}

func TestAccessTokenDoesNotDuplicatePrefix(t *testing.T) {
	got := AccessToken("gg-client-token")
	if got != "gg-client-token" {
		t.Fatalf("AccessToken() = %q, want %q", got, "gg-client-token")
	}
}

func TestLocalBaseURLUsesLocalhostForWildcardListen(t *testing.T) {
	got := LocalBaseURL("0.0.0.0:9090")
	if got != "http://localhost:9090" {
		t.Fatalf("LocalBaseURL() = %q, want %q", got, "http://localhost:9090")
	}
}
