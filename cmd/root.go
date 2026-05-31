package cmd

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "mailtriaged",
	Short: "Personal mail triage daemon",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/mailtriaged/config.yaml)")
}

func configPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mailtriaged", "config.yaml")
}
