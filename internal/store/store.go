package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

type MessageRecord struct {
	ID          int64
	Account     string
	Folder      string
	ImapUID     uint32
	UIDValidity uint32
	MessageID   string
	FromEmail   string
	FromDomain  string
	Subject     string
	ReceivedAt  string
	SeenAt      string
}

func (s *Store) InsertMessage(msg *MessageRecord) (int64, error) {
	if msg.SeenAt == "" {
		msg.SeenAt = time.Now().UTC().Format(time.RFC3339)
	}

	result, err := s.db.Exec(
		`INSERT INTO messages (account, folder, imap_uid, uid_validity, message_id, from_email, from_domain, subject, received_at, seen_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(account, folder, imap_uid, uid_validity) DO NOTHING`,
		msg.Account, msg.Folder, msg.ImapUID, msg.UIDValidity,
		msg.MessageID, msg.FromEmail, msg.FromDomain, msg.Subject,
		msg.ReceivedAt, msg.SeenAt,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting message: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	// LastInsertId returns 0 on conflict (no insert). Look up the existing row.
	if id == 0 {
		row := s.db.QueryRow(
			"SELECT id FROM messages WHERE account = ? AND folder = ? AND imap_uid = ? AND uid_validity = ?",
			msg.Account, msg.Folder, msg.ImapUID, msg.UIDValidity,
		)
		if err := row.Scan(&id); err != nil {
			return 0, fmt.Errorf("looking up existing message: %w", err)
		}
	}

	return id, nil
}

func (s *Store) IsMessageSeen(account, folder string, imapUID, uidValidity uint32) (bool, error) {
	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM messages WHERE account = ? AND folder = ? AND imap_uid = ? AND uid_validity = ?",
		account, folder, imapUID, uidValidity,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

type DecisionRecord struct {
	MessageID int64
	Action    string
	Source    string
	RuleID   string
	Reason   string
}

func (s *Store) InsertDecision(d *DecisionRecord) (int64, error) {
	result, err := s.db.Exec(
		`INSERT INTO decisions (message_id, action, source, rule_id, reason, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		d.MessageID, d.Action, d.Source, d.RuleID, d.Reason,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("inserting decision: %w", err)
	}
	return result.LastInsertId()
}

type RuleHitRecord struct {
	RuleID    string
	MessageID int64
	Action    string
}

func (s *Store) InsertRuleHit(h *RuleHitRecord) (int64, error) {
	result, err := s.db.Exec(
		`INSERT INTO rule_hits (rule_id, message_id, action, hit_at)
		 VALUES (?, ?, ?, ?)`,
		h.RuleID, h.MessageID, h.Action,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("inserting rule hit: %w", err)
	}
	return result.LastInsertId()
}

type ClassifierCallRecord struct {
	MessageID    int64
	Command      string
	RequestJSON  string
	ResponseJSON string
	ExitCode     int
	Stderr       string
	DurationMs   int64
}

func (s *Store) InsertClassifierCall(c *ClassifierCallRecord) (int64, error) {
	result, err := s.db.Exec(
		`INSERT INTO classifier_calls (message_id, command, request_json, response_json, exit_code, stderr, duration_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.MessageID, c.Command, c.RequestJSON, c.ResponseJSON,
		c.ExitCode, c.Stderr, c.DurationMs,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("inserting classifier call: %w", err)
	}
	return result.LastInsertId()
}

type SummaryItemRecord struct {
	MessageID int64
	Summary   string
}

func (s *Store) InsertSummaryItem(item *SummaryItemRecord) (int64, error) {
	result, err := s.db.Exec(
		`INSERT INTO summary_items (message_id, summary, created_at)
		 VALUES (?, ?, ?)`,
		item.MessageID, item.Summary,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("inserting summary item: %w", err)
	}
	return result.LastInsertId()
}

type SummaryItemRow struct {
	ID        int64
	MessageID int64
	Summary   string
	CreatedAt string
	FromEmail string
	Subject   string
	Action    string
	Reason    string
}

func (s *Store) UnsentSummaryItems() ([]SummaryItemRow, error) {
	rows, err := s.db.Query(
		`SELECT si.id, si.message_id, si.summary, si.created_at,
		        m.from_email, m.subject,
		        COALESCE(d.action, ''), COALESCE(d.reason, '')
		 FROM summary_items si
		 JOIN messages m ON m.id = si.message_id
		 LEFT JOIN decisions d ON d.message_id = si.message_id
		 WHERE si.sent = 0
		 ORDER BY si.created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying unsent summary items: %w", err)
	}
	defer rows.Close()

	var items []SummaryItemRow
	for rows.Next() {
		var item SummaryItemRow
		if err := rows.Scan(&item.ID, &item.MessageID, &item.Summary, &item.CreatedAt,
			&item.FromEmail, &item.Subject, &item.Action, &item.Reason); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MarkSummaryItemsSent(ids []int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range ids {
		if _, err := s.db.Exec(
			"UPDATE summary_items SET sent = 1, sent_at = ? WHERE id = ?",
			now, id,
		); err != nil {
			return fmt.Errorf("marking summary item %d sent: %w", id, err)
		}
	}
	return nil
}

type NotificationRecord struct {
	MessageID int64
	Channel   string
	Status    string
	Error     string
}

func (s *Store) InsertNotification(n *NotificationRecord) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	sentAt := ""
	if n.Status == "sent" {
		sentAt = now
	}

	result, err := s.db.Exec(
		`INSERT INTO notifications (message_id, channel, status, error, created_at, sent_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		n.MessageID, n.Channel, n.Status, n.Error, now, sentAt,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting notification: %w", err)
	}
	return result.LastInsertId()
}
