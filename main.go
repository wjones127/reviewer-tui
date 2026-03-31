package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

func initLog() (*os.File, error) {
	path := filepath.Join(configDir(), "debug.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	log.SetOutput(f)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	return f, nil
}

func main() {
	cfgPath := DefaultConfigPath()
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	logFile, err := initLog()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not open log file: %v\n", err)
	} else {
		defer logFile.Close()
	}
	log.Printf("starting reviewer-tui")

	db, err := OpenDB(DefaultDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	m := newModel(cfg, db)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
