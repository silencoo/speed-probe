package sourcecheck

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
)

// One connection validates the endpoint before any other worker may send data.
type Gate struct {
	done   chan struct{}
	once   sync.Once
	passed atomic.Bool
}

func New() *Gate             { return &Gate{done: make(chan struct{})} }
func (g *Gate) Pass()        { g.once.Do(func() { g.passed.Store(true); close(g.done) }) }
func (g *Gate) Close()       { g.once.Do(func() { close(g.done) }) }
func (g *Gate) Passed() bool { return g.passed.Load() }
func (g *Gate) Wait(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-g.done:
		return g.Passed()
	}
}
func InvalidContent(contentType string, sample []byte) bool {
	contentType = strings.ToLower(strings.Split(contentType, ";")[0])
	if contentType == "text/html" || contentType == "application/json" || contentType == "application/xml" || contentType == "text/xml" {
		return true
	}
	s := strings.ToLower(strings.TrimSpace(string(sample)))
	return strings.HasPrefix(s, "<!doctype html") || strings.HasPrefix(s, "<html") || strings.HasPrefix(s, `{"error"`) || strings.HasPrefix(s, "<error")
}
