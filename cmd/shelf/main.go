package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/irfansharif/shelf/pkg/config"
	"github.com/irfansharif/shelf/pkg/storage"
	"github.com/irfansharif/shelf/pkg/tui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if cfg.Endpoint == "" {
		fmt.Fprintf(os.Stderr, "error: endpoint not configured in %s\n", config.Path())
		os.Exit(1)
	}

	store, err := storage.New(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing storage: %v\n", err)
		os.Exit(1)
	}

	model := tui.New(store, cfg.Endpoint)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}
}
