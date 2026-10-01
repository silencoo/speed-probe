package vendors

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/internal/testproxy"
)

func TestRealOrderedSS2022AnyTLSPaths(t *testing.T) {
	ss := testproxy.New(t, "shadowsocks", "127.0.0.2")
	tls := testproxy.New(t, "anytls", "127.0.0.3")
	var calls atomic.Int32
	var observed atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		observed.Store(host)
		if r.Method == "POST" {
			b, _ := io.ReadAll(r.Body)
			if !bytes.Equal(b, bytes.Repeat([]byte("u"), 8192)) {
				t.Error("upload changed")
			}
			w.WriteHeader(204)
			return
		}
		w.Write(bytes.Repeat([]byte("d"), 8192))
	}))
	defer target.Close()
	cases := []struct {
		name  string
		hops  []interfaces.PathHop
		exit  string
		next  *testproxy.Server
		relay string
	}{{"SS2022", []interfaces.PathHop{ss.Hop}, "127.0.0.2", nil, ""}, {"AnyTLS", []interfaces.PathHop{tls.Hop}, "127.0.0.3", nil, ""}, {"SS2022-AnyTLS", []interfaces.PathHop{ss.Hop, tls.Hop}, "127.0.0.3", tls, "127.0.0.2"}, {"AnyTLS-SS2022", []interfaces.PathHop{tls.Hop, ss.Hop}, "127.0.0.2", ss, "127.0.0.3"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			before := 0
			if tc.next != nil {
				before = len(tc.next.Observed())
			}
			v := BuildNode(ctx, interfaces.VendorSingBox, interfaces.SlaveRequestNode{Path: &interfaces.TestPath{ID: "chain", Name: tc.name, Hops: tc.hops}})
			if v.Status() != interfaces.VStatusOperational {
				t.Fatal("not ready")
			}
			defer v.(interface{ Close() error }).Close()
			for _, method := range []string{"GET", "POST"} {
				response, _, e := RequestUnsafe(ctx, WithContext(ctx, v), &interfaces.RequestOptions{Method: method, URL: target.URL, Body: bytes.Repeat([]byte("u"), 8192)})
				if e != nil {
					t.Fatal(e)
				}
				b, e := io.ReadAll(response.Body)
				response.Body.Close()
				if e != nil {
					t.Fatal(e)
				}
				if method == "GET" && len(b) != 8192 {
					t.Fatal("download incomplete")
				}
				if observed.Load() != tc.exit {
					t.Fatalf("wrong exit source %v", observed.Load())
				}
			}
			if tc.next != nil {
				sources := tc.next.Observed()[before:]
				if len(sources) == 0 {
					t.Fatal("next hop was bypassed")
				}
				for _, source := range sources {
					if source != tc.relay {
						t.Fatalf("physical order wrong: %s", source)
					}
				}
			}
		})
	}
	for _, tc := range cases {
		t.Run(tc.name+" UDP", func(t *testing.T) {
			echo, e := net.ListenPacket("udp4", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer echo.Close()
			seen := make(chan string, 1)
			go func() {
				buf := make([]byte, 1024)
				n, from, e := echo.ReadFrom(buf)
				if e != nil {
					return
				}
				seen <- from.(*net.UDPAddr).IP.String()
				echo.WriteTo(buf[:n], from)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			path := &interfaces.TestPath{ID: "udp", Name: tc.name, Hops: tc.hops}
			if _, e = path.Compile(true); e != nil {
				t.Fatal(e)
			}
			v := BuildNode(ctx, interfaces.VendorSingBox, interfaces.SlaveRequestNode{Path: path})
			defer v.(interface{ Close() error }).Close()
			packet, e := WithContext(ctx, v).DialUDP(ctx, echo.LocalAddr().String())
			if e != nil {
				t.Fatal(e)
			}
			defer packet.Close()
			packet.SetDeadline(time.Now().Add(2 * time.Second))
			if _, e = packet.WriteTo([]byte("path-datagram"), echo.LocalAddr()); e != nil {
				t.Fatal(e)
			}
			buf := make([]byte, 1024)
			n, _, e := packet.ReadFrom(buf)
			if e != nil || string(buf[:n]) != "path-datagram" {
				t.Fatalf("UDP path failed: %v", e)
			}
			if source := <-seen; source != tc.exit {
				t.Fatalf("UDP bypassed exit: %s", source)
			}
			cancel()
			if _, _, e = packet.ReadFrom(buf); e == nil {
				t.Fatal("cancelled UDP session remained readable")
			}
		})
	}
	t.Run("broken relay never falls back", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		broken := ss.Hop
		broken.Payload = `{"type":"shadowsocks","server":"127.0.0.1","server_port":1,"method":"2022-blake3-aes-128-gcm","password":"AAAAAAAAAAAAAAAAAAAAAA=="}`
		before := calls.Load()
		v := BuildNode(ctx, interfaces.VendorSingBox, interfaces.SlaveRequestNode{Path: &interfaces.TestPath{ID: "broken", Name: "broken", Hops: []interfaces.PathHop{broken, tls.Hop}}})
		defer v.(interface{ Close() error }).Close()
		r, _, e := RequestUnsafe(ctx, WithContext(ctx, v), &interfaces.RequestOptions{URL: target.URL})
		if e == nil {
			r.Body.Close()
			t.Fatal("broken relay succeeded")
		}
		if calls.Load() != before {
			t.Fatal("target reached via fallback")
		}
	})
	t.Run("cancel closes stream and pooled sessions", func(t *testing.T) {
		streamStarted := make(chan struct{})
		streamStopped := make(chan struct{})
		stream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(streamStarted)
			w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			close(streamStopped)
		}))
		defer stream.Close()
		ctx, cancel := context.WithCancel(context.Background())
		v := BuildNode(ctx, interfaces.VendorSingBox, interfaces.SlaveRequestNode{Path: &interfaces.TestPath{ID: "cancel", Name: "cancel", Hops: []interfaces.PathHop{ss.Hop, tls.Hop}}})
		r, _, e := RequestUnsafe(ctx, WithContext(ctx, v), &interfaces.RequestOptions{URL: stream.URL})
		if e != nil {
			t.Fatal(e)
		}
		<-streamStarted
		cancel()
		v.(interface{ Close() error }).Close()
		r.Body.Close()
		select {
		case <-streamStopped:
		case <-time.After(2 * time.Second):
			t.Fatal("stream remained open")
		}
		deadline := time.Now().Add(2 * time.Second)
		for ss.Active()+tls.Active() != 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if ss.Active()+tls.Active() != 0 {
			t.Fatal("proxy sessions leaked")
		}
	})
}
