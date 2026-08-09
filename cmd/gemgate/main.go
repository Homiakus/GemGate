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

const version = "0.3.0"

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
	if err := fs.Parse(args); err != nil {
		return err
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

	if withTUI {
		p := tea.NewProgram(tui.New(gw))
		if _, err := p.Run(); err != nil && !errors.Is(err, tea.ErrInterrupted) {
			_ = shutdown(gw)
			return err
		}
		return shutdown(gw)
	}

	printRuntimeInfo(gw.ConfigSnapshot())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-stop:
		return shutdown(gw)
	case err := <-serverErr:
		return err
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
  gemgate run       -config config.yaml   # server + TUI
  gemgate tui       -config config.yaml   # alias for run
  gemgate serve     -config config.yaml   # headless server
  gemgate providers                       # list built-in provider types
  gemgate version

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
