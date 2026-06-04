package email

import (
	"strings"
	"testing"
)

func TestParseEML_PlainText(t *testing.T) {
	eml := `From: GitHub <notifications@github.com>
To: you@example.com
Subject: [repo-x] Dependabot alert for openssl
Date: Sat, 31 May 2026 09:15:00 +0100
Message-ID: <dependabot-123@github.com>
List-ID: owner/repo-x
X-GitHub-Reason: security_alert
Content-Type: text/plain; charset="UTF-8"

Dependabot has detected a vulnerability.`

	msg, err := ParseEML(strings.NewReader(eml), 6000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.From.Email != "notifications@github.com" {
		t.Errorf("from email: got %q", msg.From.Email)
	}
	if msg.From.Name != "GitHub" {
		t.Errorf("from name: got %q", msg.From.Name)
	}
	if msg.From.Domain != "github.com" {
		t.Errorf("from domain: got %q", msg.From.Domain)
	}
	if msg.Subject != "[repo-x] Dependabot alert for openssl" {
		t.Errorf("subject: got %q", msg.Subject)
	}
	if msg.MessageID != "dependabot-123@github.com" {
		t.Errorf("message-id: got %q", msg.MessageID)
	}
	if msg.Headers["list-id"] != "owner/repo-x" {
		t.Errorf("list-id: got %q", msg.Headers["list-id"])
	}
	if msg.Headers["x-github-reason"] != "security_alert" {
		t.Errorf("x-github-reason: got %q", msg.Headers["x-github-reason"])
	}
	if !strings.Contains(msg.BodyExcerpt, "Dependabot") {
		t.Errorf("body excerpt: got %q", msg.BodyExcerpt)
	}
	if len(msg.To) != 1 || msg.To[0] != "you@example.com" {
		t.Errorf("to: got %v", msg.To)
	}
}

func TestParseEML_MIMEEncodedSubject(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		want    string
	}{
		{
			name:    "base64 UTF-8",
			subject: "=?UTF-8?B?SU5SIDE4OC42MiBzcGVudA==?=",
			want:    "INR 188.62 spent",
		},
		{
			name:    "quoted-printable UTF-8",
			subject: "=?UTF-8?Q?Re=3A_hello_world?=",
			want:    "Re: hello world",
		},
		{
			name:    "mixed encoded and plain",
			subject: "Reservation reminder =?UTF-8?B?4oCT?= 6 June 2026",
			want:    "Reservation reminder – 6 June 2026",
		},
		{
			name:    "plain ASCII unchanged",
			subject: "Weekly digest",
			want:    "Weekly digest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eml := "From: a@b.com\nTo: c@d.com\nSubject: " + tt.subject + "\nContent-Type: text/plain\n\nbody"
			msg, err := ParseEML(strings.NewReader(eml), 6000)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if msg.Subject != tt.want {
				t.Errorf("got %q, want %q", msg.Subject, tt.want)
			}
		})
	}
}

func TestParseEML_HTMLOnly(t *testing.T) {
	eml := `From: Newsletter <news@example.org>
To: you@example.com
Subject: Weekly digest
Content-Type: text/html; charset="UTF-8"

<html><body><h1>Title</h1><p>Hello world</p><script>alert(1);</script></body></html>`

	msg, err := ParseEML(strings.NewReader(eml), 6000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(msg.BodyExcerpt, "Hello world") {
		t.Errorf("expected extracted text, got %q", msg.BodyExcerpt)
	}
	if strings.Contains(msg.BodyExcerpt, "alert(1)") {
		t.Error("script content should be excluded")
	}
}

func TestParseEML_BodyTruncation(t *testing.T) {
	body := strings.Repeat("x", 1000)
	eml := "From: a@b.com\nTo: c@d.com\nSubject: test\nContent-Type: text/plain\n\n" + body

	msg, err := ParseEML(strings.NewReader(eml), 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(msg.BodyExcerpt) != 100 {
		t.Errorf("expected 100 chars, got %d", len(msg.BodyExcerpt))
	}
}

func TestParseEML_MultipleTo(t *testing.T) {
	eml := `From: a@b.com
To: x@example.com, y@example.com
Cc: z@example.com
Subject: test
Content-Type: text/plain

body`

	msg, err := ParseEML(strings.NewReader(eml), 6000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(msg.To) != 2 {
		t.Errorf("expected 2 To addresses, got %d", len(msg.To))
	}
	if len(msg.Cc) != 1 {
		t.Errorf("expected 1 Cc address, got %d", len(msg.Cc))
	}
}

func TestParseEML_NoContentType(t *testing.T) {
	eml := `From: a@b.com
To: c@d.com
Subject: test

plain text body`

	msg, err := ParseEML(strings.NewReader(eml), 6000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.BodyExcerpt != "plain text body" {
		t.Errorf("expected plain text body, got %q", msg.BodyExcerpt)
	}
}
