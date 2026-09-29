package upload

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/utils"
	"github.com/silencoo/speed-probe/vendors"
)

func direct(ctx context.Context) interfaces.Vendor {
	return vendors.WithContext(ctx, vendors.Build(ctx, interfaces.VendorMihomo, "test", "name: test\ntype: direct\n"))
}
func config(url string, threads uint, limit uint64) *interfaces.SlaveRequestConfigs {
	return &interfaces.SlaveRequestConfigs{UploadURL: url, DownloadThreading: threads, DownloadDuration: 1, DownloadBytes: limit}
}

func TestSharedBudgetAndConfirmedThroughput(t *testing.T) {
	for _, threads := range []uint{1, 4, 32} {
		var received atomic.Uint64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Error("not a POST")
			}
			n, err := io.Copy(io.Discard, r.Body)
			if err != nil {
				t.Error(err)
			}
			received.Add(uint64(n))
			w.WriteHeader(204)
		}))
		m := &Upload{}
		Once(m, direct(context.Background()), config(server.URL, threads, 100003))
		server.Close()
		if m.TotalBytes != 100003 || m.SentBytes != 100003 || received.Load() != 100003 || m.StopReason != "byte_limit" || m.Value == 0 || m.ErrorCode != "" {
			t.Fatalf("threads=%d result=%+v received=%d", threads, m, received.Load())
		}
	}
}

func TestConfiguredRateLimitEndsNormally(t *testing.T) {
	old := utils.GCFG.SpeedLimit
	utils.GCFG.SpeedLimit = 128 * 1024
	defer func() { utils.GCFG.SpeedLimit = old }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body); w.WriteHeader(204) }))
	defer server.Close()
	m := &Upload{}
	Once(m, direct(context.Background()), config(server.URL, 1, 0))
	if m.StopReason != "duration" || m.ErrorCode != "" || m.TotalBytes == 0 || m.SentBytes > 160*1024 {
		t.Fatalf("rate limiter misreported completion: %+v", m)
	}
}
func TestRejectAndRedirectNeverBecomeSuccessfulUpload(t *testing.T) {
	for _, code := range []int{403, 413, 429, 302, 307} {
		expected := "http_error"
		if code < 400 {
			expected = "source_redirect"
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			w.Header().Set("Location", "http://127.0.0.1:1")
			w.WriteHeader(code)
		}))
		m := &Upload{}
		Once(m, direct(context.Background()), config(server.URL, 4, 100003))
		server.Close()
		if m.Value != 0 || m.TotalBytes != 0 || m.ErrorCode != expected || m.HTTPCode != code || m.SentBytes > 100003 {
			t.Fatalf("%+v", m)
		}
	}
}
func TestUnconfirmedBodiesAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			if cancelled {
				cancel()
			}
			<-r.Context().Done()
		}))
		m := &Upload{}
		start := time.Now()
		Once(m, direct(ctx), config(server.URL, 2, 100003))
		cancel()
		server.Close()
		if m.TotalBytes != 0 || m.Value != 0 || m.SentBytes > 100003 || time.Since(start) > 2*time.Second {
			t.Fatalf("%+v", m)
		}
		if cancelled && m.StopReason != "cancelled" {
			t.Fatalf("%+v", m)
		}
		if !cancelled && m.ErrorCode != "timeout" {
			t.Fatalf("%+v", m)
		}
	}
}
func TestRejectInvalidEndpointAndMissingProxy(t *testing.T) {
	for _, url := range []string{"https://user:pass@example.com/", "ftp://example.com/", "http://example.com/#secret"} {
		m := &Upload{}
		Once(m, direct(context.Background()), config(url, 1, 1))
		if m.ErrorCode != "invalid_url" {
			t.Fatalf("%+v", m)
		}
	}
	m := &Upload{}
	Once(m, nil, config("http://127.0.0.1:1", 1, 1))
	if m.ErrorCode != "network_error" {
		t.Fatalf("%+v", m)
	}
}
func TestBothCoresUploadThroughSelectedSOCKSNode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body); w.WriteHeader(204) }))
	defer server.Close()
	address, calls := testSOCKSServer(t)
	_, port, _ := net.SplitHostPort(address)
	for _, kind := range []interfaces.VendorType{interfaces.VendorMihomo, interfaces.VendorSingBox} {
		t.Run(string(kind), func(t *testing.T) {
			payload := fmt.Sprintf("name: test\ntype: socks5\nserver: 127.0.0.1\nport: %s\n", port)
			if kind == interfaces.VendorSingBox {
				payload = fmt.Sprintf(`{"type":"socks","server":"127.0.0.1","server_port":%s,"version":"5"}`, port)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			proxy := vendors.Build(ctx, kind, "node", payload)
			if closer, ok := proxy.(interface{ Close() error }); ok {
				defer closer.Close()
			}
			before := calls.Load()
			m := &Upload{}
			Once(m, vendors.WithContext(ctx, proxy), config(server.URL, 1, 200003))
			if m.TotalBytes != 200003 || m.ErrorCode != "" || calls.Load() <= before {
				t.Fatalf("core=%s proxy calls=%d result=%+v", kind, calls.Load(), m)
			}
		})
	}
}
func testSOCKSServer(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	calls := &atomic.Int32{}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				header := make([]byte, 2)
				if _, err := io.ReadFull(conn, header); err != nil {
					return
				}
				methods := make([]byte, int(header[1]))
				if _, err := io.ReadFull(conn, methods); err != nil {
					return
				}
				conn.Write([]byte{5, 0})
				command := make([]byte, 4)
				if _, err := io.ReadFull(conn, command); err != nil {
					return
				}
				var host string
				switch command[3] {
				case 1:
					ip := make([]byte, 4)
					io.ReadFull(conn, ip)
					host = net.IP(ip).String()
				case 3:
					length := make([]byte, 1)
					io.ReadFull(conn, length)
					name := make([]byte, int(length[0]))
					io.ReadFull(conn, name)
					host = string(name)
				default:
					return
				}
				port := make([]byte, 2)
				io.ReadFull(conn, port)
				target, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(binary.BigEndian.Uint16(port))), time.Second)
				if err != nil {
					return
				}
				defer target.Close()
				calls.Add(1)
				conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				go io.Copy(target, conn)
				io.Copy(conn, target)
			}()
		}
	}()
	return listener.Addr().String(), calls
}
