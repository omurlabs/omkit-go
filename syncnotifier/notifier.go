// notifier.go — notifier module.
//
// exports: Notifier | New | Notify | NotifyDelete | NotifyMetrics
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package syncnotifier provides fire-and-forget notifications to solid-sync.
package syncnotifier

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Notifier sends async sync notifications to solid-sync service.
type Notifier struct {
	baseURL string
	token   string
	client  *http.Client
}

// New creates a SyncNotifier.
func New(baseURL, token string) *Notifier {
	return &Notifier{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Notify fires a sync notification in the background.
func (n *Notifier) Notify(resourceType, resourceID string, data any) {
	go n.send(context.Background(), http.MethodPost, "/sync/"+resourceType+"/"+resourceID, data)
}

// NotifyDelete fires a delete notification in the background.
func (n *Notifier) NotifyDelete(resourceType, resourceID string) {
	go n.sendDelete(context.Background(), "/sync/"+resourceType+"/"+resourceID)
}

// NotifyMetrics fires a metrics sync notification in the background.
func (n *Notifier) NotifyMetrics(metricName, date string, rows []map[string]any) {
	payload := map[string]any{
		"metric_name": metricName,
		"date":        date,
		"rows":        rows,
	}
	go n.send(context.Background(), http.MethodPost, "/sync/metrics", payload)
}

func (n *Notifier) send(ctx context.Context, method, path string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, method, n.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Service-Token", n.token)

	resp, err := n.client.Do(req)
	if err != nil {
		slog.Warn("sync_notifier.failed", "path", path, "error", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != 202 {
		slog.Warn("sync_notifier.unexpected_status", "path", path, "status", resp.StatusCode)
	}
}

func (n *Notifier) sendDelete(ctx context.Context, path string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, n.baseURL+path, nil)
	if err != nil {
		return
	}
	req.Header.Set("X-Service-Token", n.token)

	resp, err := n.client.Do(req)
	if err != nil {
		slog.Warn("sync_notifier.delete_failed", "path", path, "error", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != 202 {
		slog.Warn("sync_notifier.delete_unexpected_status", "path", path, "status", resp.StatusCode)
	}
}
