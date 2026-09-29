// Package upload measures HTTP POST throughput through the selected outbound.
// Only fully consumed request bodies with a successful response contribute to
// throughput. In-flight/rejected bytes still consume the shared traffic budget.
package upload

import (
	"context"
	"crypto/rand"
	"github.com/silencoo/speed-probe/service/macros/sourcecheck"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/utils"
	"github.com/silencoo/speed-probe/vendors"
	"golang.org/x/time/rate"
)

type Result struct {
	SourceHealth  string
	ErrorPhase    string `json:",omitempty"`
	Value         uint64
	Max           uint64
	TotalBytes    uint64 // Successful POST body bytes, used for throughput.
	SentBytes     uint64 // Body bytes handed to transport, including incomplete POSTs.
	Speeds        []uint64
	ElapsedMillis int64
	StopReason    string
	ErrorCode     string `json:",omitempty"`
	HTTPCode      int    `json:",omitempty"`
}
type Upload struct{ Result }

func (m *Upload) Type() interfaces.SlaveRequestMacroType { return interfaces.MacroUpload }
func (m *Upload) Run(proxy interfaces.Vendor, r *interfaces.SlaveRequest) error {
	Once(m, proxy, &r.Configs)
	return nil
}

// Reservations never exceed the budget, even with many simultaneous POSTs.
type state struct {
	sync.Mutex
	remaining uint64
	limited   bool
	confirmed uint64
	bins      []float64
	sent      atomic.Uint64
	code      string
	status    int
}

func (s *state) reserve(n int) int {
	s.Lock()
	defer s.Unlock()
	if s.limited {
		if uint64(n) > s.remaining {
			n = int(s.remaining)
		}
		s.remaining -= uint64(n)
	}
	return n
}
func (s *state) failure(code string, status int) {
	s.Lock()
	defer s.Unlock()
	if s.code == "" {
		s.code, s.status = code, status
	}
}
func (s *state) accept(n int, start, end float64) {
	s.Lock()
	defer s.Unlock()
	s.confirmed += uint64(n)
	// Spread each acknowledged block over its request interval. Counting it all
	// at response time would create artificial one-second peaks.
	duration := end - start
	if duration <= 0 {
		return
	}
	for i := int(start); i < len(s.bins) && float64(i) < end; i++ {
		overlap := math.Min(end, float64(i+1)) - math.Max(start, float64(i))
		if overlap > 0 {
			s.bins[i] += float64(n) * overlap / duration
		}
	}
}

type body struct {
	ctx       context.Context
	data      []byte
	remaining int
	read      atomic.Int64
	sent      *atomic.Uint64
	limiter   *rate.Limiter
}

func (b *body) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	n := min(len(p), len(b.data), b.remaining)
	if b.limiter != nil {
		if err := b.limiter.WaitN(b.ctx, n); err != nil {
			// WaitN can reject a reservation just before the deadline. Treat this
			// as the normal end of the window, rather than a network failure.
			<-b.ctx.Done()
			return 0, b.ctx.Err()
		}
	}
	copy(p, b.data[:n])
	b.remaining -= n
	b.read.Add(int64(n))
	b.sent.Add(uint64(n))
	return n, nil
}

func Once(m *Upload, proxy interfaces.Vendor, cfg *interfaces.SlaveRequestConfigs) {
	m.Result = Result{SourceHealth: "failed", ErrorPhase: "source_check", Speeds: []uint64{}, StopReason: "upload_error"}
	u, err := url.Parse(cfg.UploadURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" {
		m.ErrorCode = "invalid_url"
		return
	}
	if proxy == nil || proxy.Status() != interfaces.VStatusOperational {
		m.ErrorCode = "network_error"
		return // Never silently bypass the selected node.
	}
	duration := time.Duration(cfg.DownloadDuration) * time.Second
	if duration < time.Second || duration > 30*time.Second || cfg.DownloadThreading < 1 || cfg.DownloadThreading > 32 {
		m.ErrorCode = "invalid_options"
		return
	}
	vendors.Report(proxy, "upload_check", true)
	defer vendors.Report(proxy, "upload_check", false)
	defer vendors.Report(proxy, "upload", false)
	gate := sourcecheck.New()
	started := time.Now()
	ctx, cancel := context.WithTimeout(vendors.Context(proxy), duration)
	defer cancel()
	s := &state{remaining: cfg.DownloadBytes, limited: cfg.DownloadBytes > 0, bins: make([]float64, int(cfg.DownloadDuration)+1)}
	data := make([]byte, 32*1024)
	if _, err := rand.Read(data); err != nil {
		m.ErrorCode = "network_error"
		return
	}
	var limiter *rate.Limiter
	if utils.GCFG.SpeedLimit > 0 {
		limiter = rate.NewLimiter(rate.Limit(utils.GCFG.SpeedLimit), len(data))
	}
	transport := &http.Transport{
		// HTTP/1.1 gives each worker one real connection, consistently across cores.
		MaxIdleConnsPerHost: int(cfg.DownloadThreading), MaxConnsPerHost: int(cfg.DownloadThreading),
		IdleConnTimeout: 5 * time.Second, TLSHandshakeTimeout: 5 * time.Second,
		DisableCompression: true,
		DialContext: func(c context.Context, _, address string) (net.Conn, error) {
			return proxy.DialTCP(c, address, interfaces.ROptionsTCP)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var wg sync.WaitGroup
	for i := uint(0); i < cfg.DownloadThreading; i++ {
		wg.Add(1)
		go func(first bool) {
			defer wg.Done()
			if first {
				defer gate.Close()
			} else if !gate.Wait(ctx) {
				return
			}
			size := 64 * 1024
			for ctx.Err() == nil {
				n := s.reserve(size)
				if n == 0 {
					return
				}
				reader := &body{ctx: ctx, data: data, remaining: n, sent: &s.sent, limiter: limiter}
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.UploadURL, reader)
				if err != nil {
					s.failure("invalid_url", 0)
					return
				}
				req.ContentLength = int64(n)
				req.Header.Set("Content-Type", "application/octet-stream")
				req.Header.Set("Cache-Control", "no-store")
				begin := time.Now()
				resp, err := client.Do(req)
				if err != nil {
					if ctx.Err() == nil {
						s.failure(vendors.NetworkErrorCode(err), 0)
					}
					return
				}
				// Bound responses from custom endpoints; never retain their contents.
				responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
				resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					code := "http_error"
					if resp.StatusCode >= 300 && resp.StatusCode < 400 {
						code = "source_redirect"
					}
					s.failure(code, resp.StatusCode)
					return
				}
				if readErr != nil {
					if ctx.Err() == nil {
						s.failure(vendors.NetworkErrorCode(readErr), resp.StatusCode)
					}
					return
				}
				if sourcecheck.InvalidContent("", responseBody) || strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
					s.failure("invalid_content", resp.StatusCode)
					return
				}
				if reader.read.Load() != int64(n) {
					s.failure("incomplete_upload", resp.StatusCode)
					return
				}
				ended := time.Now()
				s.accept(n, begin.Sub(started).Seconds(), ended.Sub(started).Seconds())
				if first && !gate.Passed() {
					gate.Pass()
					vendors.Report(proxy, "upload_check", false)
					vendors.Report(proxy, "upload", true)
				}
				// Aim for short blocks to bound unconfirmed tails on slow links, while
				// amortizing HTTP overhead on fast links. Memory remains 32 KiB per task.
				size = int(math.Max(16*1024, math.Min(4*1024*1024, float64(n)*.25/ended.Sub(begin).Seconds())))
			}
		}(i == 0)
	}
	wg.Wait()
	elapsed := time.Since(started)
	m.ElapsedMillis = elapsed.Milliseconds()
	m.TotalBytes, m.SentBytes = s.confirmed, s.sent.Load()
	m.ErrorCode, m.HTTPCode = s.code, s.status
	if gate.Passed() {
		m.SourceHealth = "passed"
		m.ErrorPhase = "transfer"
	}
	if elapsed > 0 {
		m.Value = uint64(float64(m.TotalBytes) / elapsed.Seconds())
	}
	for i, n := range s.bins {
		window := math.Min(1, elapsed.Seconds()-float64(i))
		if window <= 0 {
			break
		}
		// Omit very short final bins from peak/curve calculation; their bytes
		// remain included in the total and average.
		if window < .1 && i > 0 {
			break
		}
		v := uint64(n / window)
		m.Speeds = append(m.Speeds, v)
		m.Max = max(m.Max, v)
	}
	switch {
	case vendors.Context(proxy).Err() != nil:
		m.StopReason = "cancelled"
		m.ErrorCode = ""
	case m.ErrorCode != "":
		m.StopReason = "upload_error"
	case m.TotalBytes == 0:
		m.ErrorCode = "incomplete_upload"
		if ctx.Err() != nil {
			m.ErrorCode = vendors.NetworkErrorCode(ctx.Err())
		}
	case cfg.DownloadBytes > 0 && m.TotalBytes == cfg.DownloadBytes:
		m.StopReason = "byte_limit"
	case ctx.Err() != nil:
		m.StopReason = "duration"
	default:
		m.ErrorCode = "incomplete_upload"
	}

	if m.ErrorCode == "" {
		m.ErrorPhase = ""
	}
}
