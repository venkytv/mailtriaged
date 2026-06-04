package email

import (
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"

	"golang.org/x/net/html"
)

type Message struct {
	Account   string            `json:"account"`
	Folder    string            `json:"folder"`
	ImapUID   uint32            `json:"imap_uid"`
	MessageID string            `json:"message_id"`
	From      Address           `json:"from"`
	To        []string          `json:"to"`
	Cc        []string          `json:"cc"`
	Subject   string            `json:"subject"`
	ReceivedAt string           `json:"received_at"`
	Headers   map[string]string `json:"headers"`
	BodyExcerpt string          `json:"body_excerpt"`
}

type Address struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	Domain string `json:"domain"`
}

func ParseEML(r io.Reader, maxBodyChars int) (*Message, error) {
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return nil, fmt.Errorf("parsing message: %w", err)
	}

	from := parseFrom(msg.Header.Get("From"))
	to := parseAddressList(msg.Header.Get("To"))
	cc := parseAddressList(msg.Header.Get("Cc"))

	headers := extractHeaders(msg.Header)

	body, err := extractBody(msg, maxBodyChars)
	if err != nil {
		body = ""
	}

	return &Message{
		MessageID:   cleanMessageID(msg.Header.Get("Message-Id")),
		From:        from,
		To:          to,
		Cc:          cc,
		Subject:     decodeHeader(msg.Header.Get("Subject")),
		ReceivedAt:  msg.Header.Get("Date"),
		Headers:     headers,
		BodyExcerpt: body,
	}, nil
}

func parseFrom(raw string) Address {
	if raw == "" {
		return Address{}
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return Address{Email: raw}
	}
	domain := ""
	if idx := strings.LastIndex(addr.Address, "@"); idx >= 0 {
		domain = addr.Address[idx+1:]
	}
	return Address{
		Name:   addr.Name,
		Email:  addr.Address,
		Domain: domain,
	}
}

func parseAddressList(raw string) []string {
	if raw == "" {
		return nil
	}
	addrs, err := mail.ParseAddressList(raw)
	if err != nil {
		return []string{raw}
	}
	result := make([]string, len(addrs))
	for i, a := range addrs {
		result[i] = a.Address
	}
	return result
}

func decodeHeader(raw string) string {
	dec := &mime.WordDecoder{}
	decoded, err := dec.DecodeHeader(raw)
	if err != nil {
		return raw
	}
	return decoded
}

func cleanMessageID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(id, "<")
	id = strings.TrimSuffix(id, ">")
	return id
}

var preservedHeaders = map[string]bool{
	"list-id":        true,
	"auto-submitted": true,
	"precedence":     true,
}

func extractHeaders(h mail.Header) map[string]string {
	result := make(map[string]string)
	for key, vals := range h {
		lower := strings.ToLower(key)
		if preservedHeaders[lower] || strings.HasPrefix(lower, "x-") {
			if len(vals) > 0 {
				result[lower] = vals[0]
			}
		}
	}
	return result
}

func extractBody(msg *mail.Message, maxChars int) (string, error) {
	contentType := msg.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return readPlainBody(msg.Body, maxChars)
	}

	if mediaType == "text/plain" {
		return readPlainBody(msg.Body, maxChars)
	}

	if mediaType == "text/html" {
		return readHTMLBody(msg.Body, maxChars)
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		return extractMultipartBody(msg.Body, params["boundary"], maxChars)
	}

	return readPlainBody(msg.Body, maxChars)
}

func extractMultipartBody(r io.Reader, boundary string, maxChars int) (string, error) {
	if boundary == "" {
		return "", fmt.Errorf("no boundary in multipart message")
	}

	mr := multipart.NewReader(r, boundary)
	var plainBody, htmlBody string

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		ct := part.Header.Get("Content-Type")
		mt, _, _ := mime.ParseMediaType(ct)

		switch mt {
		case "text/plain":
			if plainBody == "" {
				plainBody, _ = readPlainBody(part, maxChars)
			}
		case "text/html":
			if htmlBody == "" {
				htmlBody, _ = readHTMLBody(part, maxChars)
			}
		}
	}

	if plainBody != "" {
		return plainBody, nil
	}
	if htmlBody != "" {
		return htmlBody, nil
	}
	return "", nil
}

func readPlainBody(r io.Reader, maxChars int) (string, error) {
	buf := make([]byte, maxChars)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(string(buf[:n])), nil
}

func readHTMLBody(r io.Reader, maxChars int) (string, error) {
	tokenizer := html.NewTokenizer(r)
	var b strings.Builder
	inScript := false
	inStyle := false

	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}

		switch tt {
		case html.StartTagToken:
			tn, _ := tokenizer.TagName()
			tag := string(tn)
			if tag == "script" {
				inScript = true
			} else if tag == "style" {
				inStyle = true
			}
		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			tag := string(tn)
			if tag == "script" {
				inScript = false
			} else if tag == "style" {
				inStyle = false
			}
		case html.TextToken:
			if !inScript && !inStyle {
				text := strings.TrimSpace(tokenizer.Token().Data)
				if text != "" {
					if b.Len() > 0 {
						b.WriteByte(' ')
					}
					b.WriteString(text)
					if b.Len() >= maxChars {
						return b.String()[:maxChars], nil
					}
				}
			}
		}
	}

	return strings.TrimSpace(b.String()), nil
}
