package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/venky/mailtriaged/internal/consolidate"
	"github.com/venky/mailtriaged/internal/rules"
)

var promoteAction string

var rulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "Manage rule files",
}

var rulesValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate rule files",
	RunE:  runRulesValidate,
}

var rulesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all loaded rules",
	RunE:  runRulesList,
}

var rulesReviewCmd = &cobra.Command{
	Use:   "review",
	Short: "List candidate rules with safety assessment",
	RunE:  runRulesReview,
}

var rulesPromoteCmd = &cobra.Command{
	Use:   "promote <candidate-id>",
	Short: "Promote a candidate rule to active rules",
	Args:  cobra.ExactArgs(1),
	RunE:  runRulesPromote,
}

var rulesRejectCmd = &cobra.Command{
	Use:   "reject <candidate-id>",
	Short: "Defer to classifier — no rule created, pattern suppressed from future suggestions",
	Args:  cobra.ExactArgs(1),
	RunE:  runRulesReject,
}

func init() {
	rulesPromoteCmd.Flags().StringVar(&promoteAction, "action", "", "override action (alert_now, daily_summary, ignore, needs_review)")
	rulesCmd.AddCommand(rulesValidateCmd)
	rulesCmd.AddCommand(rulesListCmd)
	rulesCmd.AddCommand(rulesReviewCmd)
	rulesCmd.AddCommand(rulesPromoteCmd)
	rulesCmd.AddCommand(rulesRejectCmd)
	rootCmd.AddCommand(rulesCmd)
}

func rulesDir() string {
	return filepath.Join(filepath.Dir(configPath()), "rules")
}

func runRulesValidate(cmd *cobra.Command, args []string) error {
	ruleList, err := rules.LoadDir(rulesDir())
	if err != nil {
		return err
	}

	if err := rules.Validate(ruleList); err != nil {
		return err
	}

	fmt.Printf("%d rules loaded and valid\n", len(ruleList))
	return nil
}

func runRulesList(cmd *cobra.Command, args []string) error {
	ruleList, err := rules.LoadDir(rulesDir())
	if err != nil {
		return err
	}

	if err := rules.Validate(ruleList); err != nil {
		return err
	}

	grouped := make(map[rules.Action][]rules.Rule)
	for _, r := range ruleList {
		grouped[r.Action] = append(grouped[r.Action], r)
	}

	actionOrder := []rules.Action{
		rules.ActionAlertNow,
		rules.ActionDailySummary,
		rules.ActionNeedsReview,
		rules.ActionIgnore,
	}

	for _, action := range actionOrder {
		group := grouped[action]
		if len(group) == 0 {
			continue
		}
		fmt.Printf("\n%s (%d)\n", action, len(group))
		for _, r := range group {
			if r.IsEnabled() {
				fmt.Printf("  %-40s %s\n", r.ID, r.Description)
			} else {
				fmt.Printf("  %-40s %s (disabled)\n", r.ID, r.Description)
			}
		}
	}
	fmt.Println()
	return nil
}

func candidatesPath() string {
	return filepath.Join(rulesDir(), "800-llm-candidates.yaml")
}

func activePath() string {
	return filepath.Join(rulesDir(), "100-active.yaml")
}

func rejectedPath() string {
	return filepath.Join(rulesDir(), "900-rejected.yaml")
}

func runRulesReview(cmd *cobra.Command, args []string) error {
	candidates, err := consolidate.LoadCandidates(candidatesPath())
	if err != nil {
		return fmt.Errorf("loading candidates: %w", err)
	}

	if len(candidates) == 0 {
		fmt.Println("no candidates to review")
		return nil
	}

	for _, c := range candidates {
		issues := rules.CheckSafety(c.Match, c.Action)
		safety := "OK"
		if rules.HasRejectIssues(issues) {
			safety = "REJECT"
		} else if len(issues) > 0 {
			safety = "WARN"
		}

		fmt.Printf("%-40s %-15s %-8s %s\n", c.ID, c.Action, safety, c.Reason)

		if c.Match.FromEmail != "" {
			fmt.Printf("  from_email: %s\n", c.Match.FromEmail)
		}
		if c.Match.FromDomain != "" {
			fmt.Printf("  from_domain: %s\n", c.Match.FromDomain)
		}
		if c.Match.ListID != "" {
			fmt.Printf("  list_id: %s\n", c.Match.ListID)
		}
		if len(c.Match.SubjectContainsAll) > 0 {
			fmt.Printf("  subject_contains_all: %v\n", c.Match.SubjectContainsAll)
		}
		if len(c.Match.SubjectContainsAny) > 0 {
			fmt.Printf("  subject_contains_any: %v\n", c.Match.SubjectContainsAny)
		}

		for _, issue := range issues {
			fmt.Printf("  [%s] %s\n", issue.Severity, issue.Message)
		}
		fmt.Println()
	}

	fmt.Printf("%d candidate(s)\n", len(candidates))
	fmt.Println("\nto promote:             mailtriaged rules promote <id>")
	fmt.Println("to defer to classifier: mailtriaged rules reject <id>")
	return nil
}

func runRulesPromote(cmd *cobra.Command, args []string) error {
	candidateID := args[0]

	var actionOverride rules.Action
	if promoteAction != "" {
		actionOverride = rules.Action(promoteAction)
		if !rules.IsValidAction(actionOverride) {
			return fmt.Errorf("invalid action %q; valid: alert_now, daily_summary, ignore, needs_review", promoteAction)
		}
	}

	if err := consolidate.Promote(candidatesPath(), activePath(), candidateID, actionOverride); err != nil {
		return err
	}

	fmt.Printf("promoted %q to %s\n", candidateID, activePath())

	// Validate the resulting rule set
	ruleList, err := rules.LoadDir(rulesDir())
	if err != nil {
		return fmt.Errorf("post-promote validation failed: %w", err)
	}
	if err := rules.Validate(ruleList); err != nil {
		return fmt.Errorf("post-promote validation failed: %w", err)
	}
	fmt.Printf("validated: %d rules total\n", len(ruleList))
	return nil
}

func runRulesReject(cmd *cobra.Command, args []string) error {
	candidateID := args[0]

	if err := consolidate.Reject(candidatesPath(), rejectedPath(), candidateID); err != nil {
		return err
	}

	fmt.Printf("rejected %q → %s\n", candidateID, rejectedPath())
	return nil
}
