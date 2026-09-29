// Package auth authenticates API clients; it does not attest backend software.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/silencoo/speed-probe/interfaces"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const Protocol = 3

var Capabilities = []string{"ping", "script", "topo", "speed", "custom_script"}
var validID = regexp.MustCompile("^[a-zA-Z0-9_-]{1,64}$")

type Client struct {
	ID           string   `json:"id"`
	TokenHash    string   `json:"token_hash"`
	Capabilities []string `json:"capabilities"`
	Disabled     bool     `json:"disabled"`
	MaxNodes     int      `json:"max_nodes"`
	MaxJobs      int      `json:"max_jobs"`
	MaxSeconds   int      `json:"max_seconds"`
	MaxScripts   int      `json:"max_scripts"`
}
type Store struct {
	Version int      `json:"version"`
	Clients []Client `json:"clients"`
}
type Connection struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Token   string `json:"token"`
}

func Has(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func NewToken(id string) (string, error) {
	if !validID.MatchString(id) {
		return "", errors.New("invalid client ID")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return id + "." + hex.EncodeToString(b), nil
}
func ValidateClient(c Client) error {
	if !validID.MatchString(c.ID) {
		return errors.New("invalid client ID")
	}
	h, err := hex.DecodeString(c.TokenHash)
	if err != nil || len(h) != 32 {
		return errors.New("invalid token hash; create a v3 client")
	}
	if c.MaxNodes < 1 || c.MaxNodes > 10000 || c.MaxJobs < 1 || c.MaxJobs > 100 || c.MaxSeconds < 1 || c.MaxSeconds > 3600 || c.MaxScripts < 1 || c.MaxScripts > 64 {
		return errors.New("limits: nodes 1-10000, jobs 1-100, seconds 1-3600, scripts 1-64")
	}
	for _, cap := range c.Capabilities {
		if !Has(Capabilities, cap) {
			return fmt.Errorf("unknown capability: %s", cap)
		}
	}
	return nil
}
func Load(path string) (Store, error) {
	var s Store
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if json.Unmarshal(data, &s) != nil || s.Version != Protocol {
		return s, errors.New("expected v3 client store; recreate credentials")
	}
	seen := map[string]bool{}
	for _, c := range s.Clients {
		if err := ValidateClient(c); err != nil {
			return s, err
		}
		if seen[c.ID] {
			return s, errors.New("duplicate client ID")
		}
		seen[c.ID] = true
	}
	return s, nil
}
func Save(path string, s Store) error {
	s.Version = Protocol
	seen := map[string]bool{}
	for _, c := range s.Clients {
		if err := ValidateClient(c); err != nil {
			return err
		}
		if seen[c.ID] {
			return errors.New("duplicate client ID")
		}
		seen[c.ID] = true
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".clients-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
func Required(req *interfaces.SlaveRequest) ([]string, error) {
	caps := []string{}
	for _, s := range req.Configs.Scripts {
		if s.Content != "" {
			caps = append(caps, "custom_script")
			break
		}
	}
	for _, m := range req.Options.Matrices {
		var cap string
		switch m.Type {
		case interfaces.MatrixHTTPPing, interfaces.MatrixRTTPing, interfaces.MatrixUDPType:
			cap = "ping"
		case interfaces.MatrixScriptTest:
			cap = "script"
		case interfaces.MatrixInboundGeoIP, interfaces.MatrixOutboundGeoIP:
			cap = "topo"
		case interfaces.MatrixAverageSpeed, interfaces.MatrixMaxSpeed, interfaces.MatrixPerSecondSpeed:
			cap = "speed"
		default:
			return nil, errors.New("unknown test type")
		}
		if !Has(caps, cap) {
			caps = append(caps, cap)
		}
	}
	return caps, nil
}
func (c Client) Allows(req *interfaces.SlaveRequest) error {
	if c.Disabled {
		return errors.New("client disabled")
	}
	caps, err := Required(req)
	if err != nil {
		return err
	}
	for _, cap := range caps {
		if !Has(c.Capabilities, cap) {
			return fmt.Errorf("capability not allowed: %s", cap)
		}
	}
	if len(req.Nodes) > c.MaxNodes {
		return errors.New("node limit exceeded")
	}
	if len(req.Configs.Scripts) > c.MaxScripts {
		return errors.New("script limit exceeded")
	}
	return nil
}

// Snapshot reloads on atomic file replacement. Invalid updates fail closed.
type Manager struct {
	Path    string
	static  bool
	mu      sync.Mutex
	info    os.FileInfo
	clients map[string]Client
	active  map[string]int
}

func NewManager(path string) *Manager { return &Manager{Path: path, active: map[string]int{}} }

// NewStaticManager enforces the operator's local agent policy across reconnects.
func NewStaticManager(client Client) (*Manager, error) {
	if err := ValidateClient(client); err != nil {
		return nil, err
	}
	return &Manager{static: true, clients: map[string]Client{client.ID: client}, active: map[string]int{}}, nil
}
func (m *Manager) currentLocked(id string) (Client, error) {
	if !m.static {
		info, err := os.Stat(m.Path)
		if err != nil {
			return Client{}, errors.New("client store unavailable")
		}
		if m.info == nil || !os.SameFile(info, m.info) || info.ModTime() != m.info.ModTime() || info.Size() != m.info.Size() {
			s, err := Load(m.Path)
			if err != nil {
				return Client{}, errors.New("client store unavailable")
			}
			m.clients = map[string]Client{}
			for _, c := range s.Clients {
				m.clients[c.ID] = c
			}
			m.info = info
		}
	}
	c, ok := m.clients[id]
	if !ok || c.Disabled {
		return Client{}, errors.New("unknown or disabled client")
	}
	c.Capabilities = append([]string{}, c.Capabilities...)
	return c, nil
}
func (m *Manager) Current(id string) (Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentLocked(id)
}
func (m *Manager) Verify(token string) (Client, error) {
	id, secret, ok := strings.Cut(token, ".")
	if !ok || !validID.MatchString(id) || len(secret) != 64 {
		return Client{}, errors.New("invalid credentials")
	}
	if _, err := hex.DecodeString(secret); err != nil {
		return Client{}, errors.New("invalid credentials")
	}
	c, err := m.Current(id)
	if err != nil || subtle.ConstantTimeCompare([]byte(c.TokenHash), []byte(HashToken(token))) != 1 {
		return Client{}, errors.New("invalid credentials")
	}
	return c, nil
}
func (m *Manager) Acquire(c Client) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, err := m.currentLocked(c.ID)
	if err != nil || current.TokenHash != c.TokenHash {
		return nil, errors.New("credentials changed")
	}
	if m.active[c.ID] >= current.MaxJobs {
		return nil, errors.New("concurrent task limit exceeded")
	}
	m.active[c.ID]++
	var once sync.Once
	return func() { once.Do(func() { m.mu.Lock(); defer m.mu.Unlock(); m.active[c.ID]-- }) }, nil
}
func (m *Manager) StillAllowed(c Client, req *interfaces.SlaveRequest) error {
	current, err := m.Current(c.ID)
	if err != nil {
		return err
	}
	if current.TokenHash != c.TokenHash {
		return errors.New("credential rotated")
	}
	return current.Allows(req)
}
