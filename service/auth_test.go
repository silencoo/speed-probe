package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/silencoo/speed-probe/auth"
	"github.com/silencoo/speed-probe/utils"
)

func TestUnsignedRequestsAndMissingStoreAreRejected(t *testing.T) {
	old := utils.GCFG
	defer func() { utils.GCFG = old }()
	utils.GCFG = utils.GlobalConfig{ClientsFile: filepath.Join(t.TempDir(), "absent.json")}
	if ValidateAuthConfig() == nil {
		t.Fatal("missing credentials file accepted")
	}
	manager := auth.NewManager(utils.GCFG.ClientsFile)
	for _, data := range []string{`{}`, `{"Challenge":"old-token","Basics":{"Invoker":"123"}}`, `{"version":1}`, `null`} {
		if _, _, err := authenticate([]byte(data), manager); err == nil {
			t.Fatalf("unsigned request accepted: %s", data)
		}
	}
}

func TestV2CapabilityEnforcement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	c := auth.Client{ID: "test", Secret: strings.Repeat("ab", 32), Capabilities: []string{"ping"}, MaxNodes: 5, MaxJobs: 1}
	if err := auth.Save(path, auth.Store{Clients: []auth.Client{c}}); err != nil {
		t.Fatal(err)
	}
	e := auth.Envelope{Version: 2, ClientID: c.ID, Timestamp: time.Now().Unix(), Nonce: strings.Repeat("01", 16), Payload: `{"Options":{"Matrices":[{"Type":"SPEED_MAX"}]}}`}
	e.Signature = auth.Sign(c.Secret, e)
	data, _ := json.Marshal(e)
	if _, _, err := authenticate(data, auth.NewManager(path)); err == nil {
		t.Fatal("unauthorized speed request passed")
	}
	e.Payload = `{"Nodes":[],"Options":{"Matrices":[]}}`
	e.Signature = auth.Sign(c.Secret, e)
	data, _ = json.Marshal(e)
	if _, client, err := authenticate(data, auth.NewManager(path)); err != nil || client.ID != c.ID {
		t.Fatalf("health request failed: %v", err)
	}
}
