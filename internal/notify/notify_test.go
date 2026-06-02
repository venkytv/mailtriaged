package notify

import (
	"fmt"
	"testing"

	"github.com/venky/mailtriaged/internal/email"
	"github.com/venky/mailtriaged/internal/store"
)

func TestFormatAlert(t *testing.T) {
	msg := &email.Message{
		From:    email.Address{Email: "bank@example.com"},
		Subject: "Transaction declined",
	}
	d := &Decision{
		Action: "alert_now",
		Reason: "Appears to need immediate attention",
	}

	text := formatAlert(msg, d)

	if text == "" {
		t.Fatal("expected non-empty text")
	}
	for _, want := range []string{"bank@example.com", "Transaction declined", "immediate attention"} {
		if !contains(text, want) {
			t.Errorf("alert text missing %q\ngot: %s", want, text)
		}
	}
}

func TestFormatAlert_WithSummary(t *testing.T) {
	msg := &email.Message{
		From:    email.Address{Email: "bank@example.com"},
		Subject: "Transaction declined",
	}
	summary := "Your card was declined for a $500 purchase"
	d := &Decision{
		Action:  "alert_now",
		Reason:  "Urgent",
		Summary: &summary,
	}

	text := formatAlert(msg, d)
	if !contains(text, "declined for a $500") {
		t.Errorf("alert text missing summary\ngot: %s", text)
	}
}

func TestFormatSummary(t *testing.T) {
	items := []store.SummaryItemRow{
		{
			ID: 1, FromEmail: "ci@example.com", Subject: "Build passed",
			Summary: "CI passed for main", Action: "daily_summary",
		},
		{
			ID: 2, FromEmail: "unknown@example.com", Subject: "Hey there",
			Summary: "Unknown sender", Action: "needs_review",
		},
	}

	text := FormatSummary(items)

	if !contains(text, "Daily mail summary") {
		t.Error("missing header")
	}
	if !contains(text, "Needs review") {
		t.Error("missing needs_review section")
	}
	if !contains(text, "Summary") {
		t.Error("missing summary section")
	}
	if !contains(text, "ci@example.com") {
		t.Error("missing summary item")
	}
	if !contains(text, "unknown@example.com") {
		t.Error("missing needs_review item")
	}
}

func TestFormatSummary_Consolidated(t *testing.T) {
	items := []store.SummaryItemRow{
		{ID: 1, FromEmail: "backup@nas.local", Subject: "Backup May 31", Summary: "backup success", Action: "daily_summary"},
		{ID: 2, FromEmail: "backup@nas.local", Subject: "Backup Jun 01", Summary: "backup success", Action: "daily_summary"},
		{ID: 3, FromEmail: "backup@nas.local", Subject: "Backup Jun 02", Summary: "backup success", Action: "daily_summary"},
		{ID: 4, FromEmail: "news@example.com", Subject: "Weekly digest", Summary: "newsletter", Action: "daily_summary"},
	}

	text := FormatSummary(items)

	if !contains(text, "×3") {
		t.Errorf("expected consolidated count ×3 in output:\n%s", text)
	}
	if contains(text, "Backup May 31") || contains(text, "Backup Jun 01") {
		t.Errorf("consolidated items should not show individual subjects:\n%s", text)
	}
	if !contains(text, "Weekly digest") {
		t.Errorf("non-consolidated item should still show subject:\n%s", text)
	}
}

func TestFormatSummary_Empty(t *testing.T) {
	text := FormatSummary(nil)
	if !contains(text, "Daily mail summary") {
		t.Error("expected header even for empty summary")
	}
}

func TestSplitSummaryChunks_SmallFitsInOne(t *testing.T) {
	items := []store.SummaryItemRow{
		{ID: 1, FromEmail: "a@b.com", Subject: "Hi", Summary: "test", Action: "daily_summary"},
	}
	chunks := splitSummaryChunks(items)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if len(chunks[0]) != 1 {
		t.Errorf("expected 1 item in chunk, got %d", len(chunks[0]))
	}
}

func TestSplitSummaryChunks_LargeSplitsMultiple(t *testing.T) {
	var items []store.SummaryItemRow
	for i := 0; i < 50; i++ {
		items = append(items, store.SummaryItemRow{
			ID:        int64(i),
			FromEmail: fmt.Sprintf("sender%d@example.com", i),
			Subject:   "A reasonably long subject line for testing purposes",
			Summary:   fmt.Sprintf("Unique summary %d that takes up space in the message to simulate real content", i),
			Action:    "daily_summary",
		})
	}

	chunks := splitSummaryChunks(items)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for 50 items, got %d", len(chunks))
	}

	total := 0
	for _, chunk := range chunks {
		text := FormatSummary(chunk)
		if len(text) > telegramMaxMessageLen {
			t.Errorf("chunk exceeds telegram limit: %d > %d", len(text), telegramMaxMessageLen)
		}
		total += len(chunk)
	}
	if total != len(items) {
		t.Errorf("chunks contain %d items, want %d", total, len(items))
	}
}

func TestSplitSummaryChunks_Empty(t *testing.T) {
	chunks := splitSummaryChunks(nil)
	if chunks != nil {
		t.Errorf("expected nil for empty input, got %v", chunks)
	}
}

func TestEscapeMarkdown(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"hello_world", "hello\\_world"},
		{"*bold*", "\\*bold\\*"},
		{"[link]", "\\[link]"},
		{"`code`", "\\`code\\`"},
	}
	for _, tt := range tests {
		got := escapeMarkdown(tt.in)
		if got != tt.want {
			t.Errorf("escapeMarkdown(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseSendTime(t *testing.T) {
	tests := []struct {
		in         string
		wantH, wantM int
	}{
		{"08:00", 8, 0},
		{"23:30", 23, 30},
		{"00:00", 0, 0},
	}
	for _, tt := range tests {
		h, m := parseSendTime(tt.in)
		if h != tt.wantH || m != tt.wantM {
			t.Errorf("parseSendTime(%q) = (%d, %d), want (%d, %d)", tt.in, h, m, tt.wantH, tt.wantM)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
