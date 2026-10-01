package ping

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	if m.Attempts != 4 || m.Failures != 1 || m.HTTPCode != 403 || m.Request == 0 || m.RTT == 0 || m.RTTFailures != 0 || m.RTTErrorCode != "" || m.ErrorPhase != "http" {
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
	if m.Attempts != 2 || m.Failures != 2 || m.ErrorCode != "timeout" || m.Request != 0 || m.RTT == 0 || m.RTTFailures != 0 || m.RTTErrorCode != "" || m.ErrorPhase != "http" {
		t.Fatalf("%+v", m)
	}
}

func TestHTTPSHeaderTimeoutPreservesVerifiedTLSConnection(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	p := vendors.WithContext(ctx, nil)
	rtt, delay, status, phase, err := sampleWithTransport(ctx, p, server.URL,
		&http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}})
	if rtt == 0 || delay != 0 || status != 0 || phase != "http" || vendors.NetworkErrorCode(err) != "timeout" {
		t.Fatalf("rtt=%d delay=%d status=%d phase=%s error=%v", rtt, delay, status, phase, err)
	}
}

func TestTLSFailureDoesNotCountAsSuccessfulConnection(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	defer server.Close()
	m := &Ping{}
	m.Run(vendors.WithContext(context.Background(), nil), &interfaces.SlaveRequest{Configs: interfaces.SlaveRequestConfigs{
		PingAddress: server.URL, PingAverageOver: 1, TaskTimeout: 1000,
	}})
	if m.RTT != 0 || m.Request != 0 || m.RTTFailures != 1 || m.Failures != 1 ||
		m.RTTErrorCode != "tls_error" || m.RTTErrorPhase != "tls" || m.ErrorPhase != "tls" {
		t.Fatalf("%+v", m)
	}
}

func TestDialFailureRemainsConnectionFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	m := &Ping{}
	m.Run(vendors.WithContext(context.Background(), nil), &interfaces.SlaveRequest{Configs: interfaces.SlaveRequestConfigs{
		PingAddress: "http://" + address, PingAverageOver: 2, TaskTimeout: 1000,
	}})
	if m.RTT != 0 || m.Request != 0 || m.RTTFailures != 2 || m.Failures != 2 ||
		m.RTTErrorCode == "" || m.RTTErrorCode != m.ErrorCode || m.RTTErrorPhase != "connect" || m.ErrorPhase != "connect" {
		t.Fatalf("%+v", m)
	}
}

func TestStatisticsDoNotDiscardSlowSamples(t *testing.T) {
	avg, _, max := statistics([]uint16{10, 10, 1000})
	if avg != 340 || max != 1000 {
		t.Fatal(avg, max)
	}
}
