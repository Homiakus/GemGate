package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gemgate/internal/config"
	"gemgate/internal/gateway"
	"gemgate/internal/provider"
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
	gw, err := gateway.New(rt)
	if err != nil {
		return err
	}

	serverErr := make(chan error, 1)
	go func() { serverErr <- gw.ListenAndServe() }()

	reloadCtx, cancelReload := context.WithCancel(context.Background())
	defer cancelReload()
	if *reloadInterval > 0 {
		go watchConfig(reloadCtx, *configPath, *reloadInterval, gw)
	}

	if withTUI {
		p := tea.NewProgram(tui.New(gw))
		if _, err := p.Run(); err != nil && !errors.Is(err, tea.ErrInterrupted) {
			cancelReload()
			_ = shutdown(gw)
			return err
		}
		cancelReload()
		return shutdown(gw)
	}

	printRuntimeInfo(gw.ConfigSnapshot())
	if *reloadInterval > 0 {
		fmt.Printf("Hot reload: every %s (invalid revisions are rejected atomically)\n", reloadInterval.String())
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-stop:
		cancelReload()
		return shutdown(gw)
	case err := <-serverErr:
		cancelReload()
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

func shutdown(gw *gateway.Gateway) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return gw.Shutdown(ctx)
}

func usage() {
	fmt.Print(`GemGate — multi-provider AI API gateway with Charm TUI

Usage:
  gemgate run       -config config.yaml [-reload-interval 5s]  # server + TUI
  gemgate tui       -config config.yaml [-reload-interval 5s]  # alias for run
  gemgate serve     -config config.yaml [-reload-interval 5s]  # headless server
  gemgate providers                                           # list built-in provider types
  gemgate version

Hot reload:
  Provider/client/CORS/trusted-proxy policy is validated and swapped atomically.
  Listener timeouts and rate-limit backend/Redis connection settings require restart.
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
