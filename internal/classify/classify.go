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
	"github.com/venky/mailtriaged/internal/store"
)

type Result struct {
	Action     rules.Action    `json:"action"`
	Source     string          `json:"source"`
	RuleID     string          `json:"rule_id,omitempty"`
	Reason     string          `json:"reason"`
	Summary    *string         `json:"summary,omitempty"`
	Classifier *ClassifierInfo `json:"classifier,omitempty"`
	MsgDBID    int64           `json:"-"`
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

	return ClassifyMessage(cfg, rulesDir, msg, skipClassifier, nil)
}

func ClassifyMessage(cfg *config.Config, rulesDir string, msg *email.Message, skipClassifier bool, db *store.Store) (*Result, error) {
	ruleList, err := rules.LoadDir(rulesDir)
	if err != nil {
		return nil, fmt.Errorf("loading rules: %w", err)
	}

	if err := rules.Validate(ruleList); err != nil {
		return nil, fmt.Errorf("invalid rules: %w", err)
	}

	var msgID int64
	if db != nil {
		msgID, err = db.InsertMessage(&store.MessageRecord{
			Account:    msg.Account,
			Folder:     msg.Folder,
			ImapUID:    msg.ImapUID,
			MessageID:  msg.MessageID,
			FromEmail:  msg.From.Email,
			FromDomain: msg.From.Domain,
			Subject:    msg.Subject,
			ReceivedAt: msg.ReceivedAt,
		})
		if err != nil {
			return nil, fmt.Errorf("storing message: %w", err)
		}
	}

	msgData := ToMessageData(msg)

	if !cfg.Runtime.DisableRules {
		if decision := rules.Evaluate(ruleList, msgData); decision != nil {
			result := &Result{
				Action:  decision.Action,
				Source:  decision.Source,
				RuleID:  decision.RuleID,
				Reason:  decision.Reason,
				MsgDBID: msgID,
			}
			if db != nil {
				logDecision(db, msgID, result)
				db.InsertRuleHit(&store.RuleHitRecord{
					RuleID:    decision.RuleID,
					MessageID: msgID,
					Action:    string(decision.Action),
				})
			}
			return result, nil
		}
	}

	if skipClassifier {
		result := &Result{
			Action:  rules.ActionNeedsReview,
			Source:  "none",
			Reason:  "No rule matched; classifier skipped (dry-run)",
			MsgDBID: msgID,
		}
		if db != nil {
			logDecision(db, msgID, result)
		}
		return result, nil
	}

	return invokeClassifier(cfg, rulesDir, msg, db, msgID)
}

func invokeClassifier(cfg *config.Config, rulesDir string, msg *email.Message, db *store.Store, msgID int64) (*Result, error) {
	req := classifier.BuildRequest(msg, cfg.Classifier.Instruction)

	resp, record := classifier.Execute(cfg.Classifier.Command, req, cfg.Classifier.TimeoutSeconds)

	var callID int64
	if db != nil {
		callID = logClassifierCall(db, msgID, record)
	}

	if record.Err != nil {
		log.Printf("classifier failed: %v (stderr: %s)", record.Err, record.Stderr)
		result := &Result{
			Action:  rules.ActionNeedsReview,
			Source:  "classifier",
			Reason:  fmt.Sprintf("Classifier failed: %v", record.Err),
			MsgDBID: msgID,
			Classifier: &ClassifierInfo{
				DurationMs: record.DurationMs,
				Error:      record.Err.Error(),
			},
		}
		if db != nil {
			logDecision(db, msgID, result)
			db.InsertSummaryItem(&store.SummaryItemRecord{
				MessageID: msgID,
				Summary:   fmt.Sprintf("Classifier failed: %v", record.Err),
			})
		}
		return result, nil
	}

	if db != nil && callID > 0 && resp.Metadata != nil {
		logClassifierMetadata(db, callID, resp.Metadata)
	}

	if resp.SuggestedRule != nil {
		ruleList, _ := rules.LoadDir(rulesDir)
		sm := resp.SuggestedRule.Match
		if rules.HasSenderRule(ruleList, sm.FromEmail, sm.FromDomain, resp.Action) {
			log.Printf("skipping candidate: active rule already covers sender %q with action %s", sm.FromEmail, resp.Action)
		} else {
			candidatesPath := filepath.Join(rulesDir, "800-llm-candidates.yaml")
			if err := classifier.AppendCandidate(candidatesPath, resp.SuggestedRule, msg.MessageID, resp.Reason); err != nil {
				log.Printf("failed to append candidate rule: %v", err)
			}
		}
	}

	result := &Result{
		Action:  resp.Action,
		Source:  "classifier",
		Reason:  resp.Reason,
		Summary: resp.Summary,
		MsgDBID: msgID,
		Classifier: &ClassifierInfo{
			DurationMs: record.DurationMs,
		},
	}

	if db != nil {
		logDecision(db, msgID, result)
		if resp.Action == rules.ActionDailySummary || resp.Action == rules.ActionNeedsReview {
			summary := resp.Reason
			if resp.Summary != nil {
				summary = *resp.Summary
			}
			db.InsertSummaryItem(&store.SummaryItemRecord{
				MessageID: msgID,
				Summary:   summary,
			})
		}
	}

	return result, nil
}

func logDecision(db *store.Store, msgID int64, result *Result) {
	if _, err := db.InsertDecision(&store.DecisionRecord{
		MessageID: msgID,
		Action:    string(result.Action),
		Source:    result.Source,
		RuleID:   result.RuleID,
		Reason:   result.Reason,
	}); err != nil {
		log.Printf("failed to log decision: %v", err)
	}
}

func logClassifierCall(db *store.Store, msgID int64, record *classifier.CallRecord) int64 {
	responseJSON := string(record.ResponseJSON)
	id, err := db.InsertClassifierCall(&store.ClassifierCallRecord{
		MessageID:    msgID,
		Command:      record.Command,
		RequestJSON:  string(record.RequestJSON),
		ResponseJSON: responseJSON,
		ExitCode:     record.ExitCode,
		Stderr:       record.Stderr,
		DurationMs:   record.DurationMs,
	})
	if err != nil {
		log.Printf("failed to log classifier call: %v", err)
	}
	return id
}

func logClassifierMetadata(db *store.Store, callID int64, meta *classifier.ResponseMetadata) {
	if _, err := db.InsertClassifierMetadata(&store.ClassifierMetadataRecord{
		ClassifierCallID: callID,
		Model:            meta.Model,
		Confidence:       meta.Confidence,
		Escalated:        meta.Escalated,
	}); err != nil {
		log.Printf("failed to log classifier metadata: %v", err)
	}
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
