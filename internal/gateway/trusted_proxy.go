package gateway

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"gemgate/internal/config"
)

func resolveClientIP(rt config.Runtime, r *http.Request) string {
	peer, ok := parseRemoteAddr(r.RemoteAddr)
	if !ok {
		return ""
	}
	if !trustedProxy(rt.TrustedProxies, peer) {
		return peer.String()
	}

	if forwarded, valid := parseForwardedChain(r.Header.Values("X-Forwarded-For")); valid && len(forwarded) > 0 {
		chain := append(forwarded, peer)
		idx := len(chain) - 1
		for idx > 0 && trustedProxy(rt.TrustedProxies, chain[idx]) {
			idx--
		}
		return chain[idx].String()
	}

	if raw := strings.TrimSpace(r.Header.Get("X-Real-IP")); raw != "" {
		if addr, err := netip.ParseAddr(raw); err == nil {
			return addr.Unmap().String()
		}
	}
	return peer.String()
}

func parseRemoteAddr(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Addr{}, false
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func parseForwardedChain(values []string) ([]netip.Addr, bool) {
	if len(values) == 0 {
		return nil, true
	}
	var out []netip.Addr
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				return nil, false
			}
			addr, err := netip.ParseAddr(raw)
			if err != nil {
				return nil, false
			}
			out = append(out, addr.Unmap())
		}
	}
	return out, true
}

func trustedProxy(prefixes []netip.Prefix, addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
