package classifier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/venky/mailtriaged/internal/rules"
)

func TestAppendCandidate_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "800-llm-candidates.yaml")

	suggested := &SuggestedRule{
		IDHint:      "test_rule",
		Description: "Test rule",
		Action:      rules.ActionIgnore,
		Safety:      "narrow",
		Match:       rules.Match{FromEmail: "a@b.com"},
	}

	err := AppendCandidate(path, suggested, "msg-123", "test reason")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "a@b.com") {
		t.Error("expected from_email in output")
	}
	if !strings.Contains(content, "msg-123") {
		t.Error("expected message_id in output")
	}
}

func TestAppendCandidate_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "800-llm-candidates.yaml")

	s1 := &SuggestedRule{
		IDHint: "rule1", Action: rules.ActionIgnore, Safety: "narrow",
		Match: rules.Match{FromEmail: "a@b.com"},
	}
	s2 := &SuggestedRule{
		IDHint: "rule2", Action: rules.ActionDailySummary, Safety: "narrow",
		Match: rules.Match{FromEmail: "c@d.com"},
	}

	AppendCandidate(path, s1, "msg-1", "reason 1")
	AppendCandidate(path, s2, "msg-2", "reason 2")

	candidates, err := loadCandidates(path)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
}

func TestAppendCandidate_DeduplicatesMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "800-llm-candidates.yaml")

	suggested := &SuggestedRule{
		IDHint: "rule1", Action: rules.ActionIgnore, Safety: "narrow",
		Match: rules.Match{FromEmail: "a@b.com"},
	}

	AppendCandidate(path, suggested, "msg-1", "reason 1")
	AppendCandidate(path, suggested, "msg-2", "reason 2")

	candidates, err := loadCandidates(path)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate (deduped), got %d", len(candidates))
	}
}

func TestAppendCandidate_DifferentMatchNotDeduped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "800-llm-candidates.yaml")

	s1 := &SuggestedRule{
		IDHint: "rule1", Action: rules.ActionIgnore, Safety: "narrow",
		Match: rules.Match{FromEmail: "a@b.com"},
	}
	s2 := &SuggestedRule{
		IDHint: "rule1", Action: rules.ActionIgnore, Safety: "narrow",
		Match: rules.Match{FromEmail: "a@b.com", ListID: "extra"},
	}

	AppendCandidate(path, s1, "msg-1", "reason 1")
	AppendCandidate(path, s2, "msg-2", "reason 2")

	candidates, err := loadCandidates(path)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates (different match), got %d", len(candidates))
	}
}
