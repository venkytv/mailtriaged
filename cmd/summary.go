package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/venky/mailtriaged/internal/config"
	"github.com/venky/mailtriaged/internal/notify"
	"github.com/venky/mailtriaged/internal/store"
	"github.com/venky/mailtriaged/internal/telegram"
)

var summaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Manage daily summary",
}

var summarySendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send the daily summary immediately",
	RunE:  runSummarySend,
}

func init() {
	summaryCmd.AddCommand(summarySendCmd)
	rootCmd.AddCommand(summaryCmd)
}

func runSummarySend(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configPath())
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := cfg.ResolveSecrets(); err != nil {
		return fmt.Errorf("resolving secrets: %w", err)
	}

	if !cfg.Notifications.Telegram.Enabled {
		return fmt.Errorf("telegram notifications are not enabled in config")
	}

	db, err := store.Open(stateDBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	tg := telegram.NewClient(cfg.Notifications.Telegram.BotToken, cfg.Notifications.Telegram.ChatID)

	sched, err := notify.NewSummaryScheduler(tg, db, cfg.Summary.SendTime, cfg.Summary.Timezone)
	if err != nil {
		return err
	}
	sched.SetSummarizer(buildSummarizer(cfg))

	return sched.SendNow()
}
