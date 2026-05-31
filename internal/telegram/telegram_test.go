package telegram

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendMessage_Success(t *testing.T) {
	var receivedChatID, receivedText string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		r.ParseForm()
		receivedChatID = r.FormValue("chat_id")
		receivedText = r.FormValue("text")
		json.NewEncoder(w).Encode(apiResponse{OK: true})
	}))
	defer server.Close()

	c := NewClient("fake-token", "123")
	c.baseURL = server.URL

	if err := c.SendMessage("hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedChatID != "123" {
		t.Errorf("chat_id = %q, want %q", receivedChatID, "123")
	}
	if receivedText != "hello" {
		t.Errorf("text = %q, want %q", receivedText, "hello")
	}
}

func TestSendMessage_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(apiResponse{OK: false, Description: "Unauthorized"})
	}))
	defer server.Close()

	c := NewClient("bad-token", "123")
	c.baseURL = server.URL

	err := c.SendMessage("hello")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "telegram API error: Unauthorized" {
		t.Errorf("error = %q", got)
	}
}
