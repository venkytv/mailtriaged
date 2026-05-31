package rules

import "testing"

func TestCheckSafety_DomainOnlyIgnore(t *testing.T) {
	issues := CheckSafety(Match{FromDomain: "github.com"}, ActionIgnore)
	if !HasRejectIssues(issues) {
		t.Error("expected reject for domain-only ignore rule")
	}
}

func TestCheckSafety_SubjectOnlyIgnore(t *testing.T) {
	issues := CheckSafety(Match{SubjectContainsAny: []string{"alert"}}, ActionIgnore)
	if !HasRejectIssues(issues) {
		t.Error("expected reject for subject-only ignore rule")
	}
}

func TestCheckSafety_NarrowIgnore(t *testing.T) {
	issues := CheckSafety(Match{
		FromEmail: "notifications@github.com",
		ListID:    "owner/repo-x",
		SubjectContainsAll: []string{"dependabot", "alert"},
	}, ActionIgnore)
	if HasRejectIssues(issues) {
		t.Errorf("expected no reject issues for narrow rule, got: %v", issues)
	}
}

func TestCheckSafety_AlertNowWarn(t *testing.T) {
	issues := CheckSafety(Match{FromEmail: "bank@example.com"}, ActionAlertNow)
	if len(issues) == 0 {
		t.Error("expected warning for alert_now rule")
	}
	if issues[0].Severity != "warn" {
		t.Errorf("expected warn severity, got %q", issues[0].Severity)
	}
}

func TestCheckSafety_EmptyMatch(t *testing.T) {
	issues := CheckSafety(Match{}, ActionIgnore)
	if !HasRejectIssues(issues) {
		t.Error("expected reject for empty match")
	}
}

func TestCheckSafety_DomainOnlyDailySummary(t *testing.T) {
	issues := CheckSafety(Match{FromDomain: "example.com"}, ActionDailySummary)
	if !HasRejectIssues(issues) {
		t.Error("expected reject for domain-only daily_summary rule")
	}
}

func TestCheckSafety_DomainPlusSubject(t *testing.T) {
	issues := CheckSafety(Match{
		FromDomain:         "github.com",
		SubjectContainsAll: []string{"dependabot"},
	}, ActionIgnore)
	if HasRejectIssues(issues) {
		t.Errorf("two-field rule should not be rejected, got: %v", issues)
	}
}
