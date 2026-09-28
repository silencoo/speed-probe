package singbox

import (
	"context"
	"fmt"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/hysteria"
	"github.com/sagernet/sing-box/protocol/hysteria2"
	"github.com/sagernet/sing-box/protocol/tuic"
	"github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/silencoo/speed-probe/interfaces"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// One isolated instance per node; no listener, routing subscription or shared config.
type SingBox struct {
	instance *box.Box
	outbound adapter.Outbound
	info     interfaces.ProxyInfo
	once     sync.Once
}

func (s *SingBox) Type() interfaces.VendorType { return interfaces.VendorSingBox }
func (s *SingBox) Status() interfaces.VendorStatus {
	if s.outbound == nil {
		return interfaces.VStatusNotReady
	}
	return interfaces.VStatusOperational
}
func (s *SingBox) Build(name, payload string) interfaces.Vendor {
	return s.BuildContext(context.Background(), name, payload)
}
func (s *SingBox) BuildContext(ctx context.Context, name, payload string) interfaces.Vendor {
	s.info.Name = name
	registry := include.OutboundRegistry()
	// Include QUIC outbounds in ordinary builds too (upstream include uses build tags).
	hysteria.RegisterOutbound(registry)
	hysteria2.RegisterOutbound(registry)
	tuic.RegisterOutbound(registry)
	ctx = box.Context(ctx, include.InboundRegistry(), registry, include.EndpointRegistry(),
		include.DNSTransportRegistry(), include.ServiceRegistry(), include.CertificateProviderRegistry())
	var outbound option.Outbound
	if err := json.UnmarshalContext(ctx, []byte(payload), &outbound); err != nil {
		return s
	}
	// A node must be a standalone outbound, not a selector or other routing control.
	switch outbound.Type {
	case "selector", "urltest", "block", "dns", "bridge", "wireguard", "shadowsocksr":
		return s
	}
	outbound.Tag = "test"
	opts := option.Options{Log: &option.LogOptions{Disabled: true}, Outbounds: []option.Outbound{outbound}}
	instance, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		return s
	}
	s.instance = instance
	if err = instance.Start(); err != nil {
		s.Close()
		return s
	}
	s.outbound, _ = instance.Outbound().Outbound("test")
	var raw struct {
		Server string `json:"server"`
		Port   uint16 `json:"server_port"`
	}
	_ = json.Unmarshal([]byte(payload), &raw)
	s.info.Address = net.JoinHostPort(raw.Server, strconv.Itoa(int(raw.Port)))
	s.info.Type = interfaces.ProxyType(outbound.Type)
	return s
}
func destination(raw string) (M.Socksaddr, error) {
	if !strings.Contains(raw, "://") {
		raw = "tcp://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return M.Socksaddr{}, err
	}
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || u.Hostname() == "" {
		return M.Socksaddr{}, fmt.Errorf("invalid destination")
	}
	return M.ParseSocksaddr(net.JoinHostPort(u.Hostname(), port)), nil
}
func (s *SingBox) DialTCP(ctx context.Context, raw string, network interfaces.RequestOptionsNetwork) (net.Conn, error) {
	if s.outbound == nil {
		return nil, fmt.Errorf("sing-box is not ready")
	}
	dest, err := destination(raw)
	if err != nil {
		return nil, err
	}
	return s.outbound.DialContext(ctx, "tcp", dest)
}
func (s *SingBox) DialUDP(ctx context.Context, raw string) (net.PacketConn, error) {
	if s.outbound == nil {
		return nil, fmt.Errorf("sing-box is not ready")
	}
	dest, err := destination(raw)
	if err != nil {
		return nil, err
	}
	return s.outbound.ListenPacket(ctx, dest)
}
func (s *SingBox) ProxyInfo() interfaces.ProxyInfo { return s.info }
func (s *SingBox) Close() error {
	var err error
	s.once.Do(func() {
		if s.instance != nil {
			err = s.instance.Close()
		}
	})
	return err
}
