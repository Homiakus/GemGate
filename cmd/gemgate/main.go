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
	"gemgate/internal/tui"

	tea "charm.land/bubbletea/v2"
)

const version = "0.2.0"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	cmd := "run"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	// остаток аргументов после команды (безопасно, даже если их нет)
	var rest []string
	if len(os.Args) > 2 {
		rest = os.Args[2:]
	}

	switch cmd {
	case "run", "tui":
		must(run(true, rest))
	case "serve":
		must(run(false, rest))
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

	// Если файл конфига не найден — запускаем мастер первоначальной настройки
	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		if err2 := runSetupWizard(*configPath); err2 != nil {
			return err2
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
	go func() {
		serverErr <- gw.ListenAndServe()
	}()

	if withTUI {
		p := tea.NewProgram(tui.New(gw))
		if _, err := p.Run(); err != nil && !errors.Is(err, tea.ErrInterrupted) {
			_ = shutdown(gw)
			return err
		}
		return shutdown(gw)
	}

	printConnectionInfo(gw.Addr(), "")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-stop:
		return shutdown(gw)
	case err := <-serverErr:
		return err
	}
}

// runSetupWizard запускает интерактивный TUI-мастер настройки и сохраняет config.yaml.
func runSetupWizard(configPath string) error {
	fmt.Printf("Config file %q not found. Starting first-time setup...\n", configPath)

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
	printConnectionInfo(result.Listen, result.GemgateToken)
	return nil
}

func printConnectionInfo(listen, token string) {
	baseURL := tui.LocalBaseURL(listen)
	fmt.Printf("gemgate listening on %s\n", listen)
	fmt.Printf("Connect URL: %s/v1beta/openai/\n", baseURL)
	fmt.Printf("Native Gemini URL: %s/v1beta/models/gemini-3.5-flash:generateContent\n", baseURL)
	if token != "" {
		fmt.Printf("Access token: Bearer %s\n", token)
	}
}

func shutdown(gw *gateway.Gateway) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return gw.Shutdown(ctx)
}

func usage() {
	fmt.Print(`GemGate — Go Gemini API gateway with Charm TUI

Usage:
  gemgate run   -config config.yaml   # server + TUI
  gemgate tui   -config config.yaml   # alias for run
  gemgate serve -config config.yaml   # headless server
  gemgate version

Environment example:
  export GEMINI_API_KEY="your-real-gemini-key"
  export GEMGATE_TOKEN="your-client-facing-token"

OpenAI-compatible client base URL:
  http://localhost:8080/v1beta/openai/

Native Gemini path example:
  http://localhost:8080/v1beta/models/gemini-3.5-flash:generateContent
`)
}

func must(err error) {
	if err == nil {
		return
	}
	log.Fatal(err)
}
