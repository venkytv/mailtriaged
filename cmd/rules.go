package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/venky/mailtriaged/internal/rules"
)

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

func init() {
	rulesCmd.AddCommand(rulesValidateCmd)
	rulesCmd.AddCommand(rulesListCmd)
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

	for _, r := range ruleList {
		enabled := "enabled"
		if !r.IsEnabled() {
			enabled = "disabled"
		}
		fmt.Printf("%-40s %-15s %-10s %s\n", r.ID, r.Action, enabled, r.Description)
	}
	return nil
}
