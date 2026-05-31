package imap

import (
	"crypto/tls"
	"fmt"

	goimap "github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
	idle "github.com/emersion/go-imap-idle"
	"github.com/venky/mailtriaged/internal/email"
)

type Client struct {
	c           *imapclient.Client
	account     string
	folder      string
	uidValidity uint32
	uidNext     uint32
}

func Dial(host string, port int, username, password string) (*Client, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	c, err := imapclient.DialTLS(addr, &tls.Config{})
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}

	if err := c.Login(username, password); err != nil {
		c.Logout()
		return nil, fmt.Errorf("login: %w", err)
	}

	return &Client{c: c, account: username}, nil
}

func (cl *Client) Select(folder string) (uidValidity, uidNext uint32, err error) {
	mbox, err := cl.c.Select(folder, false)
	if err != nil {
		return 0, 0, fmt.Errorf("selecting %s: %w", folder, err)
	}
	cl.folder = folder
	cl.uidValidity = mbox.UidValidity
	cl.uidNext = mbox.UidNext
	return mbox.UidValidity, mbox.UidNext, nil
}

func (cl *Client) SearchUIDsAbove(minUID uint32) ([]uint32, error) {
	seqSet := new(goimap.SeqSet)
	seqSet.AddRange(minUID, 0)

	criteria := &goimap.SearchCriteria{
		Uid: seqSet,
	}

	uids, err := cl.c.UidSearch(criteria)
	if err != nil {
		return nil, fmt.Errorf("searching UIDs >= %d: %w", minUID, err)
	}
	return uids, nil
}

func (cl *Client) FetchMessage(uid uint32, maxBodyChars int) (*email.Message, error) {
	section := &goimap.BodySectionName{Peek: true}
	items := []goimap.FetchItem{section.FetchItem(), goimap.FetchUid}

	seqSet := new(goimap.SeqSet)
	seqSet.AddNum(uid)

	messages := make(chan *goimap.Message, 1)
	done := make(chan error, 1)
	go func() {
		done <- cl.c.UidFetch(seqSet, items, messages)
	}()

	msg := <-messages
	if err := <-done; err != nil {
		return nil, fmt.Errorf("fetching UID %d: %w", uid, err)
	}
	if msg == nil {
		return nil, fmt.Errorf("UID %d not found", uid)
	}

	respSection := &goimap.BodySectionName{}
	body := msg.GetBody(respSection)
	if body == nil {
		return nil, fmt.Errorf("UID %d: no body", uid)
	}

	parsed, err := email.ParseEML(body, maxBodyChars)
	if err != nil {
		return nil, fmt.Errorf("parsing UID %d: %w", uid, err)
	}

	parsed.Account = cl.account
	parsed.Folder = cl.folder
	parsed.ImapUID = msg.Uid

	return parsed, nil
}

// Idle enters IMAP IDLE and blocks until stop is closed or a new message arrives.
// It calls onNewMail when a mailbox update is received.
// Returns nil when stopped, or an error on connection failure.
func (cl *Client) Idle(stop <-chan struct{}, onNewMail func()) error {
	updates := make(chan imapclient.Update, 16)
	cl.c.Updates = updates

	idleClient := idle.NewClient(cl.c)

	done := make(chan error, 1)
	go func() {
		done <- idleClient.IdleWithFallback(stop, 0)
	}()

	for {
		select {
		case update := <-updates:
			if _, ok := update.(*imapclient.MailboxUpdate); ok {
				onNewMail()
			}
		case err := <-done:
			cl.c.Updates = nil
			return err
		}
	}
}

func (cl *Client) UIDValidity() uint32 { return cl.uidValidity }
func (cl *Client) UIDNext() uint32     { return cl.uidNext }
func (cl *Client) Account() string     { return cl.account }

func (cl *Client) Close() error {
	return cl.c.Logout()
}
