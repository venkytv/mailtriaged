package store

import (
	"testing"
)

func openTestDB(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigration(t *testing.T) {
	s := openTestDB(t)

	var version int
	err := s.db.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version)
	if err != nil {
		t.Fatalf("reading version: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("expected version %d, got %d", len(migrations), version)
	}
}

func TestMigration_Idempotent(t *testing.T) {
	s := openTestDB(t)

	// Run migrations again — should be a no-op
	if err := migrate(s.db); err != nil {
		t.Fatalf("re-running migrations: %v", err)
	}

	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM schema_version").Scan(&count)
	if count != len(migrations) {
		t.Fatalf("expected %d version rows, got %d", len(migrations), count)
	}
}

func TestInsertMessage(t *testing.T) {
	s := openTestDB(t)

	msg := &MessageRecord{
		Account:    "you@example.com",
		Folder:     "INBOX",
		ImapUID:    100,
		MessageID:  "abc@example.com",
		FromEmail:  "sender@example.com",
		FromDomain: "example.com",
		Subject:    "Test subject",
		ReceivedAt: "2026-05-31T09:00:00Z",
	}

	id, err := s.InsertMessage(msg)
	if err != nil {
		t.Fatalf("inserting message: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
}

func TestInsertMessage_Dedupe(t *testing.T) {
	s := openTestDB(t)

	msg := &MessageRecord{
		Account: "you@example.com",
		Folder:  "INBOX",
		ImapUID: 100,
	}

	id1, _ := s.InsertMessage(msg)
	id2, _ := s.InsertMessage(msg)

	if id1 != id2 {
		t.Fatalf("expected same id for duplicate, got %d and %d", id1, id2)
	}
}

func TestInsertMessage_DifferentUIDValidity(t *testing.T) {
	s := openTestDB(t)

	msg1 := &MessageRecord{
		Account:     "you@example.com",
		Folder:      "INBOX",
		ImapUID:     100,
		UIDValidity: 1,
	}
	msg2 := &MessageRecord{
		Account:     "you@example.com",
		Folder:      "INBOX",
		ImapUID:     100,
		UIDValidity: 2,
	}

	id1, _ := s.InsertMessage(msg1)
	id2, _ := s.InsertMessage(msg2)

	if id1 == id2 {
		t.Fatal("expected different ids for different uid_validity")
	}
}

func TestIsMessageSeen(t *testing.T) {
	s := openTestDB(t)

	seen, _ := s.IsMessageSeen("you@example.com", "INBOX", 100, 0)
	if seen {
		t.Fatal("expected not seen before insert")
	}

	s.InsertMessage(&MessageRecord{
		Account: "you@example.com",
		Folder:  "INBOX",
		ImapUID: 100,
	})

	seen, _ = s.IsMessageSeen("you@example.com", "INBOX", 100, 0)
	if !seen {
		t.Fatal("expected seen after insert")
	}
}

func TestInsertDecision(t *testing.T) {
	s := openTestDB(t)

	msgID, _ := s.InsertMessage(&MessageRecord{
		Account: "you@example.com",
		Folder:  "INBOX",
		ImapUID: 100,
	})

	id, err := s.InsertDecision(&DecisionRecord{
		MessageID: msgID,
		Action:    "ignore",
		Source:    "rule",
		RuleID:    "test_rule",
		Reason:    "Matched active rule",
	})
	if err != nil {
		t.Fatalf("inserting decision: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
}

func TestInsertRuleHit(t *testing.T) {
	s := openTestDB(t)

	msgID, _ := s.InsertMessage(&MessageRecord{
		Account: "you@example.com",
		Folder:  "INBOX",
		ImapUID: 100,
	})

	id, err := s.InsertRuleHit(&RuleHitRecord{
		RuleID:    "test_rule",
		MessageID: msgID,
		Action:    "ignore",
	})
	if err != nil {
		t.Fatalf("inserting rule hit: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
}

func TestInsertClassifierCall(t *testing.T) {
	s := openTestDB(t)

	msgID, _ := s.InsertMessage(&MessageRecord{
		Account: "you@example.com",
		Folder:  "INBOX",
		ImapUID: 100,
	})

	id, err := s.InsertClassifierCall(&ClassifierCallRecord{
		MessageID:    msgID,
		Command:      "hermes run mail-triage",
		RequestJSON:  `{"schema_version":1}`,
		ResponseJSON: `{"action":"ignore"}`,
		ExitCode:     0,
		Stderr:       "",
		DurationMs:   150,
	})
	if err != nil {
		t.Fatalf("inserting classifier call: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
}

func TestSummaryQueue(t *testing.T) {
	s := openTestDB(t)

	msgID, _ := s.InsertMessage(&MessageRecord{
		Account:    "you@example.com",
		Folder:     "INBOX",
		ImapUID:    100,
		FromEmail:  "sender@example.com",
		Subject:    "Test",
	})

	s.InsertDecision(&DecisionRecord{
		MessageID: msgID,
		Action:    "daily_summary",
		Source:    "classifier",
		Reason:    "Useful but not urgent",
	})

	s.InsertSummaryItem(&SummaryItemRecord{
		MessageID: msgID,
		Summary:   "CI status update for repo-x",
	})

	items, err := s.UnsentSummaryItems()
	if err != nil {
		t.Fatalf("querying unsent items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 unsent item, got %d", len(items))
	}
	if items[0].Summary != "CI status update for repo-x" {
		t.Errorf("summary: got %q", items[0].Summary)
	}
	if items[0].FromEmail != "sender@example.com" {
		t.Errorf("from_email: got %q", items[0].FromEmail)
	}
	if items[0].Action != "daily_summary" {
		t.Errorf("action: got %q", items[0].Action)
	}

	err = s.MarkSummaryItemsSent([]int64{items[0].ID})
	if err != nil {
		t.Fatalf("marking sent: %v", err)
	}

	items, _ = s.UnsentSummaryItems()
	if len(items) != 0 {
		t.Fatalf("expected 0 unsent items after marking sent, got %d", len(items))
	}
}

func TestHighestUID(t *testing.T) {
	s := openTestDB(t)

	uid, err := s.HighestUID("you@example.com", "INBOX", 1)
	if err != nil {
		t.Fatalf("querying empty: %v", err)
	}
	if uid != 0 {
		t.Fatalf("expected 0 for empty table, got %d", uid)
	}

	s.InsertMessage(&MessageRecord{
		Account: "you@example.com", Folder: "INBOX", ImapUID: 100, UIDValidity: 1,
	})
	s.InsertMessage(&MessageRecord{
		Account: "you@example.com", Folder: "INBOX", ImapUID: 200, UIDValidity: 1,
	})
	s.InsertMessage(&MessageRecord{
		Account: "you@example.com", Folder: "INBOX", ImapUID: 150, UIDValidity: 1,
	})

	uid, err = s.HighestUID("you@example.com", "INBOX", 1)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	if uid != 200 {
		t.Fatalf("expected 200, got %d", uid)
	}

	// Different UID validity should not see these messages
	uid, _ = s.HighestUID("you@example.com", "INBOX", 2)
	if uid != 0 {
		t.Fatalf("expected 0 for different uid_validity, got %d", uid)
	}
}

func TestInsertClassifierMetadata(t *testing.T) {
	s := openTestDB(t)

	msgID, _ := s.InsertMessage(&MessageRecord{
		Account: "you@example.com", Folder: "INBOX", ImapUID: 100,
	})
	callID, _ := s.InsertClassifierCall(&ClassifierCallRecord{
		MessageID:    msgID,
		Command:      "classifier-openai",
		RequestJSON:  "{}",
		ResponseJSON: "{}",
		DurationMs:   200,
	})

	id, err := s.InsertClassifierMetadata(&ClassifierMetadataRecord{
		ClassifierCallID: callID,
		Model:            "gpt-4o-mini",
		Confidence:       0.92,
		Escalated:        false,
		RawJSON:          `{"model":"gpt-4o-mini","confidence":0.92,"escalated":false}`,
	})
	if err != nil {
		t.Fatalf("inserting metadata: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
}

func TestGetClassifierStats(t *testing.T) {
	s := openTestDB(t)

	msgID, _ := s.InsertMessage(&MessageRecord{
		Account: "you@example.com", Folder: "INBOX", ImapUID: 100,
	})
	s.InsertDecision(&DecisionRecord{
		MessageID: msgID, Action: "ignore", Source: "classifier", Reason: "test",
	})
	callID, _ := s.InsertClassifierCall(&ClassifierCallRecord{
		MessageID:    msgID,
		Command:      "classifier-openai",
		RequestJSON:  "{}",
		ResponseJSON: "{}",
		DurationMs:   200,
	})
	s.InsertClassifierMetadata(&ClassifierMetadataRecord{
		ClassifierCallID: callID,
		Model:            "gpt-4o-mini",
		Confidence:       0.95,
		Escalated:        false,
		RawJSON:          "{}",
	})

	msgID2, _ := s.InsertMessage(&MessageRecord{
		Account: "you@example.com", Folder: "INBOX", ImapUID: 101,
	})
	s.InsertDecision(&DecisionRecord{
		MessageID: msgID2, Action: "daily_summary", Source: "classifier", Reason: "test",
	})
	callID2, _ := s.InsertClassifierCall(&ClassifierCallRecord{
		MessageID:    msgID2,
		Command:      "classifier-openai",
		RequestJSON:  "{}",
		ResponseJSON: "{}",
		DurationMs:   400,
	})
	s.InsertClassifierMetadata(&ClassifierMetadataRecord{
		ClassifierCallID: callID2,
		Model:            "gpt-4o",
		Confidence:       0.85,
		Escalated:        true,
		RawJSON:          "{}",
	})

	stats, err := s.GetClassifierStats("2000-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("getting stats: %v", err)
	}

	if stats.TotalCalls != 2 {
		t.Errorf("total_calls = %d, want 2", stats.TotalCalls)
	}
	if stats.ByModel["gpt-4o-mini"] != 1 {
		t.Errorf("gpt-4o-mini count = %d", stats.ByModel["gpt-4o-mini"])
	}
	if stats.ByModel["gpt-4o"] != 1 {
		t.Errorf("gpt-4o count = %d", stats.ByModel["gpt-4o"])
	}
	if stats.EscalatedCount != 1 {
		t.Errorf("escalated = %d, want 1", stats.EscalatedCount)
	}
	if stats.AvgDurationMs != 300 {
		t.Errorf("avg_duration = %f, want 300", stats.AvgDurationMs)
	}
	if stats.ActionBreakdown["ignore"] != 1 {
		t.Errorf("ignore count = %d", stats.ActionBreakdown["ignore"])
	}
	if stats.ActionBreakdown["daily_summary"] != 1 {
		t.Errorf("daily_summary count = %d", stats.ActionBreakdown["daily_summary"])
	}
}

func TestGetClassifierStats_Empty(t *testing.T) {
	s := openTestDB(t)
	stats, err := s.GetClassifierStats("2000-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("getting stats: %v", err)
	}
	if stats.TotalCalls != 0 {
		t.Errorf("expected 0 calls, got %d", stats.TotalCalls)
	}
}

func TestInsertNotification(t *testing.T) {
	s := openTestDB(t)

	msgID, _ := s.InsertMessage(&MessageRecord{
		Account: "you@example.com",
		Folder:  "INBOX",
		ImapUID: 100,
	})

	id, err := s.InsertNotification(&NotificationRecord{
		MessageID: msgID,
		Channel:   "telegram",
		Status:    "sent",
	})
	if err != nil {
		t.Fatalf("inserting notification: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
}
