package speed

import (
	"context"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/vendors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSharedBudgetAcrossThreads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 100; i++ {
			if _, err := w.Write(make([]byte, 16384)); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	for _, threads := range []uint{1, 8, 32} {
		m := &Speed{}
		cfg := interfaces.SlaveRequestConfigs{DownloadURL: server.URL, DownloadDuration: 5, DownloadThreading: threads, DownloadBytes: 100003}
		Once(m, vendors.WithContext(context.Background(), nil), &cfg)
		if m.TotalSize != 100003 || m.StopReason != "byte_limit" || m.ElapsedMillis >= 5000 || m.AvgSpeed == 0 {
			t.Fatalf("threads %d: %+v", threads, m)
		}
	}
}

func TestBudgetContinuesAcrossShortFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("12345")) }))
	defer server.Close()
	m := &Speed{}
	Once(m, vendors.WithContext(context.Background(), nil), &interfaces.SlaveRequestConfigs{DownloadURL: server.URL, DownloadDuration: 5, DownloadThreading: 4, DownloadBytes: 503})
	if m.TotalSize != 503 || m.StopReason != "byte_limit" {
		t.Fatalf("%+v", m)
	}
}

func TestUniformDurationAndErrorReason(t *testing.T) {
	for _, code := range []int{200, 403} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			for i := 0; i < 200; i++ {
				if _, err := w.Write(make([]byte, 1024)); err != nil {
					return
				}
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}))
		m := &Speed{}
		Once(m, vendors.WithContext(context.Background(), nil), &interfaces.SlaveRequestConfigs{DownloadURL: server.URL, DownloadDuration: 1, DownloadThreading: 4})
		server.Close()
		if code == 403 {
			if m.TotalSize != 0 || m.StopReason != "download_error" || m.ErrorCode != "http_error" || m.HTTPCode != 403 {
				t.Fatalf("%+v", m)
			}
		} else if m.TotalSize == 0 || m.StopReason != "duration" || m.ElapsedMillis < 950 || m.ElapsedMillis > 2000 {
			t.Fatalf("%+v", m)
		}
	}
}

func TestNoDownloadBytesReportsTimeoutOrEmptyBody(t *testing.T) {
	for _, stage := range []string{"headers", "body", "empty"} {
		t.Run(stage, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if stage == "empty" {
					return
				}
				if stage == "body" {
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer server.Close()
			m := &Speed{}
			Once(m, vendors.WithContext(context.Background(), nil), &interfaces.SlaveRequestConfigs{DownloadURL: server.URL, DownloadDuration: 1, DownloadThreading: 2})
			want := "timeout"
			if stage == "empty" {
				want = "empty_response"
			}
			if m.TotalSize != 0 || m.StopReason != "download_error" || m.ErrorCode != want {
				t.Fatalf("%+v", m)
			}
		})
	}
}

func TestCancelIsNotDownloadFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := &Speed{}
	Once(m, vendors.WithContext(ctx, nil), &interfaces.SlaveRequestConfigs{DownloadURL: "http://127.0.0.1:1", DownloadDuration: 1, DownloadThreading: 2})
	if m.StopReason != "cancelled" || m.ErrorCode != "" {
		t.Fatalf("%+v", m)
	}
}

func TestHTTPErrorBodyIsNotThroughput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403); w.Write(make([]byte, 65536)) }))
	defer server.Close()
	counter := &WriteCounter{}
	stop := SingleThread([]string{server.URL}, vendors.WithContext(context.Background(), nil), 1, counter)
	time.Sleep(30 * time.Millisecond)
	stop()
	if counter.Take() != 0 {
		t.Fatal("error page counted as download")
	}
}
func TestSlowHeaderDoesNotBlockWindowOrCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	begin := time.Now()
	stop := SingleThread([]string{server.URL}, vendors.WithContext(ctx, nil), 30, &WriteCounter{})
	if time.Since(begin) > time.Second {
		t.Fatal("startup blocked on network")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("not started")
	}
	cancel()
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop download")
	}
}
