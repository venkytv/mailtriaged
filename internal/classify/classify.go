package classify

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/venky/mailtriaged/internal/config"
	"github.com/venky/mailtriaged/internal/email"
	"github.com/venky/mailtriaged/internal/rules"
)

type Result struct {
	Action rules.Action `json:"action"`
	Source string       `json:"source"`
	RuleID string       `json:"rule_id,omitempty"`
	Reason string       `json:"reason"`
}

func ClassifyFile(cfg *config.Config, rulesDir string, emlPath string, skipClassifier bool) (*Result, error) {
	f, err := os.Open(emlPath)
	if err != nil {
		return nil, fmt.Errorf("opening eml: %w", err)
	}
	defer f.Close()

	msg, err := email.ParseEML(f, cfg.Classifier.MaxBodyExcerptChars)
	if err != nil {
		return nil, fmt.Errorf("parsing eml: %w", err)
	}

	ruleList, err := rules.LoadDir(rulesDir)
	if err != nil {
		return nil, fmt.Errorf("loading rules: %w", err)
	}

	if err := rules.Validate(ruleList); err != nil {
		return nil, fmt.Errorf("invalid rules: %w", err)
	}

	msgData := ToMessageData(msg)

	if !cfg.Runtime.DisableRules {
		if decision := rules.Evaluate(ruleList, msgData); decision != nil {
			return &Result{
				Action: decision.Action,
				Source: decision.Source,
				RuleID: decision.RuleID,
				Reason: decision.Reason,
			}, nil
		}
	}

	if skipClassifier {
		return &Result{
			Action: rules.ActionNeedsReview,
			Source: "none",
			Reason: "No rule matched; classifier skipped (dry-run)",
		}, nil
	}

	return &Result{
		Action: rules.ActionNeedsReview,
		Source: "none",
		Reason: "No rule matched; classifier not yet implemented",
	}, nil
}

func ToMessageData(msg *email.Message) rules.MessageData {
	listID := msg.Headers["list-id"]

	return rules.MessageData{
		FromEmail:  msg.From.Email,
		FromDomain: msg.From.Domain,
		To:         msg.To,
		Cc:         msg.Cc,
		Subject:    msg.Subject,
		ListID:     listID,
		Headers:    msg.Headers,
	}
}

func PrintJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
