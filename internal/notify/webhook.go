package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/digitalbile/softwarr/internal/db/dbq"
)

const webhookTimeout = 10 * time.Second

var (
	ErrNotFound   = errors.New("Webhook not found.")
	errBadURL     = errors.New("Enter a full http or https URL.")
	errBadEvent   = errors.New("Unknown event type.")
	errNameNeeded = errors.New("Enter a name.")
)

// IsInputError reports whether err is a validation failure safe to show.
func IsInputError(err error) bool {
	return errors.Is(err, errBadURL) || errors.Is(err, errBadEvent) || errors.Is(err, errNameNeeded)
}

// Webhook is the API view of a webhook. The secret is never exposed.
type Webhook struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	HasSecret bool     `json:"hasSecret"`
	Events    []string `json:"events"`
	Enabled   bool     `json:"enabled"`
}

// WebhookInput is the body for creating or updating a webhook. An empty
// Secret keeps the stored one on update unless ClearSecret is set.
type WebhookInput struct {
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Secret      string   `json:"secret"`
	ClearSecret bool     `json:"clearSecret"`
	Events      []string `json:"events"`
	Enabled     bool     `json:"enabled"`
}

// TestResult is the outcome of a test delivery.
type TestResult struct {
	OK     bool   `json:"ok"`
	Status int    `json:"status"`
	Error  string `json:"error"`
}

func hookEvents(raw string) []string {
	var evs []string
	if err := json.Unmarshal([]byte(raw), &evs); err != nil || evs == nil {
		return []string{}
	}
	return evs
}

func toWebhook(w dbq.Webhook) Webhook {
	return Webhook{ID: w.ID, Name: w.Name, URL: w.Url, HasSecret: w.Secret != "", Events: hookEvents(w.Events), Enabled: w.Enabled != 0}
}

func validate(in WebhookInput) (WebhookInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimSpace(in.URL)
	if in.Name == "" {
		return in, errNameNeeded
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return in, errBadURL
	}
	for _, ev := range in.Events {
		if !slices.Contains(AllEvents, ev) {
			return in, errBadEvent
		}
	}
	if in.Events == nil {
		in.Events = []string{}
	}
	return in, nil
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (s *Service) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	rows, err := s.q.ListWebhooks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Webhook, 0, len(rows))
	for _, r := range rows {
		out = append(out, toWebhook(r))
	}
	return out, nil
}

func (s *Service) CreateWebhook(ctx context.Context, in WebhookInput) (Webhook, error) {
	in, err := validate(in)
	if err != nil {
		return Webhook{}, err
	}
	evs, _ := json.Marshal(in.Events)
	row, err := s.q.CreateWebhook(ctx, dbq.CreateWebhookParams{
		Name: in.Name, Url: in.URL, Secret: in.Secret, Events: string(evs), Enabled: b2i(in.Enabled),
	})
	if err != nil {
		return Webhook{}, err
	}
	return toWebhook(row), nil
}

func (s *Service) UpdateWebhook(ctx context.Context, id int64, in WebhookInput) (Webhook, error) {
	in, err := validate(in)
	if err != nil {
		return Webhook{}, err
	}
	cur, err := s.q.GetWebhook(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Webhook{}, ErrNotFound
	} else if err != nil {
		return Webhook{}, err
	}
	secret := cur.Secret
	switch {
	case in.ClearSecret:
		secret = ""
	case in.Secret != "":
		secret = in.Secret
	}
	evs, _ := json.Marshal(in.Events)
	row, err := s.q.UpdateWebhook(ctx, dbq.UpdateWebhookParams{
		Name: in.Name, Url: in.URL, Secret: secret, Events: string(evs), Enabled: b2i(in.Enabled), ID: id,
	})
	if err != nil {
		return Webhook{}, err
	}
	return toWebhook(row), nil
}

func (s *Service) DeleteWebhook(ctx context.Context, id int64) error {
	if _, err := s.q.GetWebhook(ctx, id); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return s.q.DeleteWebhook(ctx, id)
}

// TestWebhook sends a test event to one webhook and waits for the result.
func (s *Service) TestWebhook(ctx context.Context, id int64) (TestResult, error) {
	hook, err := s.q.GetWebhook(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return TestResult{}, ErrNotFound
	} else if err != nil {
		return TestResult{}, err
	}
	body := s.body(ctx, Event{Type: "test", Message: "This is a test from softwarr."})
	status, err := s.deliver(ctx, hook, body)
	res := TestResult{OK: err == nil, Status: status}
	if err != nil {
		res.Error = err.Error()
	}
	return res, nil
}

// body builds the JSON webhook payload for an event.
func (s *Service) body(ctx context.Context, e Event) []byte {
	m := map[string]any{}
	for k, v := range e.Payload {
		m[k] = v
	}
	m["event"] = e.Type
	m["timestamp"] = s.now().UTC().Format(time.RFC3339)
	m["message"] = e.Message
	m["requestId"], m["itemId"], m["url"] = nil, nil, nil
	if e.RequestID != 0 {
		m["requestId"] = e.RequestID
	}
	if e.ItemID != 0 {
		m["itemId"] = e.ItemID
	}
	if cfg, err := s.s.Get(ctx); err == nil && cfg.ExternalURL != "" {
		switch {
		case e.RequestID != 0:
			m["url"] = fmt.Sprintf("%s/requests/%d", cfg.ExternalURL, e.RequestID)
		case e.ItemID != 0:
			m["url"] = fmt.Sprintf("%s/library/%d", cfg.ExternalURL, e.ItemID)
		}
	}
	b, _ := json.Marshal(m)
	return b
}

// sendWebhooks starts a goroutine per enabled webhook subscribed to e.
func (s *Service) sendWebhooks(ctx context.Context, e Event) {
	hooks, err := s.q.ListWebhooks(ctx)
	if err != nil {
		slog.Error("notify: list webhooks", "err", err)
		return
	}
	var body []byte
	for _, h := range hooks {
		if h.Enabled == 0 || !slices.Contains(hookEvents(h.Events), e.Type) {
			continue
		}
		if body == nil {
			body = s.body(ctx, e)
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			// Detached from the caller's context so a finished request doesn't cancel delivery.
			dctx, cancel := context.WithTimeout(context.Background(), webhookTimeout)
			defer cancel()
			if _, err := s.deliver(dctx, h, body); err != nil {
				slog.Warn("notify: webhook failed", "webhook", h.ID, "err", err)
			}
		}()
	}
}

// deliver POSTs body to the webhook. It returns the HTTP status (0 if the
// request never completed) and an error for transport failures or non-2xx.
func (s *Service) deliver(ctx context.Context, h dbq.Webhook, body []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.Url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "softwarr")
	if h.Secret != "" {
		mac := hmac.New(sha256.New, []byte(h.Secret))
		mac.Write(body)
		req.Header.Set("X-Softwarr-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := s.httpc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("receiver answered %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// ListFor returns the newest 100 notifications for a user.
func (s *Service) ListFor(ctx context.Context, userID int64, unreadOnly bool) ([]Notification, error) {
	flag := int64(0)
	if unreadOnly {
		flag = 1
	}
	rows, err := s.q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: userID, Column2: flag})
	if err != nil {
		return nil, err
	}
	out := make([]Notification, 0, len(rows))
	for _, r := range rows {
		out = append(out, toNotification(r))
	}
	return out, nil
}

// MarkRead marks the given notifications read. Ids belonging to other users
// are ignored.
func (s *Service) MarkRead(ctx context.Context, userID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return s.q.MarkNotificationsRead(ctx, dbq.MarkNotificationsReadParams{
		ReadAt: sql.NullInt64{Int64: s.now().Unix(), Valid: true}, UserID: userID, Ids: ids,
	})
}

func (s *Service) MarkAllRead(ctx context.Context, userID int64) error {
	return s.q.MarkAllNotificationsRead(ctx, dbq.MarkAllNotificationsReadParams{
		ReadAt: sql.NullInt64{Int64: s.now().Unix(), Valid: true}, UserID: userID,
	})
}
