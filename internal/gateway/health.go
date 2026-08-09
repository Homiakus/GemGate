package gateway

import (
	"net/http"
	"time"
)

func (g *Gateway) writeHealth(state runtimeSnapshot, w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"service":   "gemgate",
		"uptime":    time.Since(g.started).String(),
		"providers": providerHealthSnapshot(activeProviderMetrics(g.Metrics().Providers, state.providers)),
	})
}

func providerHealthSnapshot(providers []ProviderMetricsSnapshot) map[string]string {
	out := make(map[string]string, len(providers))
	for _, p := range providers {
		out[p.Name] = p.Health
	}
	return out
}
