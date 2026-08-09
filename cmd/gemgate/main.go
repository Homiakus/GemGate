package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gemgate/internal/config"
	"gemgate/internal/gateway"
	"gemgate/internal/provider"
	"gemgate/internal/telemetry"
	"gemgate/internal/tui"

	tea "charm.land/bubbletea/v2"
)

// version is overridden for tagged builds with -ldflags "-X main.version=<version>".
var version = "0.4.0-dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	cmd := "run"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	var rest []string
	if len(os.Args) > 2 {
		rest = os.Args[2:]
	}

	switch cmd {
	case "run", "tui":
		must(run(true, rest))
	case "serve":
		must(run(false, rest))
	case "providers":
		printProviders()
	case "version", "--version", "-v":
		fmt.Println("gemgate", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func run(withTUI bool, args []string) error {
	fs := flag.NewFlagSet("gemgate", flag.ContinueOnError)
	configPath := fs.String("config", "config.yaml", "path to YAML config")
	reloadInterval := fs.Duration("reload-interval", 5*time.Second, "config/secret reload interval; 0 disables hot reload")
	operationsListen := fs.String("operations-listen", "", "optional dedicated listen address for /_healthz, /_readyz, /_metrics and /_config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *reloadInterval < 0 {
		return fmt.Errorf("reload-interval must not be negative")
	}

	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		if err := runSetupWizard(*configPath); err != nil {
			return err
		}
	}

	rt, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	shutdownTelemetry, err := telemetry.Setup(context.Background(), rt.Config.Telemetry, version)
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(ctx); err != nil {
			log.Printf("telemetry shutdown: %v", err)
		}
	}()

	gw, err := gateway.New(rt)
	if err != nil {
		return err
	}

	// Reserve the application port before any UI or HTTP goroutine starts. This
	// makes bind failures synchronous and lets us reserve the operations port too
	// before exposing either trust domain.
	applicationListener, err := net.Listen("tcp", gw.Addr())
	if err != nil {
		_ = gw.Shutdown(context.Background())
		return fmt.Errorf("listen application endpoint %q: %w", gw.Addr(), err)
	}

	var operationsServer *http.Server
	var operationsListener net.Listener
	operationsAddr := strings.TrimSpace(*operationsListen)
	if operationsAddr != "" {
		if operationsAddr == strings.TrimSpace(rt.Config.Server.Listen) {
			_ = applicationListener.Close()
			_ = gw.Shutdown(context.Background())
			return fmt.Errorf("operations-listen must differ from server.listen")
		}
		operationsListener, err = net.Listen("tcp", operationsAddr)
		if err != nil {
			_ = applicationListener.Close()
			_ = gw.Shutdown(context.Background())
			return fmt.Errorf("listen operations endpoint %q: %w", operationsAddr, err)
		}
		gw.IsolateOperationsEndpoints()
		operationsServer = &http.Server{
			Addr:              operationsAddr,
			Handler:           gw.OperationsHandler(),
			ReadTimeout:       rt.ReadTimeout,
			ReadHeaderTimeout: minPositiveDuration(10*time.Second, rt.ReadTimeout),
			WriteTimeout:      rt.WriteTimeout,
			IdleTimeout:       rt.IdleTimeout,
			MaxHeaderBytes:    1 << 20,
		}
	}

	// Both listeners are now bound and handler composition is immutable for the
	// serving lifetime. Only now do requests start being accepted.
	serverErr := make(chan error, 2)
	go func() { serverErr <- gw.Serve(applicationListener) }()
	if operationsServer != nil {
		go func() {
			log.Printf("operations listener on %s", operationsListener.Addr().String())
			err := operationsServer.Serve(operationsListener)
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			serverErr <- err
		}()
	}

	reloadCtx, cancelReload := context.WithCancel(context.Background())
	defer cancelReload()
	if *reloadInterval > 0 {
		go watchConfig(reloadCtx, *configPath, *reloadInterval, gw)
	}

	if withTUI {
		p := tea.NewProgram(tui.New(gw))
		if _, err := p.Run(); err != nil && !errors.Is(err, tea.ErrInterrupted) {
			cancelReload()
			_ = shutdown(gw, operationsServer)
			return err
		}
		cancelReload()
		return shutdown(gw, operationsServer)
	}

	printRuntimeInfo(gw.ConfigSnapshot())
	if operationsServer != nil {
		fmt.Printf("Operations listener: %s (operational paths removed from application port)\n", operationsListener.Addr().String())
	}
	if *reloadInterval > 0 {
		fmt.Printf("Hot reload: every %s (invalid revisions are rejected atomically)\n", reloadInterval.String())
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-stop:
		cancelReload()
		return shutdown(gw, operationsServer)
	case err := <-serverErr:
		cancelReload()
		_ = shutdown(gw, operationsServer)
		return err
	}
}

func watchConfig(ctx context.Context, path string, interval time.Duration, gw *gateway.Gateway) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rt, err := config.Load(path)
			if err != nil {
				gw.RecordReloadFailure(err)
				log.Printf("config reload: %v", err)
				continue
			}
			result, err := gw.Reload(rt)
			if err != nil {
				log.Printf("config reload: %v", err)
				continue
			}
			if result.Changed {
				log.Printf("config reload applied: providers=%d clients=%d", result.Providers, result.Clients)
			}
		}
	}
}

func runSetupWizard(configPath string) error {
	fmt.Printf("Config file %q not found. Starting first-time Gemini setup...\n", configPath)
	result, err := tui.RunSetup(configPath)
	if err != nil {
		return fmt.Errorf("setup wizard error: %w", err)
	}
	if result.Cancelled {
		return fmt.Errorf("setup cancelled; create %s manually and re-run", configPath)
	}
	if err := tui.WriteConfig(result); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Printf("Config saved to %s\n", configPath)
	baseURL := tui.LocalBaseURL(result.Listen)
	fmt.Printf("GemGate URL: %s\n", baseURL)
	fmt.Printf("Default Gemini OpenAI-compatible URL: %s/v1beta/openai/\n", baseURL)
	fmt.Printf("Access token: Bearer %s\n", result.GemgateToken)
	fmt.Println("Add more providers in config.yaml under providers:; see `gemgate providers`.")
	return nil
}

func printRuntimeInfo(cfg gateway.ConfigSnapshot) {
	baseURL := tui.LocalBaseURL(cfg.Listen)
	fmt.Printf("GemGate listening on %s\n", cfg.Listen)
	fmt.Printf("Default provider: %s (root requests proxy there)\n", cfg.DefaultProvider)
	for _, p := range cfg.Providers {
		fmt.Printf("Provider %-12s type=%-18s route=%s/providers/%s/\n", p.Name, p.Type, baseURL, p.Name)
	}
}

func printProviders() {
	fmt.Println("Supported provider types:")
	for _, spec := range provider.Supported() {
		base := spec.DefaultBaseURL
		if base == "" {
			base = "<configure base_url>"
		}
		compat := ""
		if spec.OpenAICompatible {
			compat = " [OpenAI-compatible]"
		}
		fmt.Printf("  %-18s %-20s %s%s\n", spec.Type, spec.DisplayName, base, compat)
	}
}

func shutdown(gw *gateway.Gateway, operationsServer *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var operationsErr error
	if operationsServer != nil {
		operationsErr = operationsServer.Shutdown(ctx)
	}
	gatewayErr := gw.Shutdown(ctx)
	if gatewayErr != nil {
		return gatewayErr
	}
	return operationsErr
}

func minPositiveDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 || a < b {
		return a
	}
	return b
}

func usage() {
	fmt.Print(`GemGate — multi-provider AI API gateway with Charm TUI

Usage:
  gemgate run       -config config.yaml [-reload-interval 5s] [-operations-listen 127.0.0.1:9090]
  gemgate tui       -config config.yaml [-reload-interval 5s] [-operations-listen 127.0.0.1:9090]
  gemgate serve     -config config.yaml [-reload-interval 5s] [-operations-listen 127.0.0.1:9090]
  gemgate providers
  gemgate version

Operations isolation:
  -operations-listen <addr> starts a separate listener for /_healthz, /_readyz,
  /_metrics and /_config. Those paths then return 404 on the application listener.
  Application/provider routes always return 404 on the operations listener.
  Both ports are bound before either listener starts accepting requests.

Hot reload:
  Provider/client/CORS/trusted-proxy policy is validated and swapped atomically.
  Listener, telemetry and rate-limit backend/Redis connection settings require restart.
  Set -reload-interval 0 to disable polling.

Routing:
  /providers/<name>/<path>  -> named provider, prefix stripped
  /<path>                   -> default_provider (backward compatible)

Examples:
  http://localhost:8080/providers/openai/responses
  http://localhost:8080/providers/anthropic/v1/messages
  http://localhost:8080/providers/gemini/v1beta/openai/chat/completions

Clients always authenticate to GemGate with:
  Authorization: Bearer <GEMGATE_TOKEN>

GemGate removes that credential and injects the selected provider's own auth upstream.
`)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
