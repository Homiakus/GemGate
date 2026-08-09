package gateway

import (
	"net/http"
	"time"
)

type readinessProvider struct {
	Health       string `json:"health"`
	Circuit      string `json:"circuit"`
	RetryAfterMS int64  `json:"retry_after_ms,omitempty"`
}

func (g *Gateway) writeReadiness(state runtimeSnapshot, w http.ResponseWriter) {
	metrics := g.metrics.Snapshot()
	healthByProvider := make(map[string]string, len(state.providers))
	for _, metric := range activeProviderMetrics(metrics.Providers, state.providers) {
		healthByProvider[metric.Name] = metric.Health
	}
	circuitByProvider := make(map[string]CircuitSnapshot, len(state.providers))
	for _, circuit := range circuitSnapshots(state, time.Now()) {
		circuitByProvider[circuit.Provider] = circuit
	}

	providers := make(map[string]readinessProvider, len(state.providers))
	ready := true
	for name := range state.providers {
		circuit := circuitByProvider[name]
		if circuit.State == "" {
			circuit.State = string(circuitClosed)
		}
		entry := readinessProvider{Health: healthByProvider[name], Circuit: circuit.State}
		if entry.Health == "" {
			entry.Health = "unknown"
		}
		if circuit.RetryAfter > 0 {
			entry.RetryAfterMS = circuit.RetryAfter.Round(time.Millisecond).Milliseconds()
		}
		providers[name] = entry
		if name == state.cfg.Config.DefaultProvider && (circuit.State == string(circuitOpen) || circuit.State == string(circuitHalfOpen)) {
			ready = false
		}
	}

	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("X-GemGate-Readiness", "passive")
	writeJSON(w, status, map[string]any{
		"ready":            ready,
		"service":          "gemgate",
		"mode":             "passive",
		"default_provider": state.cfg.Config.DefaultProvider,
		"providers":        providers,
		"checked_at":       time.Now().UTC().Format(time.RFC3339Nano),
	})
}
