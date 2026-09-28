package vendors

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/silencoo/speed-probe/interfaces"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

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
func TestBothCoresActuallyProxyHTTP(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "through-proxy") }))
	defer upstream.Close()
	address, calls := testSOCKSServer(t)
	_, port, _ := net.SplitHostPort(address)
	for _, kind := range []interfaces.VendorType{interfaces.VendorMihomo, interfaces.VendorSingBox} {
		t.Run(string(kind), func(t *testing.T) {
			payload := fmt.Sprintf("name: test\ntype: socks5\nserver: 127.0.0.1\nport: %s\n", port)
			if kind == interfaces.VendorSingBox {
				payload = fmt.Sprintf(`{"type":"socks","server":"127.0.0.1","server_port":%s,"version":"5"}`, port)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			base := Build(ctx, kind, "node", payload)
			if closer, ok := base.(interface{ Close() error }); ok {
				defer closer.Close()
			}
			if base.Status() != interfaces.VStatusOperational {
				t.Fatal("core did not initialize")
			}
			body, resp, _ := RequestWithRetry(WithContext(ctx, base), 1, 2000, &interfaces.RequestOptions{URL: upstream.URL})
			if resp == nil || string(body) != "through-proxy" {
				t.Fatalf("request failed: %s", body)
			}
			if base.ProxyInfo().Name != "node" {
				t.Fatal("node identity lost")
			}
		})
	}
	if calls.Load() != 2 {
		t.Fatalf("proxy was bypassed: %d", calls.Load())
	}
}
func TestBothCoresUDPAndClose(t *testing.T) {
	echo, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		b := make([]byte, 128)
		for {
			n, addr, err := echo.ReadFrom(b)
			if err != nil {
				return
			}
			echo.WriteTo(b[:n], addr)
		}
	}()
	for _, kind := range []interfaces.VendorType{interfaces.VendorMihomo, interfaces.VendorSingBox} {
		t.Run(string(kind), func(t *testing.T) {
			payload := "name: test\ntype: direct\n"
			if kind == interfaces.VendorSingBox {
				payload = `{"type":"direct"}`
			}
			base := Build(context.Background(), kind, "direct", payload)
			if closer, ok := base.(interface{ Close() error }); ok {
				defer closer.Close()
			}
			if base.Status() != interfaces.VStatusOperational {
				t.Fatal("not ready")
			}
			packet, err := base.DialUDP(context.Background(), "udp://"+echo.LocalAddr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer packet.Close()
			packet.SetDeadline(time.Now().Add(time.Second))
			_, err = packet.WriteTo([]byte("hello"), echo.LocalAddr())
			if err != nil {
				t.Fatal(err)
			}
			b := make([]byte, 32)
			n, _, err := packet.ReadFrom(b)
			if err != nil || string(b[:n]) != "hello" {
				t.Fatalf("UDP: %s %v", b[:n], err)
			}
		})
	}
}
func TestMihomoNewProtocolNotRejectedByOldWhitelist(t *testing.T) {
	for _, payload := range []string{
		"type: hysteria2\nserver: 127.0.0.1\nport: 443\npassword: test\nskip-cert-verify: true\n",
		"type: anytls\nserver: 127.0.0.1\nport: 443\npassword: test\nskip-cert-verify: true\n",
		"type: ss\nserver: 127.0.0.1\nport: 443\ncipher: 2022-blake3-aes-128-gcm\npassword: AAAAAAAAAAAAAAAAAAAAAA==\n",
	} {
		t.Run(strings.Split(payload, "\n")[0], func(t *testing.T) {
			base := Build(context.Background(), interfaces.VendorMihomo, "new", payload)
			if closer, ok := base.(interface{ Close() error }); ok {
				defer closer.Close()
			}
			if base.Status() != interfaces.VStatusOperational {
				t.Fatal("protocol rejected")
			}
		})
	}
}
