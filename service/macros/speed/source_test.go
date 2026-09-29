package speed

import (
	"context"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/vendors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSourceFailureDoesNotFanOut(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body, code string
		status                        int
	}{
		{"forbidden", "", "", "http_error", 403},
		{"rate-limit", "", "", "http_error", 429},
		{"redirect", "", "", "source_redirect", 302},
		{"html", "text/html", "<html>challenge</html>", "invalid_content", 200},
		{"json", "application/json", "{}", "invalid_content", 200},
		{"fragmented-html", "application/octet-stream", "<html>challenge</html>", "invalid_content", 200},
		{"empty", "", "", "empty_response", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", tc.contentType)
				w.Header().Set("Location", "http://127.0.0.1:1")
				w.WriteHeader(tc.status)
				for _, b := range []byte(tc.body) {
					w.Write([]byte{b})
					w.(http.Flusher).Flush()
				}
			}))
			defer server.Close()
			m := &Speed{}
			Once(m, vendors.WithContext(context.Background(), nil), &interfaces.SlaveRequestConfigs{DownloadURL: server.URL, DownloadThreading: 32, DownloadDuration: 1})
			if calls.Load() != 1 || m.TotalSize != 0 || m.ErrorCode != tc.code || m.SourceHealth != "failed" || m.ErrorPhase != "source_check" {
				t.Fatalf("calls=%d result=%+v", calls.Load(), m)
			}
		})
	}
}

func TestSourcePassPreservesLaterFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Write(make([]byte, 1024))
			return
		}
		w.WriteHeader(403)
	}))
	defer server.Close()
	m := &Speed{}
	Once(m, vendors.WithContext(context.Background(), nil), &interfaces.SlaveRequestConfigs{DownloadURL: server.URL, DownloadThreading: 4, DownloadDuration: 1})
	if m.TotalSize != 1024 || m.ErrorCode != "http_error" || m.HTTPCode != 403 || m.SourceHealth != "passed" || m.ErrorPhase != "transfer" {
		t.Fatalf("%+v", m)
	}
}

func TestCancelDuringSourceCheckReleasesWaiters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { cancel(); <-r.Context().Done() }))
	defer server.Close()
	m := &Speed{}
	started := time.Now()
	Once(m, vendors.WithContext(ctx, nil), &interfaces.SlaveRequestConfigs{DownloadURL: server.URL, DownloadThreading: 32, DownloadDuration: 30})
	if time.Since(started) > time.Second || m.StopReason != "cancelled" {
		t.Fatalf("%+v", m)
	}
}
