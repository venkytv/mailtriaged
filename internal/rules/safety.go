package rules

import "fmt"

type SafetyIssue struct {
	Severity string // "reject" or "warn"
	Message  string
}

func CheckSafety(m Match, action Action) []SafetyIssue {
	var issues []SafetyIssue

	fieldCount := matchFieldCount(m)

	if action == ActionIgnore || action == ActionDailySummary {
		if m.FromDomain != "" && fieldCount == 1 {
			issues = append(issues, SafetyIssue{
				Severity: "reject",
				Message:  fmt.Sprintf("domain-only %s rule is too broad (from_domain=%q)", action, m.FromDomain),
			})
		}

		if (len(m.SubjectContainsAny) > 0 || len(m.SubjectContainsAll) > 0) && fieldCount == 1 {
			issues = append(issues, SafetyIssue{
				Severity: "reject",
				Message:  fmt.Sprintf("subject-only %s rule is too broad", action),
			})
		}

		if fieldCount == 1 && m.FromEmail == "" {
			if m.ToContains != "" || m.CcContains != "" {
				issues = append(issues, SafetyIssue{
					Severity: "warn",
					Message:  fmt.Sprintf("single-field %s rule; consider adding sender constraints", action),
				})
			}
		}
	}

	if action == ActionAlertNow {
		issues = append(issues, SafetyIssue{
			Severity: "warn",
			Message:  "alert_now rules should typically be manually written",
		})
	}

	if m.IsEmpty() {
		issues = append(issues, SafetyIssue{
			Severity: "reject",
			Message:  "match block is empty; would match all messages",
		})
	}

	return issues
}

func HasRejectIssues(issues []SafetyIssue) bool {
	for _, issue := range issues {
		if issue.Severity == "reject" {
			return true
		}
	}
	return false
}

func matchFieldCount(m Match) int {
	count := 0
	if m.FromEmail != "" {
		count++
	}
	if m.FromDomain != "" {
		count++
	}
	if m.ToContains != "" {
		count++
	}
	if m.CcContains != "" {
		count++
	}
	if m.ListID != "" {
		count++
	}
	if len(m.SubjectContainsAll) > 0 {
		count++
	}
	if len(m.SubjectContainsAny) > 0 {
		count++
	}
	if len(m.HeaderEquals) > 0 {
		count++
	}
	if len(m.HeaderContains) > 0 {
		count++
	}
	return count
}
