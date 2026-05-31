// classifier-openai is a standalone classifier CLI for mailtriaged.
// It reads a mailtriaged classifier request from stdin, calls the OpenAI API
// using tool/function calling to enforce the response schema, and writes the
// classifier response to stdout.
//
// Supports tiered classification: a cheap model handles obvious emails, and
// a more capable fallback model is called when the primary model reports low
// confidence.
//
// Usage:
//
//	echo '{"schema_version":1,...}' | classifier-openai
//	echo '{"schema_version":1,...}' | classifier-openai --model gpt-4o-mini --fallback-model gpt-4o
//
// In mailtriaged config.yaml:
//
//	classifier:
//	  command: ["classifier-openai", "--model", "gpt-4o-mini", "--fallback-model", "gpt-4o"]
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// --- mailtriaged stdin types ---

type request struct {
	SchemaVersion    int              `json:"schema_version"`
	Message          requestMessage   `json:"message"`
	ValidActions     []string         `json:"valid_actions"`
	RuleCapabilities ruleCapabilities `json:"rule_capabilities"`
	Instruction      string           `json:"instruction"`
}

type requestMessage struct {
	Account     string            `json:"account"`
	Folder      string            `json:"folder"`
	ImapUID     uint32            `json:"imap_uid"`
	MessageID   string            `json:"message_id"`
	From        address           `json:"from"`
	To          []string          `json:"to"`
	Cc          []string          `json:"cc"`
	Subject     string            `json:"subject"`
	ReceivedAt  string            `json:"received_at"`
	Headers     map[string]string `json:"headers"`
	BodyExcerpt string            `json:"body_excerpt"`
}

type address struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	Domain string `json:"domain"`
}

type ruleCapabilities struct {
	SupportedMatchFields []string `json:"supported_match_fields"`
	RegexSupported       bool     `json:"regex_supported"`
}

// --- mailtriaged stdout types ---

type response struct {
	SchemaVersion int            `json:"schema_version"`
	Action        string         `json:"action"`
	Reason        string         `json:"reason"`
	Summary       *string        `json:"summary"`
	SuggestedRule *suggestedRule `json:"suggested_rule"`
}

type suggestedRule struct {
	IDHint      string `json:"id_hint"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Safety      string `json:"safety"`
	Match       match  `json:"match"`
}

type match struct {
	FromEmail          string            `json:"from_email,omitempty"`
	FromDomain         string            `json:"from_domain,omitempty"`
	ToContains         string            `json:"to_contains,omitempty"`
	CcContains         string            `json:"cc_contains,omitempty"`
	ListID             string            `json:"list_id,omitempty"`
	SubjectContainsAll []string          `json:"subject_contains_all,omitempty"`
	SubjectContainsAny []string          `json:"subject_contains_any,omitempty"`
	HeaderEquals       map[string]string `json:"header_equals,omitempty"`
	HeaderContains     map[string]string `json:"header_contains,omitempty"`
}

// --- OpenAI API types ---

type chatRequest struct {
	Model      string       `json:"model"`
	Messages   []chatMsg    `json:"messages"`
	Tools      []toolDef    `json:"tools"`
	ToolChoice any          `json:"tool_choice"`
}

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type toolDef struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

type functionDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Error   *apiError    `json:"error,omitempty"`
}

type chatChoice struct {
	Message chatResponseMsg `json:"message"`
}

type chatResponseMsg struct {
	ToolCalls []toolCall `json:"tool_calls"`
}

type toolCall struct {
	Function toolCallFunction `json:"function"`
}

type toolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type apiError struct {
	Message string `json:"message"`
}

// --- tool call result (parsed from function arguments) ---

type classifyResult struct {
	Action        string         `json:"action"`
	Reason        string         `json:"reason"`
	Summary       string         `json:"summary,omitempty"`
	Confidence    float64        `json:"confidence"`
	SuggestedRule *suggestedRule `json:"suggested_rule,omitempty"`
}

func main() {
	model := flag.String("model", "gpt-4o-mini", "OpenAI model")
	fallbackModel := flag.String("fallback-model", "", "more capable model to use when primary confidence is low")
	confidenceThreshold := flag.Float64("confidence-threshold", 0.7, "confidence below this triggers fallback model")
	apiKeyCmd := flag.String("api-key-command", "", "shell command to retrieve API key (alternative to OPENAI_API_KEY env)")
	baseURL := flag.String("base-url", "https://api.openai.com/v1", "OpenAI-compatible API base URL")
	verbose := flag.Bool("verbose", false, "print debug info to stderr")
	flag.Parse()

	apiKey, err := resolveAPIKey(*apiKeyCmd)
	if err != nil {
		fatal("resolving API key: %v", err)
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fatal("reading stdin: %v", err)
	}

	var req request
	if err := json.Unmarshal(input, &req); err != nil {
		fatal("parsing request: %v", err)
	}

	systemPrompt := buildSystemPrompt(req)
	userPrompt := buildUserPrompt(req.Message)

	if *verbose {
		fmt.Fprintf(os.Stderr, "classifier-openai: model=%s base_url=%s\n", *model, *baseURL)
		if *fallbackModel != "" {
			fmt.Fprintf(os.Stderr, "classifier-openai: fallback_model=%s threshold=%.2f\n", *fallbackModel, *confidenceThreshold)
		}
	}

	result, err := callOpenAI(apiKey, *model, *baseURL, systemPrompt, userPrompt)
	if err != nil {
		fatal("%v", err)
	}

	usedModel := *model
	if *fallbackModel != "" && result.Confidence < *confidenceThreshold {
		if *verbose {
			fmt.Fprintf(os.Stderr, "classifier-openai: %s confidence=%.2f < threshold=%.2f, escalating to %s\n",
				*model, result.Confidence, *confidenceThreshold, *fallbackModel)
		}
		result, err = callOpenAI(apiKey, *fallbackModel, *baseURL, systemPrompt, userPrompt)
		if err != nil {
			fatal("fallback model: %v", err)
		}
		usedModel = *fallbackModel
	}

	if *verbose {
		fmt.Fprintf(os.Stderr, "classifier-openai: classified by %s confidence=%.2f action=%s\n",
			usedModel, result.Confidence, result.Action)
	}

	resp := toResponse(result)
	out, err := json.Marshal(resp)
	if err != nil {
		fatal("marshaling response: %v", err)
	}
	os.Stdout.Write(out)
	os.Stdout.Write([]byte("\n"))
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "classifier-openai: "+format+"\n", args...)
	os.Exit(1)
}

func resolveAPIKey(cmd string) (string, error) {
	if cmd != "" {
		out, err := exec.Command("sh", "-c", cmd).Output()
		if err != nil {
			return "", fmt.Errorf("running api-key-command: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	}
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return "", fmt.Errorf("set OPENAI_API_KEY or use --api-key-command")
	}
	return key, nil
}

func buildSystemPrompt(req request) string {
	var sb strings.Builder
	sb.WriteString(req.Instruction)
	sb.WriteString("\n\n")

	sb.WriteString("Valid actions: ")
	sb.WriteString(strings.Join(req.ValidActions, ", "))
	sb.WriteString("\n\n")

	sb.WriteString("Supported rule match fields: ")
	sb.WriteString(strings.Join(req.RuleCapabilities.SupportedMatchFields, ", "))
	if !req.RuleCapabilities.RegexSupported {
		sb.WriteString(" (regex is NOT supported)")
	}
	sb.WriteString("\n\n")

	sb.WriteString("Action guidelines:\n")
	sb.WriteString("- alert_now: time-sensitive, requires immediate attention (bank alerts, security warnings, urgent personal)\n")
	sb.WriteString("- daily_summary: useful but not urgent (newsletters, CI, social updates). Provide a one-line summary.\n")
	sb.WriteString("- ignore: noise, no attention needed (automated non-critical alerts, marketing)\n")
	sb.WriteString("- needs_review: uncertain — let the user decide\n\n")

	sb.WriteString("Rule suggestion guidelines:\n")
	sb.WriteString("- Suggest a rule when the email fits a recurring pattern\n")
	sb.WriteString("- Prefer narrow rules: use 2+ match fields (e.g. from_email + subject_contains_all)\n")
	sb.WriteString("- Set safety to \"narrow\" if the rule uses multiple match fields or is very specific\n")
	sb.WriteString("- Set safety to \"broad\" if the rule uses a single field like from_domain alone\n")
	sb.WriteString("- Omit suggested_rule if the email is unusual or doesn't fit a repeatable pattern\n")
	sb.WriteString("- Provide a summary for alert_now and daily_summary actions\n\n")

	sb.WriteString("Confidence guidelines:\n")
	sb.WriteString("- 0.9-1.0: obvious classification (e.g. known sender pattern, clear marketing/spam)\n")
	sb.WriteString("- 0.7-0.9: reasonably confident but some ambiguity\n")
	sb.WriteString("- 0.5-0.7: uncertain, could go either way\n")
	sb.WriteString("- below 0.5: very unsure, email is unusual or complex\n")
	return sb.String()
}

func buildUserPrompt(msg requestMessage) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "From: %s <%s>\n", msg.From.Name, msg.From.Email)
	fmt.Fprintf(&sb, "To: %s\n", strings.Join(msg.To, ", "))
	if len(msg.Cc) > 0 {
		fmt.Fprintf(&sb, "Cc: %s\n", strings.Join(msg.Cc, ", "))
	}
	fmt.Fprintf(&sb, "Subject: %s\n", msg.Subject)
	fmt.Fprintf(&sb, "Date: %s\n", msg.ReceivedAt)
	if len(msg.Headers) > 0 {
		sb.WriteString("\nHeaders:\n")
		for k, v := range msg.Headers {
			fmt.Fprintf(&sb, "  %s: %s\n", k, v)
		}
	}
	if msg.BodyExcerpt != "" {
		fmt.Fprintf(&sb, "\nBody:\n%s\n", msg.BodyExcerpt)
	}
	return sb.String()
}

func classifyToolSchema() toolDef {
	return toolDef{
		Type: "function",
		Function: functionDef{
			Name:        "classify_email",
			Description: "Classify an email and optionally suggest a reusable triage rule",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"alert_now", "daily_summary", "ignore", "needs_review"},
						"description": "The triage action for this email",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Brief explanation of why this action was chosen",
					},
					"confidence": map[string]any{
						"type":        "number",
						"description": "How confident you are in this classification, from 0.0 (uncertain) to 1.0 (obvious). Use low values (<0.7) when the email is ambiguous, unusual, or could reasonably be classified differently.",
					},
					"summary": map[string]any{
						"type":        "string",
						"description": "One-line summary for notifications. Required for alert_now and daily_summary.",
					},
					"suggested_rule": map[string]any{
						"type":        "object",
						"description": "A reusable rule to auto-handle similar emails. Omit if the email doesn't fit a repeatable pattern.",
						"properties": map[string]any{
							"id_hint": map[string]any{
								"type":        "string",
								"description": "Suggested rule ID in snake_case (e.g. github_dependabot_repo_x_ignore)",
							},
							"description": map[string]any{
								"type":        "string",
								"description": "Human-readable description of what this rule matches",
							},
							"action": map[string]any{
								"type": "string",
								"enum": []string{"alert_now", "daily_summary", "ignore", "needs_review"},
							},
							"safety": map[string]any{
								"type":        "string",
								"enum":        []string{"narrow", "broad"},
								"description": "narrow = specific rule with multiple match fields; broad = single-field rule that may over-match",
							},
							"match": map[string]any{
								"type":        "object",
								"description": "Match criteria using only supported fields. Be as specific as possible.",
								"properties": map[string]any{
									"from_email":           map[string]any{"type": "string", "description": "Exact sender email address"},
									"from_domain":          map[string]any{"type": "string", "description": "Sender domain"},
									"to_contains":          map[string]any{"type": "string", "description": "Recipient address to match"},
									"cc_contains":          map[string]any{"type": "string", "description": "CC address to match"},
									"list_id":              map[string]any{"type": "string", "description": "List-Id header value"},
									"subject_contains_all": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "All of these must appear in the subject"},
									"subject_contains_any": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "At least one of these must appear in the subject"},
									"header_equals":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Headers that must match exactly"},
									"header_contains":      map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Headers that must contain the value"},
								},
								"additionalProperties": false,
							},
						},
						"required":             []string{"id_hint", "description", "action", "safety", "match"},
						"additionalProperties": false,
					},
				},
				"required":             []string{"action", "reason", "confidence"},
				"additionalProperties": false,
			},
		},
	}
}

func callOpenAI(apiKey, model, baseURL, systemPrompt, userPrompt string) (*classifyResult, error) {
	body := chatRequest{
		Model: model,
		Messages: []chatMsg{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Tools: []toolDef{classifyToolSchema()},
		ToolChoice: map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "classify_email"},
		},
	}

	reqJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling API request: %w", err)
	}

	url := strings.TrimRight(baseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading API response: %w", err)
	}

	if httpResp.StatusCode != 200 {
		return nil, fmt.Errorf("API error (HTTP %d): %s", httpResp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("parsing API response: %w", err)
	}
	if chatResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("API returned no choices")
	}

	toolCalls := chatResp.Choices[0].Message.ToolCalls
	if len(toolCalls) == 0 {
		return nil, fmt.Errorf("API returned no tool calls")
	}

	tc := toolCalls[0]
	if tc.Function.Name != "classify_email" {
		return nil, fmt.Errorf("unexpected function call: %s", tc.Function.Name)
	}

	var result classifyResult
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &result); err != nil {
		return nil, fmt.Errorf("parsing function arguments: %w", err)
	}

	return &result, nil
}

func toResponse(r *classifyResult) *response {
	resp := &response{
		SchemaVersion: 1,
		Action:        r.Action,
		Reason:        r.Reason,
	}
	if r.Summary != "" {
		resp.Summary = &r.Summary
	}
	if r.SuggestedRule != nil {
		resp.SuggestedRule = r.SuggestedRule
	}
	return resp
}
