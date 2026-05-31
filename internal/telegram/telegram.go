package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	botToken   string
	chatID     string
	httpClient *http.Client
	baseURL    string
}

func NewClient(botToken, chatID string) *Client {
	return &Client{
		botToken:   botToken,
		chatID:     chatID,
		baseURL:    "https://api.telegram.org",
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type apiResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
}

func (c *Client) SendMessage(text string) error {
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL, c.botToken)

	resp, err := c.httpClient.PostForm(endpoint, url.Values{
		"chat_id":    {c.chatID},
		"text":       {text},
		"parse_mode": {"Markdown"},
	})
	if err != nil {
		return fmt.Errorf("sending telegram message: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading telegram response: %w", err)
	}

	var result apiResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parsing telegram response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("telegram API error: %s", result.Description)
	}

	return nil
}
