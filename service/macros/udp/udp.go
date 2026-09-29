package udp

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/vendors"
)

func resolveSTUN(ctx context.Context, raw string) (*net.UDPAddr, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errInvalidResponse
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 || u.Scheme != "udp" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errInvalidResponse
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", u.Hostname())
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, &net.DNSError{Err: "no IPv4 address"}
	}
	return &net.UDPAddr{IP: ips[0], Port: port}, nil
}
func detectNATType(proxy interfaces.Vendor, raw string, timeout time.Duration) (NATMapType, NATFilterType, bool, string) {
	ctx, cancel := context.WithTimeout(vendors.Context(proxy), 5*timeout)
	defer cancel()
	dnsCtx, dnsCancel := context.WithTimeout(ctx, timeout)
	addr, err := resolveSTUN(dnsCtx, raw)
	dnsCancel()
	if err != nil {
		return NATMapFailed, NATFilterFailed, false, udpError(ctx, err)
	}
	var mt NATMapType
	var ft NATFilterType
	var mr, fr bool
	var me, fe error
	var wg sync.WaitGroup
	probe := func(run func(net.PacketConn)) error {
		conn, err := proxy.DialUDP(ctx, "udp://"+addr.String())
		if err != nil {
			return err
		}
		if conn == nil {
			return errors.New("nil UDP connection")
		}
		defer conn.Close()
		stop := context.AfterFunc(ctx, func() { conn.Close() })
		defer stop()
		run(conn)
		return nil
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := probe(func(c net.PacketConn) { mt, mr, me = mappingTests(c, addr, timeout) }); err != nil {
			me = err
		}
	}()
	go func() {
		defer wg.Done()
		if err := probe(func(c net.PacketConn) { ft, fr, fe = filteringTests(c, addr, timeout) }); err != nil {
			fe = err
		}
	}()
	wg.Wait()
	if me != nil {
		err = me
	} else {
		err = fe
	}
	return mt, ft, mr || fr, udpError(ctx, err)
}
func udpError(ctx context.Context, err error) string {
	if err == nil {
		return ""
	}
	if ctx.Err() != nil {
		return vendors.NetworkErrorCode(ctx.Err())
	}
	switch {
	case errors.Is(err, errNoOtherAddress):
		return "stun_unsupported"
	case errors.Is(err, errInvalidResponse):
		return "stun_invalid_response"
	case errors.Is(err, errSTUNRejected):
		return "stun_rejected"
	case errors.Is(err, errTimedOut):
		return "timeout"
	case errors.Is(err, interfaces.ErrUDPUnsupported):
		return "udp_unsupported"
	default:
		return vendors.NetworkErrorCode(err)
	}
}
func natTypeToString(nmt NATMapType, nft NATFilterType) string {
	if nmt == NATMapFailed || nft == NATFilterFailed {
		return "Unknown"
	}
	if nmt == NATMapNoNat {
		if nft == NATFilterIndependent {
			return "OpenInternet"
		}
		return "SymmetricFirewall"
	}
	if nmt == NATMapIndependent {
		switch nft {
		case NATFilterIndependent:
			return "FullCone"
		case NATFilterAddrIndependent:
			return "RestrictedCone"
		default:
			return "PortRestrictedCone"
		}
	}
	return "Symmetric"
}
