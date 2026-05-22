package config

import "testing"

func TestValidateRejectsNegativeClientRateLimit(t *testing.T) {
	rt := Runtime{
		Config: Config{
			Upstream: UpstreamConfig{APIKey: "gemini-key"},
			Clients: []ClientConfig{{
				Name:         "local-dev",
				Token:        "client-token",
				Enabled:      true,
				RateLimitRPM: -1,
			}},
		},
	}

	if err := validate(rt); err == nil {
		t.Fatal("expected negative rate_limit_rpm to be rejected")
	}
}
