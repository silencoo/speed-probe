package auth

import (
	"github.com/silencoo/speed-probe/interfaces"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func setup(t *testing.T) (*Manager, Client, string) {
	t.Helper()
	token := "test." + strings.Repeat("ab", 32)
	c := Client{ID: "test", TokenHash: HashToken(token), Capabilities: []string{"ping"}, MaxNodes: 2, MaxJobs: 1, MaxSeconds: 60, MaxScripts: 2}
	path := filepath.Join(t.TempDir(), "clients.json")
	if err := Save(path, Store{Clients: []Client{c}}); err != nil {
		t.Fatal(err)
	}
	return NewManager(path), c, token
}
func TestTokenAuthenticationAndHashedStorage(t *testing.T) {
	m, c, token := setup(t)
	if _, err := m.Verify(token); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", c.ID, token + "x", "other." + strings.Repeat("ab", 32), "test." + strings.Repeat("cd", 32)} {
		if _, err := m.Verify(bad); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	data, _ := os.ReadFile(m.Path)
	if strings.Contains(string(data), token) || strings.Contains(string(data), strings.Repeat("ab", 32)) {
		t.Fatal("plaintext credential stored")
	}
}
func TestPolicyReloadRotationAndFailClosed(t *testing.T) {
	m, c, token := setup(t)
	m.Verify(token)
	c.Capabilities = []string{}
	Save(m.Path, Store{Clients: []Client{c}})
	req := &interfaces.SlaveRequest{Options: interfaces.SlaveRequestOptions{Matrices: []interfaces.SlaveRequestMatrixEntry{{Type: interfaces.MatrixHTTPPing}}}}
	if m.StillAllowed(c, req) == nil {
		t.Fatal("policy update ignored")
	}
	original := c
	c.TokenHash = HashToken("test." + strings.Repeat("cd", 32))
	Save(m.Path, Store{Clients: []Client{c}})
	if m.StillAllowed(original, &interfaces.SlaveRequest{}) == nil {
		t.Fatal("old token survived rotation")
	}
	c.Disabled = true
	Save(m.Path, Store{Clients: []Client{c}})
	if _, err := m.Current(c.ID); err == nil {
		t.Fatal("revocation ignored")
	}
	os.WriteFile(m.Path, []byte("broken"), 0600)
	if _, err := m.Current(c.ID); err == nil {
		t.Fatal("invalid store failed open")
	}
}
func TestConcurrentQuotaAndIdempotentRelease(t *testing.T) {
	m, c, _ := setup(t)
	var successes atomic.Int32
	var wg sync.WaitGroup
	releases := make(chan func(), 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if release, err := m.Acquire(c); err == nil {
				successes.Add(1)
				releases <- release
			}
		}()
	}
	wg.Wait()
	close(releases)
	if successes.Load() != 1 {
		t.Fatalf("admitted %d tasks", successes.Load())
	}
	for release := range releases {
		release()
		release()
	}
	release, err := m.Acquire(c)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
func TestScriptPermissionsAndLimits(t *testing.T) {
	_, c, _ := setup(t)
	req := &interfaces.SlaveRequest{Configs: interfaces.SlaveRequestConfigs{Scripts: []interfaces.Script{{ID: "installed", Type: interfaces.STypeMedia}}}, Options: interfaces.SlaveRequestOptions{Matrices: []interfaces.SlaveRequestMatrixEntry{{Type: interfaces.MatrixScriptTest, Params: "installed"}}}}
	c.Capabilities = []string{"script"}
	if err := c.Allows(req); err != nil {
		t.Fatal(err)
	}
	req.Configs.Scripts[0].Content = "code"
	if c.Allows(req) == nil {
		t.Fatal("script upload bypass")
	}
	c.Capabilities = append(c.Capabilities, "custom_script")
	if err := c.Allows(req); err != nil {
		t.Fatal(err)
	}
	req.Nodes = make([]interfaces.SlaveRequestNode, 3)
	if c.Allows(req) == nil {
		t.Fatal("node limit bypass")
	}
}
func TestLegacyStoreRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.json")
	os.WriteFile(path, []byte("{\"clients\":[]}"), 0600)
	if _, err := Load(path); err == nil {
		t.Fatal("v2 store accepted")
	}
}
