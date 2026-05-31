package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/venky/mailtriaged/internal/email"
	"github.com/venky/mailtriaged/internal/rules"
)

type Request struct {
	SchemaVersion    int              `json:"schema_version"`
	Message          RequestMessage   `json:"message"`
	ValidActions     []rules.Action   `json:"valid_actions"`
	RuleCapabilities RuleCapabilities `json:"rule_capabilities"`
	Instruction      string           `json:"instruction"`
}

type RequestMessage struct {
	Account    string            `json:"account"`
	Folder     string            `json:"folder"`
	ImapUID    uint32            `json:"imap_uid"`
	MessageID  string            `json:"message_id"`
	From       email.Address     `json:"from"`
	To         []string          `json:"to"`
	Cc         []string          `json:"cc"`
	Subject    string            `json:"subject"`
	ReceivedAt string            `json:"received_at"`
	Headers    map[string]string `json:"headers"`
	BodyExcerpt string           `json:"body_excerpt"`
}

type RuleCapabilities struct {
	SupportedMatchFields []string `json:"supported_match_fields"`
	RegexSupported       bool     `json:"regex_supported"`
}

type Response struct {
	SchemaVersion int              `json:"schema_version"`
	Action        rules.Action     `json:"action"`
	Reason        string           `json:"reason"`
	Summary       *string          `json:"summary"`
	SuggestedRule *SuggestedRule   `json:"suggested_rule"`
	Metadata      *ResponseMetadata `json:"metadata,omitempty"`
}

type ResponseMetadata struct {
	Model      string  `json:"model"`
	Confidence float64 `json:"confidence"`
	Escalated  bool    `json:"escalated"`
}

type SuggestedRule struct {
	IDHint      string      `json:"id_hint"`
	Description string      `json:"description"`
	Action      rules.Action `json:"action"`
	Safety      string      `json:"safety"`
	Match       rules.Match `json:"match"`
}

type CallRecord struct {
	Command      string
	RequestJSON  []byte
	ResponseJSON []byte
	Stderr       string
	ExitCode     int
	DurationMs   int64
	Err          error
}

var supportedMatchFields = []string{
	"from_email", "from_domain", "to_contains", "cc_contains",
	"list_id", "subject_contains_all", "subject_contains_any",
	"header_equals", "header_contains",
}

var validResponseActions = map[rules.Action]bool{
	rules.ActionAlertNow:     true,
	rules.ActionDailySummary: true,
	rules.ActionIgnore:       true,
	rules.ActionNeedsReview:  true,
}

func BuildRequest(msg *email.Message) *Request {
	return &Request{
		SchemaVersion: 1,
		Message: RequestMessage{
			Account:     msg.Account,
			Folder:      msg.Folder,
			ImapUID:     msg.ImapUID,
			MessageID:   msg.MessageID,
			From:        msg.From,
			To:          msg.To,
			Cc:          msg.Cc,
			Subject:     msg.Subject,
			ReceivedAt:  msg.ReceivedAt,
			Headers:     msg.Headers,
			BodyExcerpt: msg.BodyExcerpt,
		},
		ValidActions: []rules.Action{
			rules.ActionAlertNow,
			rules.ActionDailySummary,
			rules.ActionIgnore,
			rules.ActionNeedsReview,
		},
		RuleCapabilities: RuleCapabilities{
			SupportedMatchFields: supportedMatchFields,
			RegexSupported:       false,
		},
		Instruction: "Classify this email for a single user's personal mail triage. Return strict JSON only. If you suggest a rule, keep it narrow and only use supported match fields.",
	}
}

func Execute(command []string, req *Request, timeoutSecs int) (*Response, *CallRecord) {
	record := &CallRecord{
		Command: fmt.Sprintf("%v", command),
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		record.Err = fmt.Errorf("marshaling request: %w", err)
		return nil, record
	}
	record.RequestJSON = reqJSON

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdin = bytes.NewReader(reqJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	record.DurationMs = time.Since(start).Milliseconds()
	record.Stderr = stderr.String()

	if ctx.Err() == context.DeadlineExceeded {
		record.Err = fmt.Errorf("classifier timed out after %ds", timeoutSecs)
		record.ExitCode = -1
		return nil, record
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			record.ExitCode = exitErr.ExitCode()
		} else {
			record.ExitCode = -1
		}
		record.Err = fmt.Errorf("classifier exited with error: %w", err)
		return nil, record
	}

	record.ResponseJSON = stdout.Bytes()

	var resp Response
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		record.Err = fmt.Errorf("invalid classifier JSON: %w", err)
		return nil, record
	}

	if err := validateResponse(&resp); err != nil {
		record.Err = err
		return nil, record
	}

	return &resp, record
}

func validateResponse(resp *Response) error {
	if resp.Action == "" {
		return fmt.Errorf("classifier response missing action")
	}
	if !validResponseActions[resp.Action] {
		return fmt.Errorf("classifier returned unsupported action %q", resp.Action)
	}
	if resp.SuggestedRule != nil {
		if err := validateSuggestedMatch(&resp.SuggestedRule.Match); err != nil {
			return fmt.Errorf("suggested rule: %w", err)
		}
	}
	return nil
}

func validateSuggestedMatch(m *rules.Match) error {
	if m.IsEmpty() {
		return fmt.Errorf("suggested rule match is empty")
	}
	return nil
}
