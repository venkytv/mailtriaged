package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/venky/mailtriaged/internal/store"
)

var statsDays int

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show classifier and rule statistics",
	RunE:  runStats,
}

func init() {
	statsCmd.Flags().IntVar(&statsDays, "days", 7, "number of days to look back")
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, args []string) error {
	db, err := store.Open(stateDBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	since := time.Now().UTC().AddDate(0, 0, -statsDays).Format(time.RFC3339)
	stats, err := db.GetClassifierStats(since)
	if err != nil {
		return fmt.Errorf("querying stats: %w", err)
	}

	fmt.Printf("Classifier stats (last %d days)\n", statsDays)
	fmt.Printf("─────────────────────────────────\n")
	fmt.Printf("Total calls:        %d\n", stats.TotalCalls)
	fmt.Printf("Avg latency:        %.0f ms\n", stats.AvgDurationMs)
	fmt.Printf("Avg confidence:     %.2f\n", stats.AvgConfidence)
	fmt.Printf("Escalated to fallback: %d", stats.EscalatedCount)
	if stats.TotalCalls > 0 {
		fmt.Printf(" (%.0f%%)", float64(stats.EscalatedCount)/float64(stats.TotalCalls)*100)
	}
	fmt.Println()

	if len(stats.ByModel) > 0 {
		fmt.Printf("\nBy model:\n")
		for model, count := range stats.ByModel {
			fmt.Printf("  %-30s %d\n", model, count)
		}
	}

	if len(stats.ActionBreakdown) > 0 {
		fmt.Printf("\nBy action:\n")
		for action, count := range stats.ActionBreakdown {
			fmt.Printf("  %-20s %d\n", action, count)
		}
	}

	return nil
}
