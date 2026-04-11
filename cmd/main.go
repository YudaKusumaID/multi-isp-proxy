package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/ayanacorp/venn-combine-connection/internal/tui"
	"github.com/ayanacorp/venn-combine-connection/internal/sysproxy"
)

var version = "0.1.0"

func main() {
	// Parse flags
	addr := flag.String("addr", "127.0.0.1:1080", "Proxy listen address")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("venn-combine-connection v%s\n", version)
		os.Exit(0)
	}

	// Setup logging to file (TUI takes over stdout)
	logFile, err := os.OpenFile("venn.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not open log file: %v\n", err)
	} else {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	log.Printf("Starting Venn Combine Connection v%s", version)

	// Emergency proxy restore on panic
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC: %v — attempting proxy restore", r)
			sysproxy.Disable()
		}
	}()

	// Setup signal handler for emergency cleanup
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received interrupt signal — restoring proxy settings")
		sysproxy.Disable()
		os.Exit(0)
	}()

	// Create and run the TUI
	model := tui.NewModel(*addr)
	p := tea.NewProgram(model)

	if _, err := p.Run(); err != nil {
		log.Printf("Error running TUI: %v", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	log.Println("Venn Combine Connection exited cleanly")
}
