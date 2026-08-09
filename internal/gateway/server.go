package gateway

import (
	"errors"
	"net"
	"net/http"
)

// Serve runs the application HTTP server on an already-bound listener. It is
// useful when process startup must reserve all application/control-plane ports
// before any server starts accepting requests.
func (g *Gateway) Serve(listener net.Listener) error {
	g.logs.Add(LogEntry{Level: "info", Message: "listening on " + listener.Addr().String(), Client: "system"})
	if err := g.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
