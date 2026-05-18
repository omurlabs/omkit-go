package syncnotifier_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omurlabs/omkit-go/syncnotifier"
)

type capturedRequest struct {
	method      string
	path        string
	contentType string
	token       string
	body        map[string]any
}

func TestNotifyMetrics_SendsPostWithEnvelope(t *testing.T) {
	got := make(chan capturedRequest, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		got <- capturedRequest{
			method:      r.Method,
			path:        r.URL.Path,
			contentType: r.Header.Get("Content-Type"),
			token:       r.Header.Get("X-Service-Token"),
			body:        body,
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	n := syncnotifier.New(srv.URL, "service-token-xyz")
	rows := []map[string]any{{"k": "v", "count": 1.0}}
	n.NotifyMetrics("daily.active", "2026-05-18", rows)

	select {
	case req := <-got:
		if req.method != http.MethodPost {
			t.Errorf("method: want POST, got %s", req.method)
		}
		if req.path != "/sync/metrics" {
			t.Errorf("path: want /sync/metrics, got %s", req.path)
		}
		if req.contentType != "application/json" {
			t.Errorf("content-type: want application/json, got %s", req.contentType)
		}
		if req.token != "service-token-xyz" {
			t.Errorf("token header: want service-token-xyz, got %s", req.token)
		}
		if req.body["metric_name"] != "daily.active" {
			t.Errorf("metric_name: %v", req.body["metric_name"])
		}
		if req.body["date"] != "2026-05-18" {
			t.Errorf("date: %v", req.body["date"])
		}
		gotRows, ok := req.body["rows"].([]any)
		if !ok || len(gotRows) != 1 {
			t.Errorf("rows: %v", req.body["rows"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notifier did not POST within 2s")
	}
}

func TestNotifyMetrics_NonAcceptedStatusDoesNotPanic(t *testing.T) {
	done := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		done <- struct{}{}
	}))
	defer srv.Close()

	n := syncnotifier.New(srv.URL, "tok")
	n.NotifyMetrics("m", "2026-05-18", nil)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server never received request")
	}
}

func TestNotifyMetrics_UnreachableServerDoesNotPanic(t *testing.T) {
	n := syncnotifier.New("http://127.0.0.1:1", "tok")
	n.NotifyMetrics("m", "2026-05-18", nil)
	time.Sleep(50 * time.Millisecond)
}
