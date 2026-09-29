package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/silencoo/speed-probe/auth"
	"github.com/silencoo/speed-probe/utils"
)

type AgentConfig struct {
	Version      int      `json:"version"`
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Controller   string   `json:"controller"`
	Token        string   `json:"token"`
	Capabilities []string `json:"capabilities"`
	MaxNodes     int      `json:"max_nodes"`
	MaxJobs      int      `json:"max_jobs"`
	MaxSeconds   int      `json:"max_seconds"`
	MaxScripts   int      `json:"max_scripts"`
}

type agentFrame struct {
	Version     int             `json:"version"`
	Type        string          `json:"type"`
	ID          string          `json:"id,omitempty"`
	StreamID    string          `json:"stream_id,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Description *Description    `json:"description,omitempty"`
}

var streamIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type agentStream struct {
	id       string
	link     *agentLink
	input    chan []byte
	done     chan struct{}
	once     sync.Once
	deadline time.Time
}

func (s *agentStream) SetReadLimit(_ int64)               {}
func (s *agentStream) SetWriteDeadline(_ time.Time) error { return nil }
func (s *agentStream) SetReadDeadline(t time.Time) error  { s.deadline = t; return nil }
func (s *agentStream) ReadMessage() (int, []byte, error) {
	var timeout <-chan time.Time
	if !s.deadline.IsZero() {
		timer := time.NewTimer(time.Until(s.deadline))
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case <-s.done:
		return 0, nil, io.EOF
	case data := <-s.input:
		return websocket.TextMessage, data, nil
	case <-timeout:
		return 0, nil, context.DeadlineExceeded
	}
}
func (s *agentStream) WriteJSON(value interface{}) error {
	select {
	case <-s.done:
		return io.EOF
	default:
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.link.write(agentFrame{Type: "data", StreamID: s.id, Payload: data})
}
func (s *agentStream) Close() error {
	s.once.Do(func() {
		close(s.done)
		s.link.mu.Lock()
		delete(s.link.streams, s.id)
		s.link.mu.Unlock()
		_ = s.link.write(agentFrame{Type: "close", StreamID: s.id})
	})
	return nil
}

type agentLink struct {
	conn    *websocket.Conn
	writer  sync.Mutex
	mu      sync.Mutex
	streams map[string]*agentStream
}

func (l *agentLink) write(frame agentFrame) error {
	l.writer.Lock()
	defer l.writer.Unlock()
	frame.Version = 1
	l.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return l.conn.WriteJSON(frame)
}

// RunAgent never publishes a listener. Each stream uses the same v3 executor.
func RunAgent(ctx context.Context, cfg AgentConfig, client auth.Client, catalog ScriptCatalog) error {
	manager, err := auth.NewStaticManager(client)
	if err != nil {
		return err
	}
	delay := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err = agentConnect(ctx, cfg, client, manager, catalog)
		if ctx.Err() != nil {
			break
		}
		// Do not log URLs, credentials, response bodies or untrusted close reasons.
		if errors.Is(err, errRegistrationPending) {
			utils.DWarnf("Registration pending; approve the fingerprint in Bot using /backend pending")
		} else if errors.Is(err, errRegistrationRejected) {
			utils.DWarnf("Registration rejected or identity revoked; check Bot approval and local identity")
		} else {
			utils.DWarnf("Controller connection lost or unavailable; reconnecting")
		}
		if time.Since(started) > time.Minute {
			delay = time.Second
		}
		timer := time.NewTimer(delay + time.Duration(rand.Int63n(int64(delay/2)+1)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
	return nil
}

func agentConnect(ctx context.Context, cfg AgentConfig, client auth.Client, manager *auth.Manager, catalog ScriptCatalog) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, Proxy: http.ProxyFromEnvironment}
	conn, response, err := dialer.DialContext(ctx, cfg.Controller, http.Header{"Authorization": []string{"Bearer " + cfg.Token}})
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		if response != nil && response.StatusCode == http.StatusUnauthorized && cfg.Name != "" {
			return registerAgent(ctx, cfg)
		}
		return errors.New("controller connection failed")
	}
	defer conn.Close()
	link := &agentLink{conn: conn, streams: map[string]*agentStream{}}
	linkCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	defer func() {
		cancel()
		conn.Close()
		link.mu.Lock()
		streams := make([]*agentStream, 0, len(link.streams))
		for _, s := range link.streams {
			streams = append(streams, s)
		}
		link.mu.Unlock()
		for _, s := range streams {
			_ = s.Close()
		}
		workers.Wait()
	}()
	go func() { <-linkCtx.Done(); conn.Close() }()
	conn.SetReadLimit(16 << 20)
	conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	conn.SetPingHandler(func(data string) error {
		conn.SetReadDeadline(time.Now().Add(45 * time.Second))
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(10*time.Second))
	})
	description := catalog.Describe(client)
	if err = link.write(agentFrame{Type: "hello", ID: cfg.ID, Description: &description}); err != nil {
		return err
	}
	var ready agentFrame
	if err = conn.ReadJSON(&ready); err != nil || ready.Version != 1 || ready.Type != "ready" {
		return errors.New("controller rejected registration")
	}
	utils.DWarnf("Controller connected; ready for tasks")
	for {
		var frame agentFrame
		if err = conn.ReadJSON(&frame); err != nil {
			return err
		}
		conn.SetReadDeadline(time.Now().Add(45 * time.Second))
		if frame.Version != 1 || !streamIDPattern.MatchString(frame.StreamID) {
			return errors.New("invalid reverse frame")
		}
		link.mu.Lock()
		stream := link.streams[frame.StreamID]
		count := len(link.streams)
		link.mu.Unlock()
		switch frame.Type {
		case "open":
			if stream != nil || count >= client.MaxJobs+2 {
				return errors.New("too many reverse streams")
			}
			stream = &agentStream{id: frame.StreamID, link: link, input: make(chan []byte, 2), done: make(chan struct{})}
			link.mu.Lock()
			link.streams[frame.StreamID] = stream
			link.mu.Unlock()
			workers.Add(1)
			go func(s *agentStream) { defer workers.Done(); ServeSession(linkCtx, s, manager, catalog, client) }(stream)
		case "data":
			if stream == nil {
				continue
			}
			select {
			case stream.input <- frame.Payload:
			case <-stream.done:
			default:
				return errors.New("reverse stream overflow")
			}
		case "close":
			if stream != nil {
				_ = stream.Close()
			}
		default:
			return errors.New("unknown reverse frame")
		}
	}
}

var errRegistrationPending = errors.New("registration pending")
var errRegistrationRejected = errors.New("registration rejected")

func registerAgent(ctx context.Context, cfg AgentConfig) error {
	// Same HTTPS path as the WebSocket, so existing ingress rules also work.
	address := "https" + cfg.Controller[3:]
	if cfg.Controller[:3] == "ws:" {
		address = "http" + cfg.Controller[2:]
	}
	data, _ := json.Marshal(map[string]interface{}{"version": 1, "id": cfg.ID, "name": cfg.Name})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("registration unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusAccepted || response.StatusCode == http.StatusOK {
		return errRegistrationPending
	}
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusConflict {
		return errRegistrationRejected
	}
	return errors.New("registration unavailable")
}
