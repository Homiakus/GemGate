package gateway

func providerHealthSnapshot(providers []ProviderMetricsSnapshot) map[string]string {
	out := make(map[string]string, len(providers))
	for _, p := range providers {
		out[p.Name] = p.Health
	}
	return out
}
