package consolidate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/venky/mailtriaged/internal/rules"
	"gopkg.in/yaml.v3"
)

const testCandidates = `candidates:
  - id: candidate_20260531_091500
    created_at: "2026-05-31T09:15:00Z"
    source_message_id: "<abc@example.com>"
    proposed_by: classifier
    action: ignore
    reason: "Recurring dependency alert"
    safety: narrow
    match:
      from_email: notifications@github.com
      list_id: "owner/repo-x"
      subject_contains_all:
        - dependabot
        - alert
  - id: candidate_20260531_100000
    created_at: "2026-05-31T10:00:00Z"
    source_message_id: "<def@example.com>"
    proposed_by: classifier
    action: ignore
    reason: "Broad domain rule"
    safety: broad
    match:
      from_domain: github.com
`

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "800-llm-candidates.yaml"), []byte(testCandidates), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPromote_NarrowRule(t *testing.T) {
	dir := setupTestDir(t)
	candidatesPath := filepath.Join(dir, "800-llm-candidates.yaml")
	activePath := filepath.Join(dir, "100-active.yaml")

	if err := Promote(candidatesPath, activePath, "candidate_20260531_091500"); err != nil {
		t.Fatalf("promote failed: %v", err)
	}

	// Check active rules
	data, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "candidate_20260531_091500") {
		t.Error("promoted rule not found in active file")
	}

	// Check candidate removed
	remaining, err := LoadCandidates(candidatesPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining candidate, got %d", len(remaining))
	}
	if remaining[0].ID != "candidate_20260531_100000" {
		t.Errorf("wrong candidate remained: %s", remaining[0].ID)
	}
}

func TestPromote_BroadRuleRejected(t *testing.T) {
	dir := setupTestDir(t)
	candidatesPath := filepath.Join(dir, "800-llm-candidates.yaml")
	activePath := filepath.Join(dir, "100-active.yaml")

	err := Promote(candidatesPath, activePath, "candidate_20260531_100000")
	if err == nil {
		t.Fatal("expected safety check to reject broad rule")
	}
	if !strings.Contains(err.Error(), "safety check") {
		t.Errorf("expected safety check error, got: %v", err)
	}

	// Active file should not exist
	if _, err := os.Stat(activePath); !os.IsNotExist(err) {
		t.Error("active file should not have been created")
	}
}

func TestPromote_NotFound(t *testing.T) {
	dir := setupTestDir(t)
	candidatesPath := filepath.Join(dir, "800-llm-candidates.yaml")
	activePath := filepath.Join(dir, "100-active.yaml")

	err := Promote(candidatesPath, activePath, "nonexistent")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got: %v", err)
	}
}

func TestReject(t *testing.T) {
	dir := setupTestDir(t)
	candidatesPath := filepath.Join(dir, "800-llm-candidates.yaml")
	rejectedPath := filepath.Join(dir, "900-rejected.yaml")

	if err := Reject(candidatesPath, rejectedPath, "candidate_20260531_100000"); err != nil {
		t.Fatalf("reject failed: %v", err)
	}

	// Check rejected file
	data, err := os.ReadFile(rejectedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "candidate_20260531_100000") {
		t.Error("rejected candidate not found in rejected file")
	}

	// Check candidate removed
	remaining, err := LoadCandidates(candidatesPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining candidate, got %d", len(remaining))
	}
}

func TestPromote_ValidatesAsRule(t *testing.T) {
	dir := setupTestDir(t)
	candidatesPath := filepath.Join(dir, "800-llm-candidates.yaml")
	activePath := filepath.Join(dir, "100-active.yaml")

	if err := Promote(candidatesPath, activePath, "candidate_20260531_091500"); err != nil {
		t.Fatalf("promote failed: %v", err)
	}

	ruleList, err := rules.LoadDir(dir)
	if err != nil {
		t.Fatalf("loading rules after promote: %v", err)
	}
	if err := rules.Validate(ruleList); err != nil {
		t.Fatalf("promoted rule fails validation: %v", err)
	}
}

func TestPromote_DuplicateID(t *testing.T) {
	dir := setupTestDir(t)
	candidatesPath := filepath.Join(dir, "800-llm-candidates.yaml")
	activePath := filepath.Join(dir, "100-active.yaml")

	// Pre-create active file with same ID
	existing := struct {
		Rules []rules.Rule `yaml:"rules"`
	}{
		Rules: []rules.Rule{
			{ID: "candidate_20260531_091500", Action: rules.ActionIgnore, Match: rules.Match{FromEmail: "x@y.com"}},
		},
	}
	data, _ := yaml.Marshal(&existing)
	os.WriteFile(activePath, data, 0644)

	err := Promote(candidatesPath, activePath, "candidate_20260531_091500")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected duplicate error, got: %v", err)
	}
}

func TestLoadCandidates_MissingFile(t *testing.T) {
	candidates, err := LoadCandidates("/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected empty candidates, got %d", len(candidates))
	}
}
