package service

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/silencoo/speed-probe/auth"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/service/taskpoll"
	"github.com/silencoo/speed-probe/utils"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testBackend(t *testing.T) (string, string, *auth.Manager, auth.Client) {
	t.Helper()
	old := utils.GCFG
	utils.GCFG = utils.GlobalConfig{}
	t.Cleanup(func() { utils.GCFG = old })
	token := "test." + strings.Repeat("ab", 32)
	c := auth.Client{ID: "test", TokenHash: auth.HashToken(token), Capabilities: []string{"ping", "script", "custom_script"}, MaxNodes: 3, MaxJobs: 1, MaxSeconds: 10, MaxScripts: 2}
	m := auth.NewManager(filepath.Join(t.TempDir(), "clients.json"))
	if err := auth.Save(m.Path, auth.Store{Clients: []auth.Client{c}}); err != nil {
		t.Fatal(err)
	}
	ConnTaskPoll = taskpoll.NewTaskPollController("test", 1, 0, 0)
	SpeedTaskPoll = taskpoll.NewTaskPollController("speed", 1, 0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go ConnTaskPoll.Run(ctx)
	go SpeedTaskPoll.Run(ctx)
	server := httptest.NewServer(NewHandler(m, ScriptCatalog{{ID: "installed", Type: interfaces.STypeMedia, Content: "function handler(){return 'ok'}", TimeoutMillis: 1000}}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http"), token, m, c
}
func connect(t *testing.T, url, token string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": []string{"Bearer " + token}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func read(t *testing.T, c *websocket.Conn) Event {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	var event Event
	if err := c.ReadJSON(&event); err != nil {
		t.Fatal(err)
	}
	if event.Protocol != 3 {
		t.Fatal("wrong protocol")
	}
	return event
}
func request(script string) *interfaces.SlaveRequest {
	r := &interfaces.SlaveRequest{Vendor: interfaces.VendorLocal, Nodes: []interfaces.SlaveRequestNode{{Name: "duplicate"}, {Name: "duplicate"}}}
	if script != "" {
		r.Configs.Scripts = []interfaces.Script{{ID: "custom", Type: interfaces.STypeMedia, Content: script, TimeoutMillis: 60000}}
		r.Options.Matrices = []interfaces.SlaveRequestMatrixEntry{{Type: interfaces.MatrixScriptTest, Params: "custom"}}
	}
	return r
}
func TestDescribeAndUnauthorizedHandshake(t *testing.T) {
	url, token, _, _ := testBackend(t)
	_, response, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil || response.StatusCode != 401 {
		t.Fatal("unauthenticated upgrade allowed")
	}
	c := connect(t, url, token)
	c.WriteJSON(Command{Protocol: 3, Type: "describe"})
	e := read(t, c)
	if e.Type != "description" || e.Description.SoftwareVersion == "" || len(e.Description.Scripts) != 1 || e.Description.Limits.MaxJobs != 1 {
		t.Fatalf("bad description: %+v", e)
	}
}
func TestTaskContractAndNoRequestEcho(t *testing.T) {
	url, token, _, _ := testBackend(t)
	c := connect(t, url, token)
	c.WriteJSON(Command{Protocol: 3, Type: "run", TaskID: "a", Request: request("")})
	if e := read(t, c); e.Type != "accepted" {
		t.Fatal(e)
	}
	for {
		e := read(t, c)
		if e.Type == "finished" {
			if e.State != "succeeded" || len(e.Results) != 2 || e.Results[0].Index != 0 || e.Results[1].Index != 1 {
				t.Fatalf("bad result: %+v", e)
			}
			data, _ := json.Marshal(e)
			if strings.Contains(string(data), "\"Request\"") {
				t.Fatal("request echoed")
			}
			break
		}
	}
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("connection remained open after terminal")
	}
}
func TestExplicitCancelAndCapacityRelease(t *testing.T) {
	url, token, _, _ := testBackend(t)
	c := connect(t, url, token)
	c.WriteJSON(Command{Protocol: 3, Type: "run", TaskID: "slow", Request: request("function handler(){while(true){}}")})
	if e := read(t, c); e.Type != "accepted" {
		t.Fatal(e)
	}
	second := connect(t, url, token)
	second.WriteJSON(Command{Protocol: 3, Type: "run", TaskID: "second", Request: request("")})
	if e := read(t, second); e.Error == nil || e.Error.Code != "capacity_exceeded" {
		t.Fatal(e)
	}
	c.WriteJSON(Command{Protocol: 3, Type: "cancel", TaskID: "slow"})
	for {
		e := read(t, c)
		if e.Type == "finished" {
			if e.State != "cancelled" {
				t.Fatal(e)
			}
			break
		}
	}
	next := connect(t, url, token)
	next.WriteJSON(Command{Protocol: 3, Type: "run", TaskID: "next", Request: request("")})
	if e := read(t, next); e.Type != "accepted" {
		t.Fatal("capacity not released", e)
	}
	for {
		if e := read(t, next); e.Type == "finished" {
			break
		}
	}
}
func TestRevocationAndDeadlineAreNotSuccess(t *testing.T) {
	for _, mode := range []string{"revoke", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			url, token, m, client := testBackend(t)
			if mode == "deadline" {
				client.MaxSeconds = 1
				auth.Save(m.Path, auth.Store{Clients: []auth.Client{client}})
			}
			c := connect(t, url, token)
			c.WriteJSON(Command{Protocol: 3, Type: "run", TaskID: "slow", Request: request("function handler(){while(true){}}")})
			if e := read(t, c); e.Type != "accepted" {
				t.Fatal(e)
			}
			if mode == "revoke" {
				client.Disabled = true
				auth.Save(m.Path, auth.Store{Clients: []auth.Client{client}})
			}
			for {
				e := read(t, c)
				if e.Type == "finished" {
					expected := "permission_revoked"
					if mode == "deadline" {
						expected = "deadline_exceeded"
					}
					if e.State != "failed" || e.Error == nil || e.Error.Code != expected {
						t.Fatalf("unexpected terminal %+v", e)
					}
					if len(e.Results) != 0 {
						t.Fatal("cancelled work became fake results")
					}
					break
				}
			}
		})
	}
}
func TestInstalledScriptNeedsNoUploadPermission(t *testing.T) {
	url, token, m, client := testBackend(t)
	client.Capabilities = []string{"script"}
	auth.Save(m.Path, auth.Store{Clients: []auth.Client{client}})
	r := request("")
	r.Configs.Scripts = []interfaces.Script{{ID: "installed", Type: interfaces.STypeMedia}}
	r.Options.Matrices = []interfaces.SlaveRequestMatrixEntry{{Type: interfaces.MatrixScriptTest, Params: "installed"}}
	c := connect(t, url, token)
	c.WriteJSON(Command{Protocol: 3, Type: "run", TaskID: "installed", Request: r})
	if e := read(t, c); e.Type != "accepted" {
		t.Fatal(e)
	}
	for {
		e := read(t, c)
		if e.Type == "finished" {
			if e.State != "succeeded" {
				t.Fatal(e)
			}
			break
		}
	}
	r.Configs.Scripts[0].Content = "function handler(){return 'forged'}"
	c2 := connect(t, url, token)
	c2.WriteJSON(Command{Protocol: 3, Type: "run", TaskID: "upload", Request: r})
	if e := read(t, c2); e.Error == nil || e.Error.Code != "permission_denied" {
		t.Fatal(e)
	}
}

func TestDisconnectCancelsHostFetchAndReleasesQuota(t *testing.T) {
	url, token, manager, client := testBackend(t)
	started := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer slow.Close()
	c := connect(t, url, token)
	source := "function handler(){fetch('" + slow.URL + "',{useHost:true,timeout:30000});return 'done'}"
	req := request(source)
	req.Nodes = req.Nodes[:1]
	c.WriteJSON(Command{Protocol: 3, Type: "run", TaskID: "disconnect", Request: req})
	if e := read(t, c); e.Type != "accepted" {
		t.Fatal(e)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		c.Close()
		t.Fatal("script did not reach local HTTP server")
	}
	c.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if release, err := manager.Acquire(client); err == nil {
			release()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("disconnected task retained quota")
}

func TestSharedPythonProtocolFixture(t *testing.T) {
	data, err := os.ReadFile("../auth/testdata/v3.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]json.RawMessage
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	cmd, err := decodeCommand(fixture["run"])
	if err != nil || cmd.Type != "run" || cmd.Request.Nodes[0].Name != "测试 & <node>" {
		t.Fatalf("wire mismatch: %+v %v", cmd, err)
	}
	var event Event
	if err = json.Unmarshal(fixture["finished"], &event); err != nil {
		t.Fatal(err)
	}
	if event.Protocol != 3 || event.Results[0].Index != 0 || event.State != "succeeded" {
		t.Fatal("result fixture mismatch")
	}
}
