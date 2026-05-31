package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/spf13/cobra"

	imapwatch "github.com/venky/mailtriaged/internal/imap"
	"github.com/venky/mailtriaged/internal/config"
	"github.com/venky/mailtriaged/internal/rules"
	"github.com/venky/mailtriaged/internal/store"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the mail triage daemon",
	RunE:  runDaemon,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

func runDaemon(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configPath())
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := cfg.ResolveSecrets(); err != nil {
		return fmt.Errorf("resolving secrets: %w", err)
	}

	if cfg.IMAP.Password == "" {
		return fmt.Errorf("IMAP password is empty after secret resolution")
	}

	rulesDir := filepath.Join(filepath.Dir(configPath()), "rules")

	ruleList, err := rules.LoadDir(rulesDir)
	if err != nil {
		return fmt.Errorf("loading rules: %w", err)
	}
	if err := rules.Validate(ruleList); err != nil {
		return fmt.Errorf("validating rules: %w", err)
	}
	log.Printf("loaded %d rules from %s", len(ruleList), rulesDir)

	dbPath := stateDBPath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()
	log.Printf("database: %s", dbPath)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	var wg sync.WaitGroup
	for _, folder := range cfg.IMAP.Folders {
		wg.Add(1)
		go func(folder string) {
			defer wg.Done()
			w := imapwatch.NewWatcher(cfg, folder, rulesDir, db)
			if err := w.Run(ctx); err != nil && ctx.Err() == nil {
				log.Printf("[%s] watcher exited with error: %v", folder, err)
			}
		}(folder)
	}

	log.Printf("watching %d folders on %s", len(cfg.IMAP.Folders), cfg.IMAP.Host)

	<-ctx.Done()
	log.Println("shutting down...")

	wg.Wait()
	log.Println("stopped")
	return nil
}

func stateDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "mailtriaged", "mailtriaged.db")
}
