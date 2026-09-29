package udp

import (
	"errors"
	"net"
	"net/netip"
	"time"

	"github.com/pion/stun"
)

type NATMapType int

const (
	NATMapFailed NATMapType = iota
	NATMapIndependent
	NATMapAddrIndependent
	NATMapAddrPortIndependent
	NATMapNoNat
)

type NATFilterType int

const (
	NATFilterFailed NATFilterType = iota
	NATFilterIndependent
	NATFilterAddrIndependent
	NATFilterAddrPortIndependent
)

var (
	errTimedOut        = errors.New("STUN response timed out")
	errNoOtherAddress  = errors.New("STUN behavior discovery unsupported")
	errInvalidResponse = errors.New("invalid STUN response")
	errSTUNRejected    = errors.New("STUN request rejected")
)

// No reader goroutine: closing the task's packet connection interrupts ReadFrom.
// Retry once within the fixed budget and ignore unrelated/stale datagrams.
func roundTrip(conn net.PacketConn, target, expected *net.UDPAddr, flags byte, timeout time.Duration) (*stun.Message, error) {
	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	if flags != 0 {
		request.Add(stun.AttrChangeRequest, []byte{0, 0, 0, flags})
	}
	end := time.Now().Add(timeout)
	buf := make([]byte, 2048)
	for attempt := 0; attempt < 2; attempt++ {
		deadline := end
		if attempt == 0 {
			deadline = time.Now().Add(timeout / 2)
		}
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, err
		}
		if _, err := conn.WriteTo(request.Raw, target); err != nil {
			return nil, err
		}
		for time.Now().Before(deadline) {
			n, source, err := conn.ReadFrom(buf)
			if err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					break
				}
				return nil, err
			}
			msg := &stun.Message{Raw: append([]byte(nil), buf[:n]...)}
			if msg.Decode() != nil || msg.TransactionID != request.TransactionID {
				continue
			}
			if msg.Type == stun.BindingError {
				return nil, errSTUNRejected
			}
			if msg.Type != stun.BindingSuccess {
				continue
			}
			if !sameAddress(source, expected) {
				return nil, errNoOtherAddress
			}
			return msg, nil
		}
	}
	return nil, errTimedOut
}
func sameAddress(a net.Addr, b *net.UDPAddr) bool {
	if a == nil || b == nil {
		return false
	}
	// Some cores return their own net.Addr implementation.
	address, err := netip.ParseAddrPort(a.String())
	ip, ok := netip.AddrFromSlice(b.IP)
	return err == nil && ok && int(address.Port()) == b.Port && address.Addr().Unmap() == ip.Unmap()
}
func mapped(msg *stun.Message) (*stun.XORMappedAddress, error) {
	addr := new(stun.XORMappedAddress)
	if addr.GetFrom(msg) != nil || addr.IP == nil || addr.Port < 1 {
		return nil, errInvalidResponse
	}
	return addr, nil
}
func other(msg *stun.Message, primary *net.UDPAddr) (*net.UDPAddr, error) {
	addr := new(stun.OtherAddress)
	if addr.GetFrom(msg) != nil || addr.IP.To4() == nil || addr.Port < 1 || addr.Port == primary.Port || addr.IP.Equal(primary.IP) {
		return nil, errNoOtherAddress
	}
	return &net.UDPAddr{IP: addr.IP, Port: addr.Port}, nil
}

// RFC 5780 mapping and filtering need separate sockets: sending to the alternate
// server during mapping would open the filter being measured by filtering.
func mappingTests(conn net.PacketConn, primary *net.UDPAddr, timeout time.Duration) (NATMapType, bool, error) {
	first, err := roundTrip(conn, primary, primary, 0, timeout)
	if err != nil {
		return NATMapFailed, false, err
	}
	a, err := mapped(first)
	if err != nil {
		return NATMapFailed, false, err
	}
	alternate, err := other(first, primary)
	if err != nil {
		return NATMapFailed, true, err
	}
	if sameAddress(conn.LocalAddr(), &net.UDPAddr{IP: a.IP, Port: a.Port}) {
		return NATMapNoNat, true, nil
	}
	secondAddr := &net.UDPAddr{IP: alternate.IP, Port: primary.Port}
	second, err := roundTrip(conn, secondAddr, secondAddr, 0, timeout)
	if err != nil {
		return NATMapFailed, true, err
	}
	b, err := mapped(second)
	if err != nil {
		return NATMapFailed, true, err
	}
	if a.String() == b.String() {
		return NATMapIndependent, true, nil
	}
	third, err := roundTrip(conn, alternate, alternate, 0, timeout)
	if err != nil {
		return NATMapFailed, true, err
	}
	c, err := mapped(third)
	if err != nil {
		return NATMapFailed, true, err
	}
	if b.String() == c.String() {
		return NATMapAddrIndependent, true, nil
	}
	return NATMapAddrPortIndependent, true, nil
}
func filteringTests(conn net.PacketConn, primary *net.UDPAddr, timeout time.Duration) (NATFilterType, bool, error) {
	first, err := roundTrip(conn, primary, primary, 0, timeout)
	if err != nil {
		return NATFilterFailed, false, err
	}
	if _, err = mapped(first); err != nil {
		return NATFilterFailed, false, err
	}
	alternate, err := other(first, primary)
	if err != nil {
		return NATFilterFailed, true, err
	}
	_, err = roundTrip(conn, primary, alternate, 6, timeout)
	if err == nil {
		return NATFilterIndependent, true, nil
	}
	if !errors.Is(err, errTimedOut) {
		return NATFilterFailed, true, err
	}
	expected := &net.UDPAddr{IP: primary.IP, Port: alternate.Port}
	_, err = roundTrip(conn, primary, expected, 2, timeout)
	if err == nil {
		return NATFilterAddrIndependent, true, nil
	}
	if !errors.Is(err, errTimedOut) {
		return NATFilterFailed, true, err
	}
	// A lost connection is not evidence of a port-restricted filter.
	if _, err = roundTrip(conn, primary, primary, 0, timeout); err != nil {
		return NATFilterFailed, true, err
	}
	return NATFilterAddrPortIndependent, true, nil
}
