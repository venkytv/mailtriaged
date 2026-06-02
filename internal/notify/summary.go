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

// Summarizer generates a summary message from a list of items.
// If nil, FormatSummary is used as the fallback.
type Summarizer func(items []store.SummaryItemRow) (string, error)

type SummaryScheduler struct {
	tg         *telegram.Client
	db         *store.Store
	sendTime   string // "HH:MM"
	location   *time.Location
	summarizer Summarizer
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

func (s *SummaryScheduler) SetSummarizer(fn Summarizer) {
	s.summarizer = fn
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

	if s.summarizer != nil {
		return s.sendWithSummarizer(items)
	}
	return s.sendWithFormatting(items)
}

func (s *SummaryScheduler) sendWithSummarizer(items []store.SummaryItemRow) error {
	text, err := s.summarizer(items)
	if err != nil {
		log.Printf("summarizer failed, falling back to formatted summary: %v", err)
		return s.sendWithFormatting(items)
	}

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

	log.Printf("daily summary: sent %d items via summarizer", len(items))
	return nil
}

func (s *SummaryScheduler) sendWithFormatting(items []store.SummaryItemRow) error {
	chunks := splitSummaryChunks(items)
	totalSent := 0
	for _, chunk := range chunks {
		text := FormatSummary(chunk)
		if err := s.tg.SendMessage(text); err != nil {
			return fmt.Errorf("sending summary (sent %d/%d items): %w", totalSent, len(items), err)
		}

		ids := make([]int64, len(chunk))
		for i, item := range chunk {
			ids[i] = item.ID
		}
		if err := s.db.MarkSummaryItemsSent(ids); err != nil {
			return fmt.Errorf("marking items sent: %w", err)
		}
		totalSent += len(chunk)
	}

	log.Printf("daily summary: sent %d items in %d messages", len(items), len(chunks))
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

const telegramMaxMessageLen = 4096

func splitSummaryChunks(items []store.SummaryItemRow) [][]store.SummaryItemRow {
	if len(items) == 0 {
		return nil
	}
	if len(FormatSummary(items)) <= telegramMaxMessageLen {
		return [][]store.SummaryItemRow{items}
	}

	var chunks [][]store.SummaryItemRow
	remaining := items
	for len(remaining) > 0 {
		hi := len(remaining)
		lo := 1
		for lo < hi {
			mid := (lo + hi + 1) / 2
			if len(FormatSummary(remaining[:mid])) <= telegramMaxMessageLen {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		chunks = append(chunks, remaining[:lo])
		remaining = remaining[lo:]
	}
	return chunks
}

type consolidatedItem struct {
	FromEmail string
	Subject   string
	Summary   string
	Count     int
}

func consolidateItems(items []store.SummaryItemRow) []consolidatedItem {
	type groupKey struct{ from, summary string }
	var keys []groupKey
	groups := make(map[groupKey]*consolidatedItem)

	for _, item := range items {
		k := groupKey{item.FromEmail, item.Summary}
		if g, ok := groups[k]; ok {
			g.Count++
		} else {
			keys = append(keys, k)
			groups[k] = &consolidatedItem{
				FromEmail: item.FromEmail,
				Subject:   item.Subject,
				Summary:   item.Summary,
				Count:     1,
			}
		}
	}

	result := make([]consolidatedItem, len(keys))
	for i, k := range keys {
		result[i] = *groups[k]
	}
	return result
}

func formatConsolidatedItem(b *strings.Builder, item consolidatedItem, labelSummary string) {
	if item.Count > 1 {
		fmt.Fprintf(b, "• %s ×%d\n  %s: %s\n",
			escapeMarkdown(item.FromEmail), item.Count,
			labelSummary, escapeMarkdown(item.Summary))
	} else {
		fmt.Fprintf(b, "• %s\n  Subject: %s\n  %s: %s\n",
			escapeMarkdown(item.FromEmail),
			escapeMarkdown(item.Subject),
			labelSummary, escapeMarkdown(item.Summary))
	}
}

func FormatSummary(items []store.SummaryItemRow) string {
	var needsReviewItems, summaryItems []store.SummaryItemRow
	for _, item := range items {
		if item.Action == "needs_review" {
			needsReviewItems = append(needsReviewItems, item)
		} else {
			summaryItems = append(summaryItems, item)
		}
	}

	var b strings.Builder
	b.WriteString("*Daily mail summary*\n")

	if len(needsReviewItems) > 0 {
		b.WriteString("\n*Needs review*\n")
		for _, item := range consolidateItems(needsReviewItems) {
			formatConsolidatedItem(&b, item, "Reason")
		}
	}

	if len(summaryItems) > 0 {
		b.WriteString("\n*Summary*\n")
		for _, item := range consolidateItems(summaryItems) {
			formatConsolidatedItem(&b, item, "Summary")
		}
	}

	return b.String()
}
