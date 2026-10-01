// Package testproxy runs temporary real protocol servers for local integration tests.
package testproxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	J "github.com/sagernet/sing/common/json"
	"github.com/silencoo/speed-probe/interfaces"
)

type Server struct {
	Hop         interfaces.PathHop
	mu          sync.Mutex
	Sources     []string
	active      int
	connections map[net.Conn]bool
}

func (s *Server) Observed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.Sources...)
}
func (s *Server) Active() int { s.mu.Lock(); defer s.mu.Unlock(); return s.active }

func New(t *testing.T, typ, source string) *Server {
	t.Helper()
	front, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	temporary, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := temporary.Addr().(*net.TCPAddr).Port
	temporary.Close()
	secret := make([]byte, 16)
	if _, err = rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	password := base64.StdEncoding.EncodeToString(secret)
	inbound := map[string]any{"type": typ, "listen": "127.0.0.1", "listen_port": port, "tag": "in"}
	outbound := map[string]any{"type": typ, "server": "127.0.0.1", "server_port": front.Addr().(*net.TCPAddr).Port, "password": password}
	if typ == "shadowsocks" {
		inbound["method"] = "2022-blake3-aes-128-gcm"
		inbound["password"] = password
		outbound["method"] = inbound["method"]
	} else {
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if e != nil {
			t.Fatal(e)
		}
		keyDER, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			t.Fatal(e)
		}
		cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
		private := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
		inbound["users"] = []any{map[string]any{"name": "test", "password": password}}
		inbound["tls"] = map[string]any{"enabled": true, "certificate": []string{cert}, "key": []string{private}}
		outbound["tls"] = map[string]any{"enabled": true, "server_name": "localhost", "certificate": []string{cert}}
	}
	ctx := box.Context(context.Background(), include.InboundRegistry(), include.OutboundRegistry(), include.EndpointRegistry(), include.DNSTransportRegistry(), include.ServiceRegistry(), include.CertificateProviderRegistry())
	raw, _ := json.Marshal(map[string]any{"log": map[string]any{"disabled": true}, "inbounds": []any{inbound}, "outbounds": []any{map[string]any{"type": "direct", "tag": "out", "inet4_bind_address": source}}, "route": map[string]any{"final": "out"}})
	var options option.Options
	if err = J.UnmarshalContext(ctx, raw, &options); err != nil {
		t.Fatal(err)
	}
	instance, err := box.New(box.Options{Context: ctx, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	if err = instance.Start(); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(outbound)
	s := &Server{Hop: interfaces.PathHop{ID: typ, Name: typ, Payload: string(payload)}, connections: map[net.Conn]bool{}}
	var wg sync.WaitGroup
	var udpFront net.PacketConn
	udpPeers := map[string]*net.UDPConn{}
	var udpMu sync.Mutex
	if typ == "shadowsocks" {
		udpFront, err = net.ListenPacket("udp4", front.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 65536)
			for {
				n, peer, e := udpFront.ReadFrom(buf)
				if e != nil {
					return
				}
				udpMu.Lock()
				up := udpPeers[peer.String()]
				if up == nil {
					up, e = net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
					if e != nil {
						udpMu.Unlock()
						return
					}
					udpPeers[peer.String()] = up
					s.mu.Lock()
					s.Sources = append(s.Sources, peer.(*net.UDPAddr).IP.String())
					s.mu.Unlock()
					wg.Add(1)
					go func(up *net.UDPConn, peer net.Addr) {
						defer wg.Done()
						reply := make([]byte, 65536)
						for {
							n, _, e := up.ReadFrom(reply)
							if e != nil {
								return
							}
							udpFront.WriteTo(reply[:n], peer)
						}
					}(up, peer)
				}
				up.Write(buf[:n])
				udpMu.Unlock()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, e := front.Accept()
			if e != nil {
				return
			}
			s.mu.Lock()
			s.Sources = append(s.Sources, c.RemoteAddr().(*net.TCPAddr).IP.String())
			s.connections[c] = true
			s.active++
			s.mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				defer func() { s.mu.Lock(); delete(s.connections, c); s.active--; s.mu.Unlock() }()
				up, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
				if e != nil {
					return
				}
				defer up.Close()
				done := make(chan struct{})
				go func() { io.Copy(up, c); up.Close(); close(done) }()
				io.Copy(c, up)
				c.Close()
				<-done
			}()
		}
	}()
	t.Cleanup(func() {
		front.Close()
		if udpFront != nil {
			udpFront.Close()
			udpMu.Lock()
			for _, up := range udpPeers {
				up.Close()
			}
			udpMu.Unlock()
		}
		s.mu.Lock()
		for c := range s.connections {
			c.Close()
		}
		s.mu.Unlock()
		instance.Close()
		wg.Wait()
	})
	return s
}
