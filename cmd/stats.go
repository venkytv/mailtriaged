package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/venky/mailtriaged/internal/store"
)

var (
	statsDays    int
	statsVerbose bool
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show classifier and rule statistics",
	RunE:  runStats,
}

func init() {
	statsCmd.Flags().IntVar(&statsDays, "days", 7, "number of days to look back")
	statsCmd.Flags().BoolVarP(&statsVerbose, "verbose", "v", false, "show debug details (pending summary queue, etc.)")
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, args []string) error {
	db, err := store.Open(stateDBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	since := time.Now().UTC().AddDate(0, 0, -statsDays).Format(time.RFC3339)

	totalMsgs, err := db.GetTotalMessages(since)
	if err != nil {
		return fmt.Errorf("querying total messages: %w", err)
	}

	cStats, err := db.GetClassifierStats(since)
	if err != nil {
		return fmt.Errorf("querying classifier stats: %w", err)
	}

	rStats, err := db.GetRuleStats(since)
	if err != nil {
		return fmt.Errorf("querying rule stats: %w", err)
	}

	actions, err := db.GetActionBreakdown(since)
	if err != nil {
		return fmt.Errorf("querying action breakdown: %w", err)
	}

	fmt.Printf("Triage stats (last %d days)\n", statsDays)
	fmt.Printf("─────────────────────────────────\n")
	fmt.Printf("Total messages:       %d\n", totalMsgs)
	if totalMsgs > 0 {
		fmt.Printf("  Matched by rules:   %-4d (%.0f%%)\n", rStats.TotalHits, pct(rStats.TotalHits, totalMsgs))
		fmt.Printf("  Sent to classifier: %-4d (%.0f%%)\n", cStats.DistinctMessages, pct(cStats.DistinctMessages, totalMsgs))
	}

	if cStats.TotalCalls > 0 {
		fmt.Printf("\nClassifier (%d calls)\n", cStats.TotalCalls)
		fmt.Printf("  Avg latency:        %.0f ms\n", cStats.AvgDurationMs)
		fmt.Printf("  Avg confidence:     %.2f\n", cStats.AvgConfidence)
		fmt.Printf("  Escalated:          %d (%.0f%%)\n", cStats.EscalatedCount, pct(cStats.EscalatedCount, cStats.TotalCalls))

		if len(cStats.ByModel) > 0 {
			fmt.Printf("  By model:\n")
			for model, count := range cStats.ByModel {
				fmt.Printf("    %-20s %d\n", model, count)
			}
		}
	}

	if len(actions) > 0 {
		fmt.Printf("\nBy action:\n")
		for _, a := range actions {
			total := a.RuleCount + a.ClassifierCount
			var sources []string
			if a.RuleCount > 0 {
				sources = append(sources, fmt.Sprintf("rules: %d", a.RuleCount))
			}
			if a.ClassifierCount > 0 {
				sources = append(sources, fmt.Sprintf("classifier: %d", a.ClassifierCount))
			}
			if len(sources) == 1 {
				fmt.Printf("  %-20s %d\n", a.Action, total)
			} else {
				fmt.Printf("  %-20s %-4d (%s)\n", a.Action, total, strings.Join(sources, ", "))
			}
		}
	}

	if len(rStats.ByRule) > 0 {
		fmt.Printf("\nTop rules:\n")
		for _, r := range rStats.ByRule {
			fmt.Printf("  %-20s %d\n", r.RuleID, r.Count)
		}
	}

	if statsVerbose {
		if err := printVerboseDetails(db); err != nil {
			return err
		}
	}

	return nil
}

func printVerboseDetails(db *store.Store) error {
	items, err := db.UnsentSummaryItems()
	if err != nil {
		return fmt.Errorf("querying pending summary: %w", err)
	}

	fmt.Printf("\nPending daily summary (%d unsent):\n", len(items))
	if len(items) == 0 {
		fmt.Printf("  (none)\n")
	} else {
		fmt.Printf("  %-12s %-30s %-4s %s\n", "Date", "From", "Via", "Subject")
		for _, item := range items {
			date := item.CreatedAt
			if t, err := time.Parse(time.RFC3339, item.CreatedAt); err == nil {
				date = t.Format("Jan 02 15:04")
			}
			via := "?"
			switch item.Source {
			case "rule":
				via = "rule"
			case "classifier":
				via = "llm"
			}
			fmt.Printf("  %-12s %-30s %-4s %s\n", date, truncate(item.FromEmail, 30), via, truncate(item.Subject, 55))
		}
	}

	return nil
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
