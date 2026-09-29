package upload

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestBadSourceDoesNotFanOut(t *testing.T) {
	for _, tc := range []struct {
		contentType, body, code string
		status                  int
	}{
		{"text/html", "<html>verification</html>", "invalid_content", 200},
		{"application/json", `{"error":"rate limited"}`, "invalid_content", 200},
		{"", "", "http_error", 403},
		{"", "", "source_redirect", 307},
	} {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", tc.contentType)
			w.WriteHeader(tc.status)
			w.Write([]byte(tc.body))
		}))
		m := &Upload{}
		Once(m, direct(context.Background()), config(server.URL, 32, 1024*1024))
		server.Close()
		if calls.Load() != 1 || m.TotalBytes != 0 || m.ErrorCode != tc.code || m.SourceHealth != "failed" || m.ErrorPhase != "source_check" {
			t.Fatalf("calls=%d result=%+v", calls.Load(), m)
		}
	}
}

func TestSuccessfulJSONAcknowledgementThenRejectedTransfer(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if calls.Add(1) > 1 {
			w.WriteHeader(429)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"received":65536}`))
	}))
	defer server.Close()
	m := &Upload{}
	Once(m, direct(context.Background()), config(server.URL, 4, 1024*1024))
	if m.TotalBytes != 65536 || m.ErrorCode != "http_error" || m.HTTPCode != 429 || m.SourceHealth != "passed" || m.ErrorPhase != "transfer" {
		t.Fatalf("%+v", m)
	}
}
