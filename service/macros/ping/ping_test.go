package ping

import (
	"context"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/vendors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSamplingIncludesFailuresAndStatus(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 2 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		w.WriteHeader(403)
	}))
	defer server.Close()
	m := &Ping{}
	m.Run(vendors.WithContext(context.Background(), nil), &interfaces.SlaveRequest{Configs: interfaces.SlaveRequestConfigs{
		PingAddress: server.URL, PingAverageOver: 4, TaskRetry: 1, TaskTimeout: 1000,
	}})
	if m.Attempts != 4 || m.Failures != 1 || m.HTTPCode != 403 || m.Request == 0 {
		t.Fatalf("%+v", m)
	}
}

func TestStatisticsDoNotDiscardSlowSamples(t *testing.T) {
	avg, _, max := statistics([]uint16{10, 10, 1000})
	if avg != 340 || max != 1000 {
		t.Fatal(avg, max)
	}
}
