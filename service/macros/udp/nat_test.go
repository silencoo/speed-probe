package udp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pion/stun"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/vendors"
)

type datagram struct {
	raw  []byte
	from net.Addr
}
type testPacket struct {
	mu       sync.Mutex
	deadline time.Time
	closed   chan struct{}
	once     sync.Once
	queue    chan datagram
	send     func(*stun.Message, *net.UDPAddr)
}

func newPacket() *testPacket {
	return &testPacket{closed: make(chan struct{}), queue: make(chan datagram, 16)}
}
func (p *testPacket) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
}
func (p *testPacket) Close() error { p.once.Do(func() { close(p.closed) }); return nil }
func (p *testPacket) SetDeadline(t time.Time) error {
	p.mu.Lock()
	p.deadline = t
	p.mu.Unlock()
	return nil
}
func (p *testPacket) SetReadDeadline(t time.Time) error  { return p.SetDeadline(t) }
func (p *testPacket) SetWriteDeadline(t time.Time) error { return p.SetDeadline(t) }
func (p *testPacket) WriteTo(b []byte, a net.Addr) (int, error) {
	msg := &stun.Message{Raw: append([]byte(nil), b...)}
	if err := msg.Decode(); err != nil {
		return 0, err
	}
	p.send(msg, a.(*net.UDPAddr))
	return len(b), nil
}
func (p *testPacket) ReadFrom(b []byte) (int, net.Addr, error) {
	p.mu.Lock()
	end := p.deadline
	p.mu.Unlock()
	timer := time.NewTimer(time.Until(end))
	defer timer.Stop()
	select {
	case d := <-p.queue:
		return copy(b, d.raw), d.from, nil
	case <-p.closed:
		return 0, nil, net.ErrClosed
	case <-timer.C:
		return 0, nil, &net.DNSError{IsTimeout: true}
	}
}

var primary = &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 3478}
var alternate = &net.UDPAddr{IP: net.IPv4(192, 0, 2, 2), Port: 3479}

func response(request *stun.Message, port int, extended bool) *stun.Message {
	msg := stun.MustBuild(stun.BindingSuccess, stun.NewTransactionIDSetter(request.TransactionID), &stun.XORMappedAddress{IP: net.IPv4(198, 51, 100, 1), Port: port})
	if extended {
		(&stun.OtherAddress{IP: alternate.IP, Port: alternate.Port}).AddTo(msg)
	}
	return msg
}
func TestSTUNNeedsBehaviorDiscoveryButRetainsReachability(t *testing.T) {
	p := newPacket()
	defer p.Close()
	p.send = func(r *stun.Message, a *net.UDPAddr) { p.queue <- datagram{response(r, 5000, false).Raw, a} }
	mt, reachable, err := mappingTests(p, primary, time.Second)
	if mt != NATMapFailed || !reachable || !errors.Is(err, errNoOtherAddress) {
		t.Fatalf("%v %v %v", mt, reachable, err)
	}
}
func TestMappingAndFiltering(t *testing.T) {
	for _, mode := range []string{"full", "restricted", "port", "symmetric", "missing_mapped", "ignores_change", "lost_connection"} {
		t.Run(mode, func(t *testing.T) {
			p := newPacket()
			defer p.Close()
			initial := false
			p.send = func(r *stun.Message, a *net.UDPAddr) {
				change, _ := r.Get(stun.AttrChangeRequest)
				flags := byte(0)
				if len(change) == 4 {
					flags = change[3]
				}
				if mode == "lost_connection" && initial {
					return
				}
				if flags == 6 && (mode == "restricted" || mode == "port") {
					return
				}
				if flags == 2 && mode == "port" {
					return
				}
				port := 5000
				if mode == "symmetric" && !a.IP.Equal(primary.IP) {
					port = 5001 + a.Port - primary.Port
				}
				msg := response(r, port, true)
				if mode == "missing_mapped" && !a.IP.Equal(primary.IP) {
					msg = stun.MustBuild(stun.BindingSuccess, stun.NewTransactionIDSetter(r.TransactionID))
				}
				source := a
				if mode != "ignores_change" {
					if flags == 6 {
						source = alternate
					}
					if flags == 2 {
						source = &net.UDPAddr{IP: primary.IP, Port: alternate.Port}
					}
				}
				p.queue <- datagram{msg.Raw, source}
				initial = true
			}
			if mode == "symmetric" || mode == "missing_mapped" || mode == "full" {
				got, _, err := mappingTests(p, primary, 20*time.Millisecond)
				if mode == "missing_mapped" {
					if !errors.Is(err, errInvalidResponse) {
						t.Fatal(err)
					}
					return
				}
				expected := NATMapIndependent
				if mode == "symmetric" {
					expected = NATMapAddrPortIndependent
				}
				if err != nil || got != expected {
					t.Fatalf("%v %v", got, err)
				}
			}
			initial = false
			got, _, err := filteringTests(p, primary, 20*time.Millisecond)
			if mode == "ignores_change" {
				if !errors.Is(err, errNoOtherAddress) {
					t.Fatal(err)
				}
				return
			}
			if mode == "lost_connection" {
				if !errors.Is(err, errTimedOut) {
					t.Fatal(err)
				}
				return
			}
			expected := NATFilterIndependent
			if mode == "restricted" {
				expected = NATFilterAddrIndependent
			}
			if mode == "port" {
				expected = NATFilterAddrPortIndependent
			}
			if err != nil || got != expected {
				t.Fatalf("%v %v", got, err)
			}
		})
	}
}
func TestRoundTripIgnoresStaleAndMalformedPacketsAndRetries(t *testing.T) {
	p := newPacket()
	defer p.Close()
	calls := 0
	p.send = func(r *stun.Message, a *net.UDPAddr) {
		calls++
		if calls == 1 {
			return
		}
		p.queue <- datagram{[]byte("not STUN"), a}
		stale := response(stun.MustBuild(stun.TransactionID, stun.BindingRequest), 5000, true)
		p.queue <- datagram{stale.Raw, a}
		p.queue <- datagram{response(r, 6000, true).Raw, a}
	}
	msg, err := roundTrip(p, primary, primary, 0, 20*time.Millisecond)
	if err != nil || calls != 2 {
		t.Fatalf("calls %d: %v", calls, err)
	}
	addr, _ := mapped(msg)
	if addr.Port != 6000 {
		t.Fatal("accepted stale transaction")
	}
}
func TestErrorResponseCannotBeFullCone(t *testing.T) {
	p := newPacket()
	defer p.Close()
	p.send = func(r *stun.Message, a *net.UDPAddr) {
		p.queue <- datagram{stun.MustBuild(stun.BindingError, stun.NewTransactionIDSetter(r.TransactionID)).Raw, a}
	}
	_, _, err := filteringTests(p, primary, time.Second)
	if !errors.Is(err, errSTUNRejected) {
		t.Fatal(err)
	}
}

type packetVendor struct {
	interfaces.Vendor
	packet *testPacket
}

func (v packetVendor) DialUDP(context.Context, string) (net.PacketConn, error) { return v.packet, nil }
func TestCancellationClosesPacketReaders(t *testing.T) {
	p := newPacket()
	p.send = func(*stun.Message, *net.UDPAddr) {}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, code := detectNATType(vendors.WithContext(ctx, packetVendor{packet: p}), "udp://192.0.2.1:3478", time.Second)
	if code != "cancelled" {
		t.Fatal(code)
	}
	if got := udpError(context.Background(), fmt.Errorf("NODE_SECRET: %w", interfaces.ErrUDPUnsupported)); got != "udp_unsupported" {
		t.Fatal(got)
	}
}
func TestBothCoresRetainBasicUDPReachability(t *testing.T) {
	server, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, a, err := server.ReadFrom(buf)
			if err != nil {
				return
			}
			r := &stun.Message{Raw: append([]byte(nil), buf[:n]...)}
			if r.Decode() == nil {
				server.WriteTo(response(r, 5000, false).Raw, a)
			}
		}
	}()
	for _, kind := range []interfaces.VendorType{interfaces.VendorMihomo, interfaces.VendorSingBox} {
		t.Run(string(kind), func(t *testing.T) {
			payload := "type: direct\n"
			if kind == interfaces.VendorSingBox {
				payload = `{"type":"direct"}`
			}
			base := vendors.Build(context.Background(), kind, "test", payload)
			if closer, ok := base.(interface{ Close() error }); ok {
				defer closer.Close()
			}
			mt, ft, reachable, code := detectNATType(vendors.WithContext(context.Background(), base), "udp://"+server.LocalAddr().String(), time.Second)
			if natTypeToString(mt, ft) != "Unknown" || !reachable || code != "stun_unsupported" {
				t.Fatalf("%v %v %v %s", mt, ft, reachable, code)
			}
		})
	}
}
