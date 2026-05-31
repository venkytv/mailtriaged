package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/spf13/cobra"
)

const plistLabel = "com.venky.mailtriaged"

var plistTemplate = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>{{ .Label }}</string>

  <key>ProgramArguments</key>
  <array>
    <string>{{ .BinaryPath }}</string>
    <string>run</string>
    <string>--config</string>
    <string>{{ .ConfigPath }}</string>
  </array>

  <key>RunAtLoad</key>
  <true/>

  <key>KeepAlive</key>
  <true/>

  <key>StandardOutPath</key>
  <string>{{ .LogDir }}/stdout.log</string>

  <key>StandardErrorPath</key>
  <string>{{ .LogDir }}/stderr.log</string>
</dict>
</plist>
`))

type plistData struct {
	Label      string
	BinaryPath string
	ConfigPath string
	LogDir     string
}

var launchdCmd = &cobra.Command{
	Use:   "launchd",
	Short: "Manage launchd user agent",
}

var launchdInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install and load the launchd user agent",
	RunE:  runLaunchdInstall,
}

var launchdUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Unload and remove the launchd user agent",
	RunE:  runLaunchdUninstall,
}

var launchdStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show launchd agent status",
	RunE:  runLaunchdStatus,
}

func init() {
	launchdCmd.AddCommand(launchdInstallCmd)
	launchdCmd.AddCommand(launchdUninstallCmd)
	launchdCmd.AddCommand(launchdStatusCmd)
	rootCmd.AddCommand(launchdCmd)
}

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", plistLabel+".plist")
}

func logDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "mailtriaged", "logs")
}

func runLaunchdInstall(cmd *cobra.Command, args []string) error {
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("determining binary path: %w", err)
	}
	binPath, err = filepath.EvalSymlinks(binPath)
	if err != nil {
		return fmt.Errorf("resolving binary path: %w", err)
	}

	cfgPath := configPath()
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		return fmt.Errorf("config file not found: %s", cfgPath)
	}

	logs := logDir()
	if err := os.MkdirAll(logs, 0700); err != nil {
		return fmt.Errorf("creating log directory: %w", err)
	}
	fmt.Printf("log directory: %s\n", logs)

	plist := plistPath()
	if err := os.MkdirAll(filepath.Dir(plist), 0755); err != nil {
		return fmt.Errorf("creating LaunchAgents directory: %w", err)
	}

	// Unload existing agent if present
	if _, err := os.Stat(plist); err == nil {
		_ = exec.Command("launchctl", "unload", plist).Run()
	}

	f, err := os.Create(plist)
	if err != nil {
		return fmt.Errorf("creating plist: %w", err)
	}
	defer f.Close()

	data := plistData{
		Label:      plistLabel,
		BinaryPath: binPath,
		ConfigPath: cfgPath,
		LogDir:     logs,
	}
	if err := plistTemplate.Execute(f, data); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}

	fmt.Printf("plist written: %s\n", plist)

	if err := exec.Command("launchctl", "load", plist).Run(); err != nil {
		return fmt.Errorf("launchctl load failed: %w", err)
	}

	fmt.Println("agent loaded successfully")
	fmt.Printf("\nuseful commands:\n")
	fmt.Printf("  launchctl list %s          # check status\n", plistLabel)
	fmt.Printf("  tail -f %s/stderr.log      # watch logs\n", logs)
	fmt.Printf("  mailtriaged launchd uninstall        # stop and remove\n")
	return nil
}

func runLaunchdUninstall(cmd *cobra.Command, args []string) error {
	plist := plistPath()

	if _, err := os.Stat(plist); os.IsNotExist(err) {
		return fmt.Errorf("plist not found: %s", plist)
	}

	if err := exec.Command("launchctl", "unload", plist).Run(); err != nil {
		fmt.Printf("warning: launchctl unload failed: %v\n", err)
	}

	if err := os.Remove(plist); err != nil {
		return fmt.Errorf("removing plist: %w", err)
	}

	fmt.Printf("removed %s\n", plist)
	fmt.Println("agent uninstalled")
	return nil
}

func runLaunchdStatus(cmd *cobra.Command, args []string) error {
	plist := plistPath()

	if _, err := os.Stat(plist); os.IsNotExist(err) {
		fmt.Println("not installed")
		return nil
	}

	fmt.Printf("plist: %s\n", plist)

	out, err := exec.Command("launchctl", "list", plistLabel).CombinedOutput()
	if err != nil {
		fmt.Println("status: not running")
		return nil
	}

	fmt.Printf("\n%s", out)
	return nil
}
