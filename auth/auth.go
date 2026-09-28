// Package auth implements versioned client authentication, independent of Telegram.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/silencoo/speed-probe/interfaces"
)

var Capabilities = []string{"ping", "script", "topo", "speed"}
var validID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)

type Client struct {
	ID           string   `json:"id"`
	Secret       string   `json:"secret"`
	Capabilities []string `json:"capabilities"`
	Disabled     bool     `json:"disabled"`
	MaxNodes     int      `json:"max_nodes"`
	MaxJobs      int      `json:"max_jobs"`
}
type Store struct {
	Clients []Client `json:"clients"`
}
type Envelope struct {
	Version   int    `json:"version"`
	ClientID  string `json:"client_id"`
	Timestamp int64  `json:"timestamp"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"`
	// A string preserves exactly the bytes signed by clients in other languages.
	Payload string `json:"payload"`
}
type Connection struct {
	Version  int    `json:"version"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	ClientID string `json:"client_id"`
	Secret   string `json:"secret"`
}

func Has(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func ValidateClient(c Client) error {
	if !validID.MatchString(c.ID) {
		return errors.New("client ID must contain 1-40 letters, digits, _ or -")
	}
	key, err := hex.DecodeString(c.Secret)
	if err != nil || len(key) != 32 {
		return errors.New("client secret must be 32 random bytes encoded as hex")
	}
	if c.MaxNodes < 1 || c.MaxJobs < 1 {
		return errors.New("max_nodes and max_jobs must be positive")
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
	if err = json.Unmarshal(data, &s); err != nil {
		return s, errors.New("invalid client store JSON")
	}
	seen := map[string]bool{}
	for _, c := range s.Clients {
		if err = ValidateClient(c); err != nil {
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
	for _, c := range s.Clients {
		if err := ValidateClient(c); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".clients-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
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
	return os.Rename(name, path)
}
func NewSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func Sign(secret string, e Envelope) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "speed-probe/v2\n%s\n%s\n%s\n%s", e.ClientID, strconv.FormatInt(e.Timestamp, 10), e.Nonce, e.Payload)
	return hex.EncodeToString(mac.Sum(nil))
}
func Required(req *interfaces.SlaveRequest) ([]string, error) {
	caps := []string{}
	// Custom IP scripts execute code too, even without a TEST_SCRIPT matrix.
	for _, script := range req.Configs.Scripts {
		if script.Content != "" {
			caps = append(caps, "script")
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
		return errors.New("client is disabled")
	}
	caps, err := Required(req)
	if err != nil {
		return err
	}
	for _, cap := range caps {
		if !Has(c.Capabilities, cap) {
			return fmt.Errorf("client is not allowed to run %s tests", cap)
		}
	}
	if len(req.Nodes) > c.MaxNodes {
		return errors.New("client node limit exceeded")
	}
	return nil
}

type Manager struct {
	Path   string
	mu     sync.Mutex
	seen   map[string]int64
	active map[string]int
}

func NewManager(path string) *Manager {
	return &Manager{Path: path, seen: map[string]int64{}, active: map[string]int{}}
}
func (m *Manager) Current(id string) (Client, error) {
	s, err := Load(m.Path)
	if err != nil {
		return Client{}, errors.New("client store unavailable")
	}
	for _, c := range s.Clients {
		if c.ID == id && !c.Disabled {
			return c, nil
		}
	}
	return Client{}, errors.New("unknown or disabled client")
}
func (m *Manager) Verify(e Envelope, now time.Time) (Client, error) {
	if e.Version != 2 || !validID.MatchString(e.ClientID) || len(e.Nonce) != 32 {
		return Client{}, errors.New("invalid authentication envelope")
	}
	if _, err := hex.DecodeString(e.Nonce); err != nil {
		return Client{}, errors.New("invalid nonce")
	}
	if e.Timestamp < now.Unix()-120 || e.Timestamp > now.Unix()+120 {
		return Client{}, errors.New("request expired or clock skew exceeds 120 seconds")
	}
	c, err := m.Current(e.ClientID)
	if err != nil {
		return c, err
	}
	actual, err := hex.DecodeString(e.Signature)
	expected, _ := hex.DecodeString(Sign(c.Secret, e))
	if err != nil || !hmac.Equal(actual, expected) {
		return Client{}, errors.New("invalid request signature")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, expiry := range m.seen {
		if expiry < now.Unix() {
			delete(m.seen, key)
		}
	}
	key := c.ID + ":" + e.Nonce
	if _, exists := m.seen[key]; exists {
		return Client{}, errors.New("request already used")
	}
	if len(m.seen) >= 100000 {
		return Client{}, errors.New("authentication capacity reached")
	}
	m.seen[key] = e.Timestamp + 120
	return c, nil
}
func (m *Manager) Acquire(c Client) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active[c.ID] >= c.MaxJobs {
		return nil, errors.New("client concurrent task limit exceeded")
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
	if !hmac.Equal([]byte(current.Secret), []byte(c.Secret)) {
		return errors.New("client credential was rotated")
	}
	return current.Allows(req)
}
