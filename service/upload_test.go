package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/silencoo/speed-probe/auth"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/service/macros/upload"
)

func TestUploadAuthorizationAndSequentialWindows(t *testing.T) {
	address, token, manager, client := testBackend(t)
	var downloaded atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if !downloaded.Load() {
				t.Error("upload overlapped download")
			}
			io.Copy(io.Discard, r.Body)
			w.WriteHeader(204)
			return
		}
		time.Sleep(30 * time.Millisecond)
		downloaded.Store(true)
		w.Write(make([]byte, 100003))
	}))
	defer server.Close()
	req := &interfaces.SlaveRequest{Vendor: interfaces.VendorMihomo,
		Nodes:   []interfaces.SlaveRequestNode{{Name: "test", Payload: "name: test\ntype: direct\n"}},
		Configs: interfaces.SlaveRequestConfigs{StageProgress: true, UploadURL: server.URL, DownloadURL: server.URL, DownloadBytes: 100003, DownloadDuration: 1, DownloadThreading: 1},
		Options: interfaces.SlaveRequestOptions{Matrices: []interfaces.SlaveRequestMatrixEntry{{Type: interfaces.MatrixUploadSpeed}, {Type: interfaces.MatrixAverageSpeed}}},
	}
	denied := connect(t, address, token)
	denied.WriteJSON(Command{Protocol: 3, Type: "run", TaskID: "denied", Request: req})
	if e := read(t, denied); e.Error == nil || e.Error.Code != "permission_denied" {
		t.Fatalf("%+v", e)
	}
	client.Capabilities = append(client.Capabilities, "speed")
	if err := auth.Save(manager.Path, auth.Store{Clients: []auth.Client{client}}); err != nil {
		t.Fatal(err)
	}
	c := connect(t, address, token)
	c.WriteJSON(Command{Protocol: 3, Type: "run", TaskID: "upload", Request: req})
	stages := map[string]bool{}
	for {
		e := read(t, c)
		if e.Type == "stage" && e.Active {
			stages[e.Stage] = true
			if e.Stage == "upload_check" && !stages["download"] {
				t.Fatal("upload stage before download")
			}
		}
		if e.Type != "finished" {
			continue
		}
		if e.State != "succeeded" || len(e.Results) != 1 || len(e.Results[0].Matrices) != 2 {
			t.Fatalf("%+v", e)
		}
		found := false
		for _, matrix := range e.Results[0].Matrices {
			if matrix.Type != interfaces.MatrixUploadSpeed {
				continue
			}
			var result upload.Result
			if err := json.Unmarshal([]byte(matrix.Payload), &result); err != nil {
				t.Fatal(err)
			}
			if result.TotalBytes != 100003 || result.StopReason != "byte_limit" {
				t.Fatalf("%+v", result)
			}
			found = true
		}
		if !found {
			t.Fatal("missing upload matrix")
		}
		break
	}
	for _, stage := range []string{"connecting", "download_check", "download", "upload_check", "upload"} {
		if !stages[stage] {
			t.Errorf("missing stage %s", stage)
		}
	}
	req.Configs.StageProgress = false
	legacy := connect(t, address, token)
	legacy.WriteJSON(Command{Protocol: 3, Type: "run", TaskID: "legacy", Request: req})
	for {
		e := read(t, legacy)
		if e.Type == "stage" {
			t.Fatal("unsolicited stage event")
		}
		if e.Type == "finished" {
			break
		}
	}
}
