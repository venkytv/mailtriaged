package classifier

import (
	"testing"

	"github.com/venky/mailtriaged/internal/email"
	"github.com/venky/mailtriaged/internal/rules"
)

func TestBuildRequest(t *testing.T) {
	msg := &email.Message{
		Account:     "you@example.com",
		Folder:      "INBOX",
		ImapUID:     12345,
		MessageID:   "abc@example.com",
		From:        email.Address{Name: "GitHub", Email: "notifications@github.com", Domain: "github.com"},
		To:          []string{"you@example.com"},
		Subject:     "test subject",
		ReceivedAt:  "2026-05-31T09:15:00+01:00",
		Headers:     map[string]string{"list-id": "owner/repo-x"},
		BodyExcerpt: "body text",
	}

	req := BuildRequest(msg, "")

	if req.SchemaVersion != 1 {
		t.Errorf("schema_version: got %d", req.SchemaVersion)
	}
	if req.Message.From.Email != "notifications@github.com" {
		t.Errorf("from email: got %q", req.Message.From.Email)
	}
	if len(req.ValidActions) != 4 {
		t.Errorf("valid_actions: got %d", len(req.ValidActions))
	}
	if req.RuleCapabilities.RegexSupported {
		t.Error("regex_supported should be false")
	}
}

func TestExecute_Success(t *testing.T) {
	req := &Request{SchemaVersion: 1}

	resp, record := Execute(
		[]string{"sh", "-c", `echo '{"schema_version":1,"action":"ignore","reason":"test reason","summary":null,"suggested_rule":null}'`},
		req, 5,
	)

	if record.Err != nil {
		t.Fatalf("unexpected error: %v", record.Err)
	}
	if resp.Action != rules.ActionIgnore {
		t.Errorf("action: got %q", resp.Action)
	}
	if resp.Reason != "test reason" {
		t.Errorf("reason: got %q", resp.Reason)
	}
	if record.DurationMs < 0 {
		t.Error("duration should be non-negative")
	}
}

func TestExecute_WithSuggestedRule(t *testing.T) {
	script := `echo '{"schema_version":1,"action":"ignore","reason":"recurring alert","summary":null,"suggested_rule":{"id_hint":"test_rule","description":"test","action":"ignore","safety":"narrow","match":{"from_email":"a@b.com"}}}'`

	resp, record := Execute([]string{"sh", "-c", script}, &Request{SchemaVersion: 1}, 5)

	if record.Err != nil {
		t.Fatalf("unexpected error: %v", record.Err)
	}
	if resp.SuggestedRule == nil {
		t.Fatal("expected suggested rule")
	}
	if resp.SuggestedRule.IDHint != "test_rule" {
		t.Errorf("id_hint: got %q", resp.SuggestedRule.IDHint)
	}
	if resp.SuggestedRule.Match.FromEmail != "a@b.com" {
		t.Errorf("match from_email: got %q", resp.SuggestedRule.Match.FromEmail)
	}
}

func TestExecute_Timeout(t *testing.T) {
	_, record := Execute([]string{"sleep", "10"}, &Request{SchemaVersion: 1}, 1)

	if record.Err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestExecute_NonZeroExit(t *testing.T) {
	_, record := Execute([]string{"sh", "-c", "exit 1"}, &Request{SchemaVersion: 1}, 5)

	if record.Err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if record.ExitCode != 1 {
		t.Errorf("exit code: got %d", record.ExitCode)
	}
}

func TestExecute_InvalidJSON(t *testing.T) {
	_, record := Execute([]string{"echo", "not json"}, &Request{SchemaVersion: 1}, 5)

	if record.Err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestExecute_MissingAction(t *testing.T) {
	_, record := Execute(
		[]string{"sh", "-c", `echo '{"schema_version":1,"reason":"test"}'`},
		&Request{SchemaVersion: 1}, 5,
	)

	if record.Err == nil {
		t.Fatal("expected error for missing action")
	}
}

func TestExecute_UnsupportedAction(t *testing.T) {
	_, record := Execute(
		[]string{"sh", "-c", `echo '{"schema_version":1,"action":"delete","reason":"test"}'`},
		&Request{SchemaVersion: 1}, 5,
	)

	if record.Err == nil {
		t.Fatal("expected error for unsupported action")
	}
}

func TestExecute_SuggestedRuleEmptyMatch(t *testing.T) {
	script := `echo '{"schema_version":1,"action":"ignore","reason":"test","suggested_rule":{"id_hint":"x","description":"x","action":"ignore","safety":"narrow","match":{}}}'`

	_, record := Execute([]string{"sh", "-c", script}, &Request{SchemaVersion: 1}, 5)

	if record.Err == nil {
		t.Fatal("expected error for empty match in suggested rule")
	}
}

func TestExecute_CapturesStderr(t *testing.T) {
	_, record := Execute(
		[]string{"sh", "-c", `echo '{"schema_version":1,"action":"ignore","reason":"ok"}' && echo "debug info" >&2`},
		&Request{SchemaVersion: 1}, 5,
	)

	if record.Err != nil {
		t.Fatalf("unexpected error: %v", record.Err)
	}
	if record.Stderr == "" {
		t.Error("expected captured stderr")
	}
}

func TestExecute_WithSummary(t *testing.T) {
	script := `echo '{"schema_version":1,"action":"daily_summary","reason":"useful","summary":"CI update for repo-x"}'`

	resp, record := Execute([]string{"sh", "-c", script}, &Request{SchemaVersion: 1}, 5)

	if record.Err != nil {
		t.Fatalf("unexpected error: %v", record.Err)
	}
	if resp.Summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if *resp.Summary != "CI update for repo-x" {
		t.Errorf("summary: got %q", *resp.Summary)
	}
}
