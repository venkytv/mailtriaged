package imap

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/venky/mailtriaged/internal/classify"
	"github.com/venky/mailtriaged/internal/config"
	"github.com/venky/mailtriaged/internal/store"
)

type Watcher struct {
	cfg      *config.Config
	folder   string
	rulesDir string
	db       *store.Store

	mu          sync.Mutex
	lastUID     uint32
	uidValidity uint32
}

func NewWatcher(cfg *config.Config, folder, rulesDir string, db *store.Store) *Watcher {
	return &Watcher{
		cfg:      cfg,
		folder:   folder,
		rulesDir: rulesDir,
		db:       db,
	}
}

// Run is the main loop for a single folder. It connects, fetches new messages,
// enters IDLE, and reconnects on failure with exponential backoff.
func (w *Watcher) Run(ctx context.Context) error {
	backoff := w.cfg.Runtime.ReconnectBackoffSeconds
	attempt := 0

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := w.runOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			delay := backoffDelay(backoff, attempt)
			log.Printf("[%s] connection error: %v; reconnecting in %ds", w.folder, err, delay)
			attempt++

			select {
			case <-time.After(time.Duration(delay) * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		} else {
			attempt = 0
		}
	}
}

func (w *Watcher) runOnce(ctx context.Context) error {
	log.Printf("[%s] connecting to %s:%d", w.folder, w.cfg.IMAP.Host, w.cfg.IMAP.Port)

	cl, err := Dial(w.cfg.IMAP.Host, w.cfg.IMAP.Port, w.cfg.IMAP.Username, w.cfg.IMAP.Password)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer cl.Close()

	uidValidity, uidNext, err := cl.Select(w.folder)
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}

	log.Printf("[%s] selected (uidvalidity=%d uidnext=%d)", w.folder, uidValidity, uidNext)

	w.mu.Lock()
	if w.uidValidity != uidValidity {
		// UIDVALIDITY changed — reset tracked UID so we don't skip messages
		// whose UIDs are reused from the old validity epoch.
		w.lastUID = 0
		w.uidValidity = uidValidity
	}
	startUID := w.lastUID
	w.mu.Unlock()

	if startUID == 0 {
		// First run or UIDVALIDITY reset: check the store for the highest UID
		// we've already processed for this account+folder+uidValidity.
		highUID, err := w.db.HighestUID(cl.Account(), w.folder, uidValidity)
		if err != nil {
			log.Printf("[%s] error looking up highest UID: %v", w.folder, err)
		}
		if highUID > 0 {
			startUID = highUID
		} else {
			// Truly first run: start from uidNext so we don't reprocess old mail
			startUID = uidNext
		}
		w.mu.Lock()
		w.lastUID = startUID
		w.mu.Unlock()
	}

	// Fetch any messages that arrived while we were offline
	if err := w.fetchAndClassify(ctx, cl, startUID+1); err != nil {
		return fmt.Errorf("initial fetch: %w", err)
	}

	// Enter IDLE loop
	for {
		if ctx.Err() != nil {
			return nil
		}

		newMail := make(chan struct{}, 1)
		stop := make(chan struct{})

		go func() {
			select {
			case <-ctx.Done():
				close(stop)
			case <-newMail:
				close(stop)
			}
		}()

		log.Printf("[%s] entering IDLE", w.folder)
		err := cl.Idle(stop, func() {
			select {
			case newMail <- struct{}{}:
			default:
			}
		})
		if err != nil {
			return fmt.Errorf("idle: %w", err)
		}

		if ctx.Err() != nil {
			return nil
		}

		w.mu.Lock()
		searchFrom := w.lastUID + 1
		w.mu.Unlock()

		if err := w.fetchAndClassify(ctx, cl, searchFrom); err != nil {
			return fmt.Errorf("fetch after idle: %w", err)
		}
	}
}

func (w *Watcher) fetchAndClassify(ctx context.Context, cl *Client, minUID uint32) error {
	uids, err := cl.SearchUIDsAbove(minUID)
	if err != nil {
		return err
	}

	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })

	for _, uid := range uids {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		seen, err := w.db.IsMessageSeen(cl.Account(), w.folder, uid, cl.UIDValidity())
		if err != nil {
			log.Printf("[%s] error checking seen status for UID %d: %v", w.folder, uid, err)
			continue
		}
		if seen {
			w.mu.Lock()
			if uid > w.lastUID {
				w.lastUID = uid
			}
			w.mu.Unlock()
			continue
		}

		msg, err := cl.FetchMessage(uid, w.cfg.Classifier.MaxBodyExcerptChars)
		if err != nil {
			log.Printf("[%s] error fetching UID %d: %v", w.folder, uid, err)
			continue
		}

		result, err := classify.ClassifyMessage(w.cfg, w.rulesDir, msg, false, w.db)
		if err != nil {
			log.Printf("[%s] error classifying UID %d: %v", w.folder, uid, err)
		} else {
			log.Printf("[%s] UID %d: action=%s source=%s from=%s subject=%q",
				w.folder, uid, result.Action, result.Source, msg.From.Email, msg.Subject)
		}

		w.mu.Lock()
		if uid > w.lastUID {
			w.lastUID = uid
		}
		w.mu.Unlock()
	}

	return nil
}

func backoffDelay(backoff []int, attempt int) int {
	if attempt >= len(backoff) {
		return backoff[len(backoff)-1]
	}
	return backoff[attempt]
}
