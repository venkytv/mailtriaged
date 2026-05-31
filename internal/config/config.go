package config

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	IMAP          IMAP          `yaml:"imap"`
	Classifier    Classifier    `yaml:"classifier"`
	Notifications Notifications `yaml:"notifications"`
	Summary       Summary       `yaml:"summary"`
	Runtime       Runtime       `yaml:"runtime"`
}

type IMAP struct {
	Host            string   `yaml:"host"`
	Port            int      `yaml:"port"`
	Username        string   `yaml:"username"`
	Password        string   `yaml:"-"`
	PasswordCommand string   `yaml:"password_command"`
	Folders         []string `yaml:"folders"`
}

type Classifier struct {
	Command             []string `yaml:"command"`
	TimeoutSeconds      int      `yaml:"timeout_seconds"`
	MaxBodyExcerptChars int      `yaml:"max_body_excerpt_chars"`
	Instruction         string   `yaml:"instruction"`
}

type Notifications struct {
	Telegram Telegram `yaml:"telegram"`
}

type Telegram struct {
	Enabled         bool   `yaml:"enabled"`
	BotToken        string `yaml:"-"`
	BotTokenCommand string `yaml:"bot_token_command"`
	ChatID          string `yaml:"chat_id"`
}

type Summary struct {
	Enabled  bool   `yaml:"enabled"`
	SendTime string `yaml:"send_time"`
	Timezone string `yaml:"timezone"`
}

type Runtime struct {
	RulesReloadSeconds       int   `yaml:"rules_reload_seconds"`
	ReconnectBackoffSeconds  []int `yaml:"reconnect_backoff_seconds"`
	DisableRules             bool  `yaml:"disable_rules"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.setDefaults(); err != nil {
		return nil, err
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) setDefaults() error {
	if c.IMAP.Port == 0 {
		c.IMAP.Port = 993
	}
	if c.Classifier.TimeoutSeconds == 0 {
		c.Classifier.TimeoutSeconds = 30
	}
	if c.Classifier.MaxBodyExcerptChars == 0 {
		c.Classifier.MaxBodyExcerptChars = 6000
	}
	if c.Runtime.RulesReloadSeconds == 0 {
		c.Runtime.RulesReloadSeconds = 30
	}
	if len(c.Runtime.ReconnectBackoffSeconds) == 0 {
		c.Runtime.ReconnectBackoffSeconds = []int{5, 15, 60, 300}
	}
	if c.Summary.Timezone == "" {
		c.Summary.Timezone = "UTC"
	}
	return nil
}

func (c *Config) validate() error {
	if c.IMAP.Host == "" {
		return fmt.Errorf("imap.host is required")
	}
	if c.IMAP.Username == "" {
		return fmt.Errorf("imap.username is required")
	}
	if len(c.IMAP.Folders) == 0 {
		return fmt.Errorf("imap.folders must have at least one entry")
	}
	if len(c.Classifier.Command) == 0 {
		return fmt.Errorf("classifier.command is required")
	}
	return nil
}

func (c *Config) ResolveSecrets() error {
	if c.IMAP.PasswordCommand != "" {
		pw, err := runCommand(c.IMAP.PasswordCommand)
		if err != nil {
			return fmt.Errorf("resolving imap password: %w", err)
		}
		c.IMAP.Password = pw
	}
	if c.Notifications.Telegram.BotTokenCommand != "" {
		token, err := runCommand(c.Notifications.Telegram.BotTokenCommand)
		if err != nil {
			return fmt.Errorf("resolving telegram bot token: %w", err)
		}
		c.Notifications.Telegram.BotToken = token
	}
	return nil
}

func runCommand(cmdStr string) (string, error) {
	cmd := exec.Command("sh", "-c", cmdStr)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("running %q: %w", cmdStr, err)
	}
	return strings.TrimSpace(string(out)), nil
}
