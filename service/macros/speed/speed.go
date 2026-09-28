package speed

import (
	"context"
	jsoniter "github.com/json-iterator/go"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/preconfigs"
	"github.com/silencoo/speed-probe/utils"
	"github.com/silencoo/speed-probe/vendors"
	"golang.org/x/time/rate"
	"io"
	"strings"
	"sync"
	"time"
)

// Budget reserves body bytes before reading so concurrent connections cannot
// each spend the full per-node allowance. Transport/TLS overhead is excluded.
type budget struct {
	mu                    sync.Mutex
	limit, reserved, used uint64
	exhausted             chan struct{}
	once                  sync.Once
}

func (b *budget) take(n int) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit == 0 {
		return n
	}
	left := b.limit - b.used - b.reserved
	if uint64(n) > left {
		n = int(left)
	}
	b.reserved += uint64(n)
	return n
}
func (b *budget) commit(reserved, n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit > 0 {
		b.reserved -= uint64(reserved)
	}
	b.used += uint64(n)
	if b.limit > 0 && b.used == b.limit {
		b.once.Do(func() { close(b.exhausted) })
	}
}

func Once(speed *Speed, proxy interfaces.Vendor, cfg *interfaces.SlaveRequestConfigs) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(vendors.Context(proxy), time.Duration(cfg.DownloadDuration)*time.Second)
	defer cancel()
	proxy = vendors.WithContext(ctx, proxy)
	files := RefetchDownloadFiles(proxy, cfg.DownloadURL)
	b := &budget{limit: cfg.DownloadBytes, exhausted: make(chan struct{})}
	counter := &WriteCounter{}
	var limiter *rate.Limiter
	if utils.GCFG.SpeedLimit > 0 {
		limiter = rate.NewLimiter(rate.Limit(utils.GCFG.SpeedLimit), 32*1024)
	}
	var wg sync.WaitGroup
	for i := uint(0); i < cfg.DownloadThreading; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); download(ctx, files, proxy, counter, b, limiter) }()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	speed.Speeds = []uint64{}
	last := started
	sample := func(end time.Time) {
		n := counter.Take()
		elapsed := end.Sub(last).Seconds()
		speed.TotalSize += n
		if elapsed > 0 && (n > 0 || elapsed >= .5) {
			rate := uint64(float64(n) / elapsed)
			speed.Speeds = append(speed.Speeds, rate)
			if rate > speed.MaxSpeed {
				speed.MaxSpeed = rate
			}
		}
		last = end
	}
	speed.StopReason = "source_exhausted"
loop:
	for {
		select {
		case now := <-ticker.C:
			sample(now)
		case <-b.exhausted:
			speed.StopReason = "byte_limit"
			break loop
		case <-ctx.Done():
			speed.StopReason = "duration"
			if vendors.Context(proxy).Err() == context.Canceled {
				speed.StopReason = "cancelled"
			}
			break loop
		case <-done:
			break loop
		}
	}
	cancel()
	<-done
	ended := time.Now()
	sample(ended)
	speed.ElapsedMillis = ended.Sub(started).Milliseconds()
	if speed.TotalSize > 0 {
		speed.AvgSpeed = uint64(float64(speed.TotalSize) / ended.Sub(started).Seconds())
	}
	// A completed last reader may win the select over the budget signal.
	if cfg.DownloadBytes > 0 && speed.TotalSize == cfg.DownloadBytes {
		speed.StopReason = "byte_limit"
	}
	if speed.TotalSize == 0 && speed.StopReason == "source_exhausted" {
		speed.StopReason = "download_error"
	}
}

func download(ctx context.Context, files []string, proxy interfaces.Vendor, wc *WriteCounter, b *budget, limiter *rate.Limiter) {
	if len(files) == 0 {
		return
	}
	for i := 0; ctx.Err() == nil; i++ {
		resp, _, err := vendors.RequestUnsafe(ctx, proxy, &interfaces.RequestOptions{URL: files[i%len(files)], Network: interfaces.ROptionsTCP})
		if err != nil {
			return
		}
		if resp.StatusCode != 200 && resp.StatusCode != 206 {
			resp.Body.Close()
			return
		}
		received := 0
		buf := make([]byte, 32*1024)
		for ctx.Err() == nil {
			size := b.take(len(buf))
			if size == 0 {
				select {
				case <-ctx.Done():
				case <-b.exhausted:
				case <-time.After(time.Millisecond):
					continue
				}
				break
			}
			if limiter != nil {
				if err = limiter.WaitN(ctx, size); err != nil {
					b.commit(size, 0)
					break
				}
			}
			n, readErr := resp.Body.Read(buf[:size])
			// Count before signaling budget exhaustion; final collection waits for workers.
			wc.Write(buf[:n])
			b.commit(size, n)
			received += n
			if readErr != nil {
				err = readErr
				break
			}
		}
		resp.Body.Close()
		if received == 0 || (err != nil && err != io.EOF) {
			return
		}
	}
}

// Kept as a focused downloader entry point for cancellation tests.
func SingleThread(files []string, proxy interfaces.Vendor, seconds int64, wc *WriteCounter) context.CancelFunc {
	ctx, cancel := context.WithTimeout(vendors.Context(proxy), time.Duration(seconds)*time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var limiter *rate.Limiter
		if wc.RateLimit > 0 {
			limiter = rate.NewLimiter(rate.Limit(wc.RateLimit), 32*1024)
		}
		download(ctx, files, proxy, wc, &budget{exhausted: make(chan struct{})}, limiter)
	}()
	return func() { cancel(); <-done }
}
func RefetchDownloadFiles(proxy interfaces.Vendor, file string) []string {
	defaultList := []string{preconfigs.SPEED_DEFAULT_LARGE_FILE_STATIC_MSFT}
	if proxy == nil || proxy.Status() == interfaces.VStatusNotReady {
		return defaultList
	}

	switch file {
	case preconfigs.SPEED_DEFAULT_LARGE_FILE_DYN_INTL:
		body, _, _ := vendors.RequestWithRetry(proxy, 1, 1000, &interfaces.RequestOptions{
			URL:     "https://ipinfo.io",
			NoRedir: true,
		})

		if strings.Contains(string(body), "Microsoft") {
			return []string{preconfigs.SPEED_DEFAULT_LARGE_FILE_STATIC_MSFT}
		} else {
			return []string{preconfigs.SPEED_DEFAULT_LARGE_FILE_STATIC_GOOGLE}
		}
	case preconfigs.SPEED_DEFAULT_LARGE_FILE_DYN_FAST:
		body, _, _ := vendors.RequestWithRetry(proxy, 3, 1000, &interfaces.RequestOptions{
			URL:     "https://api.fast.com/netflix/speedtest/v2?https=false&token=YXNkZmFzZGxmbnNkYWZoYXNkZmhrYWxm&urlCount=5",
			NoRedir: true,
		})
		url := jsoniter.Get(body, "targets", 0, "url").ToString()
		if url != "" {
			return []string{url}
		} else {
			return defaultList
		}
	}
	return []string{file}
}
