package vendors

import (
	"context"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/vendors/local"
	"net"
	"sync"
)

// A task-scoped vendor propagates cancellation through legacy macro interfaces.
type scopedVendor struct {
	interfaces.Vendor
	ctx context.Context
}

func Context(p interfaces.Vendor) context.Context {
	if v, ok := p.(*scopedVendor); ok {
		return v.ctx
	}
	return context.Background()
}
func WithContext(ctx context.Context, p interfaces.Vendor) interfaces.Vendor {
	if p == nil {
		p = (&local.Local{}).Build("direct", "")
	}
	return &scopedVendor{p, ctx}
}
func (v *scopedVendor) DialTCP(ctx context.Context, url string, network interfaces.RequestOptionsNetwork) (net.Conn, error) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(v.ctx, cancel)
	if v.ctx.Err() != nil {
		cancel()
	}
	conn, err := v.Vendor.DialTCP(ctx, url, network)
	if err != nil {
		stop()
		cancel()
		return nil, err
	}
	wrapped := &scopedConn{Conn: conn}
	stopClose := context.AfterFunc(ctx, func() { conn.Close() })
	wrapped.cleanup = func() { stopClose(); stop(); cancel() }
	return wrapped, nil
}
func (v *scopedVendor) DialUDP(ctx context.Context, url string) (net.PacketConn, error) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(v.ctx, cancel)
	if v.ctx.Err() != nil {
		cancel()
	}
	conn, err := v.Vendor.DialUDP(ctx, url)
	if err != nil {
		stop()
		cancel()
		return nil, err
	}
	wrapped := &scopedPacket{PacketConn: conn}
	stopClose := context.AfterFunc(ctx, func() { conn.Close() })
	wrapped.cleanup = func() { stopClose(); stop(); cancel() }
	return wrapped, nil
}

type scopedConn struct {
	net.Conn
	once    sync.Once
	cleanup func()
}

func (c *scopedConn) Close() error { err := c.Conn.Close(); c.once.Do(c.cleanup); return err }

type scopedPacket struct {
	net.PacketConn
	once    sync.Once
	cleanup func()
}

func (c *scopedPacket) Close() error { err := c.PacketConn.Close(); c.once.Do(c.cleanup); return err }
