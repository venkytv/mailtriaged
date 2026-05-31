package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestMatchRule_FromEmail(t *testing.T) {
	m := Match{FromEmail: "notifications@github.com"}
	msg := MessageData{FromEmail: "notifications@github.com"}
	if !matchRule(m, msg) {
		t.Fatal("expected match")
	}

	msg.FromEmail = "other@github.com"
	if matchRule(m, msg) {
		t.Fatal("expected no match")
	}
}

func TestMatchRule_CaseInsensitive(t *testing.T) {
	m := Match{FromEmail: "Notifications@GitHub.com"}
	msg := MessageData{FromEmail: "notifications@github.com"}
	if !matchRule(m, msg) {
		t.Fatal("expected case-insensitive match")
	}
}

func TestMatchRule_FromDomain(t *testing.T) {
	m := Match{FromDomain: "github.com"}
	msg := MessageData{FromDomain: "github.com"}
	if !matchRule(m, msg) {
		t.Fatal("expected match")
	}

	msg.FromDomain = "example.com"
	if matchRule(m, msg) {
		t.Fatal("expected no match")
	}
}

func TestMatchRule_SubjectContainsAll(t *testing.T) {
	m := Match{SubjectContainsAll: []string{"dependabot", "alert"}}

	msg := MessageData{Subject: "[repo-x] Dependabot Alert for openssl"}
	if !matchRule(m, msg) {
		t.Fatal("expected match (case-insensitive)")
	}

	msg.Subject = "[repo-x] Dependabot update for openssl"
	if matchRule(m, msg) {
		t.Fatal("expected no match (missing 'alert')")
	}
}

func TestMatchRule_SubjectContainsAny(t *testing.T) {
	m := Match{SubjectContainsAny: []string{"outage", "incident"}}

	msg := MessageData{Subject: "Production incident report"}
	if !matchRule(m, msg) {
		t.Fatal("expected match")
	}

	msg.Subject = "Weekly status update"
	if matchRule(m, msg) {
		t.Fatal("expected no match")
	}
}

func TestMatchRule_ToContains(t *testing.T) {
	m := Match{ToContains: "me@example.com"}
	msg := MessageData{To: []string{"team@example.com", "me@example.com"}}
	if !matchRule(m, msg) {
		t.Fatal("expected match")
	}

	msg.To = []string{"other@example.com"}
	if matchRule(m, msg) {
		t.Fatal("expected no match")
	}
}

func TestMatchRule_CcContains(t *testing.T) {
	m := Match{CcContains: "me@example.com"}
	msg := MessageData{Cc: []string{"me@example.com"}}
	if !matchRule(m, msg) {
		t.Fatal("expected match")
	}
}

func TestMatchRule_ListID(t *testing.T) {
	m := Match{ListID: "owner/repo-x"}
	msg := MessageData{ListID: "owner/repo-x"}
	if !matchRule(m, msg) {
		t.Fatal("expected match")
	}
}

func TestMatchRule_HeaderEquals(t *testing.T) {
	m := Match{HeaderEquals: map[string]string{"x-github-reason": "security_alert"}}
	msg := MessageData{Headers: map[string]string{"x-github-reason": "security_alert"}}
	if !matchRule(m, msg) {
		t.Fatal("expected match")
	}

	msg.Headers["x-github-reason"] = "ci_activity"
	if matchRule(m, msg) {
		t.Fatal("expected no match")
	}
}

func TestMatchRule_HeaderContains(t *testing.T) {
	m := Match{HeaderContains: map[string]string{"x-some-header": "useful"}}
	msg := MessageData{Headers: map[string]string{"x-some-header": "this-is-useful-fragment"}}
	if !matchRule(m, msg) {
		t.Fatal("expected match")
	}
}

func TestMatchRule_MultipleConditionsAND(t *testing.T) {
	m := Match{
		FromEmail:          "notifications@github.com",
		ListID:             "owner/repo-x",
		SubjectContainsAll: []string{"dependabot", "alert"},
	}
	msg := MessageData{
		FromEmail: "notifications@github.com",
		ListID:    "owner/repo-x",
		Subject:   "[repo-x] Dependabot alert for openssl",
	}
	if !matchRule(m, msg) {
		t.Fatal("expected match with all conditions")
	}

	msg.ListID = "owner/repo-y"
	if matchRule(m, msg) {
		t.Fatal("expected no match when one condition fails")
	}
}

func TestEvaluate_FirstMatchWins(t *testing.T) {
	rules := []Rule{
		{ID: "rule1", Action: ActionIgnore, Match: Match{FromDomain: "github.com"}},
		{ID: "rule2", Action: ActionAlertNow, Match: Match{FromDomain: "github.com"}},
	}
	msg := MessageData{FromDomain: "github.com"}
	d := Evaluate(rules, msg)
	if d == nil {
		t.Fatal("expected decision")
	}
	if d.RuleID != "rule1" {
		t.Fatalf("expected rule1, got %s", d.RuleID)
	}
	if d.Action != ActionIgnore {
		t.Fatalf("expected ignore, got %s", d.Action)
	}
}

func TestEvaluate_DisabledRuleSkipped(t *testing.T) {
	rules := []Rule{
		{ID: "disabled", Enabled: boolPtr(false), Action: ActionIgnore, Match: Match{FromDomain: "github.com"}},
		{ID: "enabled", Action: ActionAlertNow, Match: Match{FromDomain: "github.com"}},
	}
	msg := MessageData{FromDomain: "github.com"}
	d := Evaluate(rules, msg)
	if d == nil {
		t.Fatal("expected decision")
	}
	if d.RuleID != "enabled" {
		t.Fatalf("expected enabled rule, got %s", d.RuleID)
	}
}

func TestEvaluate_NoMatch(t *testing.T) {
	rules := []Rule{
		{ID: "rule1", Action: ActionIgnore, Match: Match{FromDomain: "github.com"}},
	}
	msg := MessageData{FromDomain: "example.com"}
	d := Evaluate(rules, msg)
	if d != nil {
		t.Fatal("expected no decision")
	}
}

func TestValidate_DuplicateID(t *testing.T) {
	rules := []Rule{
		{ID: "dup", Action: ActionIgnore, Match: Match{FromDomain: "a.com"}},
		{ID: "dup", Action: ActionIgnore, Match: Match{FromDomain: "b.com"}},
	}
	if err := Validate(rules); err == nil {
		t.Fatal("expected error for duplicate id")
	}
}

func TestValidate_EmptyMatch(t *testing.T) {
	rules := []Rule{
		{ID: "empty", Action: ActionIgnore, Match: Match{}},
	}
	if err := Validate(rules); err == nil {
		t.Fatal("expected error for empty match")
	}
}

func TestValidate_InvalidAction(t *testing.T) {
	rules := []Rule{
		{ID: "bad", Action: "delete", Match: Match{FromDomain: "a.com"}},
	}
	if err := Validate(rules); err == nil {
		t.Fatal("expected error for invalid action")
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, filepath.Join(dir, "000-test.yaml"), `rules:
  - id: test1
    action: ignore
    match:
      from_domain: example.com
`)
	writeYAML(t, filepath.Join(dir, "100-test.yaml"), `rules:
  - id: test2
    action: alert_now
    match:
      from_domain: bank.com
`)

	rules, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].ID != "test1" || rules[1].ID != "test2" {
		t.Fatal("rules not in expected order")
	}
}

func writeYAML(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
