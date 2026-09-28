package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/silencoo/speed-probe/interfaces"
)

func setup(t *testing.T) (*Manager, Client, Envelope) {
	t.Helper()
	c := Client{ID: "test", Secret: strings.Repeat("ab", 32), Capabilities: []string{"ping"}, MaxNodes: 2, MaxJobs: 1}
	path := filepath.Join(t.TempDir(), "clients.json")
	if err := Save(path, Store{Clients: []Client{c}}); err != nil {
		t.Fatal(err)
	}
	e := Envelope{Version: 2, ClientID: c.ID, Timestamp: 1700000000, Nonce: strings.Repeat("01", 16), Payload: `{"Vendor":"clash","Nodes":[]}`}
	e.Signature = Sign(c.Secret, e)
	return NewManager(path), c, e
}
func TestSignatureReplayAndTamper(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, name := range []string{"valid", "vendor", "signature", "expired", "future", "nonce", "unknown", "version"} {
		t.Run(name, func(t *testing.T) {
			m, c, e := setup(t)
			switch name {
			case "vendor":
				e.Payload = strings.ReplaceAll(e.Payload, "clash", "local")
			case "signature":
				e.Signature = strings.Repeat("00", 32)
			case "expired":
				e.Timestamp -= 121
				e.Signature = Sign(c.Secret, e)
			case "future":
				e.Timestamp += 121
				e.Signature = Sign(c.Secret, e)
			case "nonce":
				e.Nonce = "bad"
				e.Signature = Sign(c.Secret, e)
			case "unknown":
				e.ClientID = "other"
				e.Signature = Sign(c.Secret, e)
			case "version":
				e.Version = 3
			}
			_, err := m.Verify(e, now)
			if (err == nil) != (name == "valid") {
				t.Fatalf("unexpected verify result: %v", err)
			}
			if name == "valid" {
				if _, err = m.Verify(e, now); err == nil {
					t.Fatal("replay accepted")
				}
			}
		})
	}
}
func TestConcurrentReplay(t *testing.T) {
	m, _, e := setup(t)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.Verify(e, time.Unix(e.Timestamp, 0)); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("accepted %d copies", successes.Load())
	}
}
func TestRevocationRotationAndFailClosed(t *testing.T) {
	m, c, e := setup(t)
	if _, err := m.Verify(e, time.Unix(e.Timestamp, 0)); err != nil {
		t.Fatal(err)
	}
	changed := c
	changed.Disabled = true
	if err := Save(m.Path, Store{Clients: []Client{changed}}); err != nil {
		t.Fatal(err)
	}
	if err := m.StillAllowed(c, &interfaces.SlaveRequest{}); err == nil {
		t.Fatal("revoked queued request accepted")
	}
	changed.Disabled = false
	changed.Secret = strings.Repeat("cd", 32)
	Save(m.Path, Store{Clients: []Client{changed}})
	if err := m.StillAllowed(c, &interfaces.SlaveRequest{}); err == nil {
		t.Fatal("old credential survived rotation")
	}
	os.WriteFile(m.Path, []byte("broken"), 0600)
	if _, err := m.Current(c.ID); err == nil {
		t.Fatal("malformed store accepted")
	}
}
func TestCapabilitiesLimitsAndRelease(t *testing.T) {
	m, c, _ := setup(t)
	req := &interfaces.SlaveRequest{Options: interfaces.SlaveRequestOptions{Matrices: []interfaces.SlaveRequestMatrixEntry{{Type: interfaces.MatrixAverageSpeed}}}}
	if c.Allows(req) == nil {
		t.Fatal("speed capability bypass")
	}
	req.Options.Matrices[0].Type = interfaces.MatrixHTTPPing
	if err := c.Allows(req); err != nil {
		t.Fatal(err)
	}
	req.Nodes = make([]interfaces.SlaveRequestNode, 3)
	if c.Allows(req) == nil {
		t.Fatal("node limit bypass")
	}
	req.Nodes = nil
	req.Options.Matrices = nil
	req.Configs.Scripts = []interfaces.Script{{Type: interfaces.STypeIP, Content: "custom code"}}
	if c.Allows(req) == nil {
		t.Fatal("custom IP script bypass")
	}
	release, err := m.Acquire(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Acquire(c); err == nil {
		t.Fatal("job limit bypass")
	}
	release()
	release()
	if _, err = m.Acquire(c); err != nil {
		t.Fatal(err)
	}
}
func TestPythonWireFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var e Envelope
	if err = json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	if Sign(strings.Repeat("ab", 32), e) != e.Signature {
		t.Fatal("Python/Go signature mismatch")
	}
}
