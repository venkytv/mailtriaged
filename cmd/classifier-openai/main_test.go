package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCallOpenAI(t *testing.T) {
	toolArgs := `{
		"action": "ignore",
		"reason": "Recurring dependency alert",
		"summary": "",
		"suggested_rule": {
			"id_hint": "github_dependabot_repo_x",
			"description": "Ignore Dependabot alerts for repo-x",
			"action": "ignore",
			"safety": "narrow",
			"match": {
				"from_email": "notifications@github.com",
				"list_id": "owner/repo-x",
				"subject_contains_all": ["dependabot", "alert"]
			}
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected auth header, got %q", r.Header.Get("Authorization"))
		}

		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("parsing request: %v", err)
		}

		if req.Model != "gpt-4o-mini" {
			t.Errorf("expected model gpt-4o-mini, got %s", req.Model)
		}
		if len(req.Messages) != 2 {
			t.Errorf("expected 2 messages, got %d", len(req.Messages))
		}
		if len(req.Tools) != 1 {
			t.Errorf("expected 1 tool, got %d", len(req.Tools))
		}

		resp := chatResponse{
			Choices: []chatChoice{{
				Message: chatResponseMsg{
					ToolCalls: []toolCall{{
						Function: toolCallFunction{
							Name:      "classify_email",
							Arguments: toolArgs,
						},
					}},
				},
			}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	result, err := callOpenAI("test-key", "gpt-4o-mini", server.URL, "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("callOpenAI: %v", err)
	}

	if result.Action != "ignore" {
		t.Errorf("action = %q, want ignore", result.Action)
	}
	if result.Reason != "Recurring dependency alert" {
		t.Errorf("reason = %q", result.Reason)
	}
	if result.SuggestedRule == nil {
		t.Fatal("expected suggested_rule")
	}
	if result.SuggestedRule.Safety != "narrow" {
		t.Errorf("safety = %q, want narrow", result.SuggestedRule.Safety)
	}
	if result.SuggestedRule.Match.FromEmail != "notifications@github.com" {
		t.Errorf("from_email = %q", result.SuggestedRule.Match.FromEmail)
	}
}

func TestToResponse(t *testing.T) {
	summary := "Bank declined a transaction"
	result := &classifyResult{
		Action:  "alert_now",
		Reason:  "Requires immediate attention",
		Summary: summary,
	}

	resp := toResponse(result)

	if resp.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", resp.SchemaVersion)
	}
	if resp.Action != "alert_now" {
		t.Errorf("action = %q", resp.Action)
	}
	if resp.Summary == nil || *resp.Summary != summary {
		t.Errorf("summary = %v", resp.Summary)
	}
	if resp.SuggestedRule != nil {
		t.Error("expected nil suggested_rule")
	}

	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var parsed response
	json.Unmarshal(out, &parsed)
	if parsed.SchemaVersion != 1 {
		t.Error("roundtrip lost schema_version")
	}
}

func TestToResponse_NoSummary(t *testing.T) {
	result := &classifyResult{
		Action: "ignore",
		Reason: "Noise",
	}

	resp := toResponse(result)
	if resp.Summary != nil {
		t.Errorf("expected nil summary for ignore action, got %v", resp.Summary)
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	req := request{
		Instruction:  "Classify this email.",
		ValidActions: []string{"alert_now", "daily_summary", "ignore", "needs_review"},
		RuleCapabilities: ruleCapabilities{
			SupportedMatchFields: []string{"from_email", "from_domain"},
			RegexSupported:       false,
		},
	}

	prompt := buildSystemPrompt(req)
	if prompt == "" {
		t.Fatal("empty system prompt")
	}
	for _, want := range []string{"Classify this email.", "alert_now", "from_email", "regex is NOT supported"} {
		if !contains(prompt, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}

func TestBuildUserPrompt(t *testing.T) {
	msg := requestMessage{
		From:        address{Name: "GitHub", Email: "noreply@github.com", Domain: "github.com"},
		To:          []string{"user@example.com"},
		Subject:     "Test subject",
		ReceivedAt:  "2026-05-31T09:00:00Z",
		BodyExcerpt: "Hello world",
		Headers:     map[string]string{"list-id": "test-list"},
	}

	prompt := buildUserPrompt(msg)
	for _, want := range []string{"noreply@github.com", "Test subject", "Hello world", "list-id: test-list"} {
		if !contains(prompt, want) {
			t.Errorf("user prompt missing %q", want)
		}
	}
}

func TestCallOpenAI_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()

	_, err := callOpenAI("bad-key", "gpt-4o-mini", server.URL, "sys", "usr")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !contains(err.Error(), "401") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestToolSchema(t *testing.T) {
	schema := classifyToolSchema()
	if schema.Type != "function" {
		t.Errorf("type = %q, want function", schema.Type)
	}
	if schema.Function.Name != "classify_email" {
		t.Errorf("name = %q", schema.Function.Name)
	}

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshaling tool schema: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty schema JSON")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
