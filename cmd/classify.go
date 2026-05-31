package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/venky/mailtriaged/internal/classify"
	"github.com/venky/mailtriaged/internal/config"
)

var (
	emlFile    string
	dryRun     bool
)

var classifyCmd = &cobra.Command{
	Use:   "classify",
	Short: "Classify an email from a .eml file",
	RunE:  runClassify,
}

func init() {
	classifyCmd.Flags().StringVar(&emlFile, "file", "", "path to .eml file (required)")
	classifyCmd.MarkFlagRequired("file")
	classifyCmd.Flags().BoolVar(&dryRun, "dry-run", false, "skip external classifier, only apply local rules")
	rootCmd.AddCommand(classifyCmd)
}

func runClassify(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configPath())
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	rulesDir := filepath.Join(filepath.Dir(configPath()), "rules")

	result, err := classify.ClassifyFile(cfg, rulesDir, emlFile, dryRun)
	if err != nil {
		return err
	}

	return classify.PrintJSON(result)
}
