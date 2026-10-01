package service

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/silencoo/speed-probe/auth"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/internal/testproxy"
)

func TestPathTaskUsesExitForAllMatricesAndStopsOnMismatch(t *testing.T) {
	address, token, manager, client := testBackend(t)
	client.Capabilities = append(client.Capabilities, "speed")
	if err := auth.Save(manager.Path, auth.Store{Clients: []auth.Client{client}}); err != nil {
		t.Fatal(err)
	}
	ss := testproxy.New(t, "shadowsocks", "127.0.0.2")
	tls := testproxy.New(t, "anytls", "127.0.0.3")
	var down, up, ping, script atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if host != "127.0.0.3" {
			t.Errorf("path bypassed, source %s", host)
		}
		switch r.URL.Path {
		case "/ip":
			w.Write([]byte(host))
		case "/ping":
			ping.Add(1)
			w.WriteHeader(204)
		case "/script":
			script.Add(1)
			w.Write([]byte("through-exit"))
		default:
			if r.Method == "POST" {
				up.Add(1)
				io.Copy(io.Discard, r.Body)
				w.WriteHeader(204)
			} else {
				down.Add(1)
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Write(make([]byte, 8192))
			}
		}
	}))
	defer target.Close()
	path := &interfaces.TestPath{ID: "chain", Name: "Local chain", Hops: []interfaces.PathHop{ss.Hop, tls.Hop}, CheckURL: target.URL + "/ip", ExpectedIP: "127.0.0.3"}
	req := &interfaces.SlaveRequest{Vendor: interfaces.VendorSingBox, Nodes: []interfaces.SlaveRequestNode{{Name: path.Name, Path: path}},
		Configs: interfaces.SlaveRequestConfigs{DownloadURL: target.URL + "/download", UploadURL: target.URL + "/upload", PingAddress: target.URL + "/ping", PingAverageOver: 2, DownloadBytes: 4096, DownloadDuration: 1, DownloadThreading: 2, TaskRetry: 1, TaskTimeout: 2000, Scripts: []interfaces.Script{{ID: "local", Type: interfaces.STypeMedia, Content: `function handler(){var r=fetch(` + strconv.Quote(target.URL+"/script") + `,{detailed:true});return {text:r.body,status:'reachable'}}`, TimeoutMillis: 2000}}},
		Options: interfaces.SlaveRequestOptions{Matrices: []interfaces.SlaveRequestMatrixEntry{{Type: interfaces.MatrixHTTPPing}, {Type: interfaces.MatrixAverageSpeed}, {Type: interfaces.MatrixUploadSpeed}, {Type: interfaces.MatrixScriptTest, Params: "local"}}}}
	c := connect(t, address, token)
	c.WriteJSON(Command{Protocol: 3, Type: "run", TaskID: "path", Request: req})
	for {
		e := read(t, c)
		if e.Type != "finished" {
			continue
		}
		if e.State != "succeeded" || len(e.Results) != 1 {
			t.Fatalf("%+v", e)
		}
		r := e.Results[0]
		if r.Error != "" || r.ExitCheck == nil || r.ExitCheck.State != "passed" || len(r.Matrices) != 4 {
			t.Fatalf("%+v", r)
		}
		for _, m := range r.Matrices {
			var raw map[string]any
			if err := json.Unmarshal([]byte(m.Payload), &raw); err != nil {
				t.Fatal(err)
			}
			if m.Type == interfaces.MatrixAverageSpeed || m.Type == interfaces.MatrixUploadSpeed {
				if raw["TotalBytes"] != float64(4096) {
					t.Fatalf("wrong payload accounting: %+v", raw)
				}
			}
		}
		break
	}
	if down.Load() == 0 || up.Load() == 0 || ping.Load() != 2 || script.Load() != 1 {
		t.Fatalf("missing path traffic: %d/%d/%d/%d", down.Load(), up.Load(), ping.Load(), script.Load())
	}
	beforeDown, beforeUp := down.Load(), up.Load()
	req.Nodes[0].Path.ExpectedIP = "192.0.2.123"
	c2 := connect(t, address, token)
	c2.WriteJSON(Command{Protocol: 3, Type: "run", TaskID: "mismatch", Request: req})
	for {
		e := read(t, c2)
		if e.Type != "finished" {
			continue
		}
		if len(e.Results) != 1 || e.Results[0].Error != "unexpected_exit_ip" || len(e.Results[0].Matrices) != 0 {
			t.Fatalf("mismatch was ignored: %+v", e)
		}
		break
	}
	if down.Load() != beforeDown || up.Load() != beforeUp {
		t.Fatal("large transfers started after exit mismatch")
	}
}
