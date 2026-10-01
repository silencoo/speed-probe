package ping

import (
	"context"
	"crypto/tls"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/vendors"
	"math"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"time"
)

// Each sample uses a fresh connection. RTT here is connection setup (TCP plus
// TLS for HTTPS), not ICMP RTT. HTTP delay runs through response headers.
func sample(ctx context.Context, p interfaces.Vendor, target string) (uint16, uint16, int, string, error) {
	return sampleWithTransport(ctx, p, target, &http.Transport{TLSHandshakeTimeout: 5 * time.Second})
}

func sampleWithTransport(ctx context.Context, p interfaces.Vendor, target string, transport *http.Transport) (uint16, uint16, int, string, error) {
	start := time.Now()
	var connected atomic.Int64
	var phase atomic.Int32 // 0: proxy connection, 1: target TLS, 2: HTTP headers.
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return 0, 0, 0, "connect", err
	}
	transport.DisableKeepAlives = true
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := p.DialTCP(ctx, addr, interfaces.ROptionsTCP)
		if err == nil {
			if req.URL.Scheme == "https" {
				phase.Store(1)
			} else {
				connected.Store(time.Since(start).Nanoseconds())
				phase.Store(2)
			}
		}
		return conn, err
	}
	defer transport.CloseIdleConnections()
	trace := &httptrace.ClientTrace{TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
		if err == nil {
			connected.Store(time.Since(start).Nanoseconds())
			phase.Store(2)
		}
	}}
	resp, err := transport.RoundTrip(req.WithContext(httptrace.WithClientTrace(ctx, trace)))
	ms := func(v int64) uint16 {
		if v <= 0 {
			return 0
		}
		v /= int64(time.Millisecond)
		if v < 1 {
			v = 1
		}
		if v > 65535 {
			v = 65535
		}
		return uint16(v)
	}
	rtt := ms(connected.Load())
	if err != nil {
		return rtt, 0, 0, [...]string{"connect", "tls", "http"}[phase.Load()], err
	}
	delay := ms(time.Since(start).Nanoseconds())
	resp.Body.Close()
	return rtt, delay, resp.StatusCode, "", nil
}

func statistics(values []uint16) (uint16, uint16, uint16) {
	if len(values) == 0 {
		return 0, 0, 0
	}
	var sum, variance float64
	var peak uint16
	for _, v := range values {
		sum += float64(v)
		if v > peak {
			peak = v
		}
	}
	mean := sum / float64(len(values))
	for _, v := range values {
		variance += math.Pow(float64(v)-mean, 2)
	}
	return uint16(math.Round(mean)), uint16(math.Round(math.Sqrt(variance / float64(len(values))))), peak
}

func measure(m *Ping, p interfaces.Vendor, cfg interfaces.SlaveRequestConfigs) {
	rtts, requests := []uint16{}, []uint16{}
	p = vendors.WithContext(vendors.Context(p), p)
	for i := uint16(0); i < cfg.PingAverageOver && vendors.Context(p).Err() == nil; i++ {
		ctx, cancel := context.WithTimeout(vendors.Context(p), time.Duration(cfg.TaskTimeout)*time.Millisecond)
		rtt, delay, status, phase, err := sample(ctx, p, cfg.PingAddress)
		cancel()
		m.Attempts++
		if rtt > 0 {
			rtts = append(rtts, rtt)
		} else {
			m.RTTFailures++
			m.RTTErrorCode = vendors.NetworkErrorCode(err)
			m.RTTErrorPhase = phase
		}
		if err != nil {
			m.Failures++
			m.ErrorCode = vendors.NetworkErrorCode(err)
			m.ErrorPhase = phase
			continue
		}
		m.HTTPCode = status
		requests = append(requests, delay)
	}
	m.RTT, m.RTTStd, m.RTTMax = statistics(rtts)
	m.Request, m.RequestStd, m.RequestMax = statistics(requests)
}
