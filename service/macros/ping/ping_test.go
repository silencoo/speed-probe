package ping

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/vendors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
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

func TestFailureCodeDoesNotExposeRawErrors(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{fmt.Errorf("private URL: %w", context.DeadlineExceeded), "timeout"},
		{&net.DNSError{Err: "PRIVATE_HOST", Name: "private.invalid"}, "dns_error"},
		{&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, "connection_refused"},
		{fmt.Errorf("private: %w", syscall.ECONNRESET), "connection_reset"},
		{tls.RecordHeaderError{Msg: "PRIVATE_CERT"}, "tls_error"},
		{context.Canceled, "cancelled"},
		{errors.New("PASSWORD_SECRET"), "network_error"},
	}
	for _, tc := range cases {
		if got := vendors.NetworkErrorCode(tc.err); got != tc.code {
			t.Fatalf("got %q want %q", got, tc.code)
		}
	}
}

func TestTimeoutIsReportedInMeasurement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer server.Close()
	m := &Ping{}
	m.Run(vendors.WithContext(context.Background(), nil), &interfaces.SlaveRequest{Configs: interfaces.SlaveRequestConfigs{
		PingAddress: server.URL, PingAverageOver: 2, TaskTimeout: 20,
	}})
	if m.Attempts != 2 || m.Failures != 2 || m.ErrorCode != "timeout" || m.Request != 0 {
		t.Fatalf("%+v", m)
	}
}

func TestStatisticsDoNotDiscardSlowSamples(t *testing.T) {
	avg, _, max := statistics([]uint16{10, 10, 1000})
	if avg != 340 || max != 1000 {
		t.Fatal(avg, max)
	}
}
