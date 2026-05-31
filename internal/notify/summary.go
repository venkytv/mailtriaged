package notify

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/venky/mailtriaged/internal/store"
	"github.com/venky/mailtriaged/internal/telegram"
)

type SummaryScheduler struct {
	tg       *telegram.Client
	db       *store.Store
	sendTime string // "HH:MM"
	location *time.Location
}

func NewSummaryScheduler(tg *telegram.Client, db *store.Store, sendTime, timezone string) (*SummaryScheduler, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("loading timezone %q: %w", timezone, err)
	}
	return &SummaryScheduler{
		tg:       tg,
		db:       db,
		sendTime: sendTime,
		location: loc,
	}, nil
}

// Run starts the daily summary scheduler. It blocks until ctx is cancelled.
func (s *SummaryScheduler) Run(ctx context.Context) {
	for {
		next := s.nextSendTime()
		delay := time.Until(next)
		log.Printf("next daily summary at %s (in %s)", next.Format(time.RFC3339), delay.Round(time.Second))

		select {
		case <-time.After(delay):
			if err := s.SendNow(); err != nil {
				log.Printf("daily summary send failed: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// SendNow sends the daily summary immediately.
func (s *SummaryScheduler) SendNow() error {
	items, err := s.db.UnsentSummaryItems()
	if err != nil {
		return fmt.Errorf("querying unsent items: %w", err)
	}

	if len(items) == 0 {
		log.Println("daily summary: no unsent items")
		return nil
	}

	text := FormatSummary(items)

	if err := s.tg.SendMessage(text); err != nil {
		return fmt.Errorf("sending summary: %w", err)
	}

	ids := make([]int64, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	if err := s.db.MarkSummaryItemsSent(ids); err != nil {
		return fmt.Errorf("marking items sent: %w", err)
	}

	log.Printf("daily summary: sent %d items", len(items))
	return nil
}

func (s *SummaryScheduler) nextSendTime() time.Time {
	now := time.Now().In(s.location)

	hour, min := parseSendTime(s.sendTime)
	candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, s.location)

	if candidate.Before(now) {
		candidate = candidate.Add(24 * time.Hour)
	}
	return candidate
}

func parseSendTime(s string) (hour, min int) {
	fmt.Sscanf(s, "%d:%d", &hour, &min)
	return
}

func FormatSummary(items []store.SummaryItemRow) string {
	var needsReview, summary []store.SummaryItemRow
	for _, item := range items {
		if item.Action == "needs_review" {
			needsReview = append(needsReview, item)
		} else {
			summary = append(summary, item)
		}
	}

	var b strings.Builder
	b.WriteString("*Daily mail summary*\n")

	if len(needsReview) > 0 {
		b.WriteString("\n*Needs review*\n")
		for _, item := range needsReview {
			fmt.Fprintf(&b, "• From: %s\n  Subject: %s\n  Reason: %s\n",
				escapeMarkdown(item.FromEmail),
				escapeMarkdown(item.Subject),
				escapeMarkdown(item.Summary))
		}
	}

	if len(summary) > 0 {
		b.WriteString("\n*Summary*\n")
		for _, item := range summary {
			fmt.Fprintf(&b, "• From: %s\n  Subject: %s\n  Summary: %s\n",
				escapeMarkdown(item.FromEmail),
				escapeMarkdown(item.Subject),
				escapeMarkdown(item.Summary))
		}
	}

	return b.String()
}
