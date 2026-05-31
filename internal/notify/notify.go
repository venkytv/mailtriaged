package notify

import (
	"fmt"
	"log"
	"strings"

	"github.com/venky/mailtriaged/internal/email"
	"github.com/venky/mailtriaged/internal/rules"
	"github.com/venky/mailtriaged/internal/store"
	"github.com/venky/mailtriaged/internal/telegram"
)

type Notifier struct {
	tg *telegram.Client
	db *store.Store
}

func NewNotifier(tg *telegram.Client, db *store.Store) *Notifier {
	return &Notifier{tg: tg, db: db}
}

type Decision struct {
	Action  rules.Action
	Reason  string
	Summary *string
}

// HandleAction dispatches a classification result to the appropriate notification channel.
// msgDBID is the database ID of the message (from store.InsertMessage).
func (n *Notifier) HandleAction(msg *email.Message, msgDBID int64, d *Decision) {
	switch d.Action {
	case rules.ActionAlertNow:
		n.sendAlertNow(msg, msgDBID, d)
	case rules.ActionDailySummary, rules.ActionNeedsReview:
		// Already queued as summary_item by the classify layer; nothing else needed.
	case rules.ActionIgnore:
		// No notification.
	}
}

func (n *Notifier) sendAlertNow(msg *email.Message, msgDBID int64, d *Decision) {
	text := formatAlert(msg, d)

	err := n.tg.SendMessage(text)

	status := "sent"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
		log.Printf("telegram alert failed: %v", err)
	}

	if n.db != nil {
		n.db.InsertNotification(&store.NotificationRecord{
			MessageID: msgDBID,
			Channel:   "telegram",
			Status:    status,
			Error:     errMsg,
		})
	}
}

func formatAlert(msg *email.Message, d *Decision) string {
	var b strings.Builder
	b.WriteString("*Mail Alert*\n\n")
	fmt.Fprintf(&b, "*From:* %s\n", escapeMarkdown(msg.From.Email))
	fmt.Fprintf(&b, "*Subject:* %s\n", escapeMarkdown(msg.Subject))
	fmt.Fprintf(&b, "*Reason:* %s\n", escapeMarkdown(d.Reason))
	if d.Summary != nil && *d.Summary != "" {
		fmt.Fprintf(&b, "*Summary:* %s\n", escapeMarkdown(*d.Summary))
	}
	return b.String()
}

func escapeMarkdown(s string) string {
	r := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"`", "\\`",
	)
	return r.Replace(s)
}
