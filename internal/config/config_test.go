package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
imap:
  host: imap.example.com
  username: you@example.com
  folders:
    - INBOX
classifier:
  command:
    - hermes
    - run
`), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.IMAP.Host != "imap.example.com" {
		t.Errorf("host: got %q", cfg.IMAP.Host)
	}
	if cfg.IMAP.Port != 993 {
		t.Errorf("port default: got %d", cfg.IMAP.Port)
	}
	if cfg.Classifier.TimeoutSeconds != 30 {
		t.Errorf("timeout default: got %d", cfg.Classifier.TimeoutSeconds)
	}
	if cfg.Classifier.MaxBodyExcerptChars != 6000 {
		t.Errorf("max body default: got %d", cfg.Classifier.MaxBodyExcerptChars)
	}
}

func TestLoad_MissingHost(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
imap:
  username: you@example.com
  folders:
    - INBOX
classifier:
  command:
    - hermes
`), 0644)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestLoad_MissingClassifierCommand(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
imap:
  host: imap.example.com
  username: you@example.com
  folders:
    - INBOX
classifier:
  command: []
`), 0644)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing classifier command")
	}
}

func TestResolveSecrets(t *testing.T) {
	cfg := &Config{
		IMAP: IMAP{
			PasswordCommand: "echo test-secret",
		},
	}

	if err := cfg.ResolveSecrets(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.IMAP.Password != "test-secret" {
		t.Errorf("password: got %q", cfg.IMAP.Password)
	}
}
