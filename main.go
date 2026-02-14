package main

import (
	"fmt"
	"log"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/googlesky/sstop/internal/collector"
	"github.com/googlesky/sstop/internal/platform"
	"github.com/googlesky/sstop/internal/ui"
)

func main() {
	// Redirect log output to a file so it doesn't interfere with TUI
	logFile, err := os.CreateTemp("", "sstop-*.log")
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	p, err := platform.NewPlatform()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init platform: %v\n", err)
		os.Exit(1)
	}
	defer p.Close()

	// Smart detect the main outbound interface
	defaultIface := platform.DetectDefaultInterface()

	c := collector.New(p, 1*time.Second)
	snapCh := c.Start()
	defer c.Stop()

	model := ui.New(snapCh)
	model.SetDefaultInterface(defaultIface)
	model.SetCollector(c)

	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
