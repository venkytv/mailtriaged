package classify

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/venky/mailtriaged/internal/classifier"
	"github.com/venky/mailtriaged/internal/config"
	"github.com/venky/mailtriaged/internal/email"
	"github.com/venky/mailtriaged/internal/rules"
)

type Result struct {
	Action    rules.Action `json:"action"`
	Source    string       `json:"source"`
	RuleID    string       `json:"rule_id,omitempty"`
	Reason    string       `json:"reason"`
	Summary   *string      `json:"summary,omitempty"`
	Classifier *ClassifierInfo `json:"classifier,omitempty"`
}

type ClassifierInfo struct {
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
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

	return invokeClassifier(cfg, rulesDir, msg)
}

func invokeClassifier(cfg *config.Config, rulesDir string, msg *email.Message) (*Result, error) {
	req := classifier.BuildRequest(msg)

	resp, record := classifier.Execute(cfg.Classifier.Command, req, cfg.Classifier.TimeoutSeconds)

	if record.Err != nil {
		log.Printf("classifier failed: %v (stderr: %s)", record.Err, record.Stderr)
		return &Result{
			Action: rules.ActionNeedsReview,
			Source: "classifier",
			Reason: fmt.Sprintf("Classifier failed: %v", record.Err),
			Classifier: &ClassifierInfo{
				DurationMs: record.DurationMs,
				Error:      record.Err.Error(),
			},
		}, nil
	}

	if resp.SuggestedRule != nil {
		candidatesPath := filepath.Join(rulesDir, "800-llm-candidates.yaml")
		if err := classifier.AppendCandidate(candidatesPath, resp.SuggestedRule, msg.MessageID, resp.Reason); err != nil {
			log.Printf("failed to append candidate rule: %v", err)
		}
	}

	return &Result{
		Action:  resp.Action,
		Source:  "classifier",
		Reason:  resp.Reason,
		Summary: resp.Summary,
		Classifier: &ClassifierInfo{
			DurationMs: record.DurationMs,
		},
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
