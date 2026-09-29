package factory

import (
	"context"
	"github.com/dop251/goja"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/vendors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchDiagnosticContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/blocked", 302)
			return
		}
		w.Header().Set("CF-Mitigated", "challenge")
		w.WriteHeader(403)
		w.Write([]byte("blocked"))
	}))
	defer server.Close()
	vm := goja.New()
	vm.Set("fetch", FetchFactory(vm, vendors.WithContext(context.Background(), nil), interfaces.ROptionsTCP))
	vm.Set("base", server.URL)
	for _, tc := range []struct{ code, want string }{
		{`fetch(base+'/slow',{timeout:20,detailed:true}).errorCode`, "timeout"},
		{`String(fetch(base+'/slow',{timeout:20}))`, "null"},
		{`String(fetch(base+'/redirect',{detailed:true}).statusCode)`, "403"},
		{`fetch(base+'/redirect',{detailed:true}).body`, "blocked"},
		{`String(fetch(base+'/redirect',{detailed:true}).url.endsWith('/blocked'))`, "true"},
	} {
		v, err := vm.RunString(tc.code)
		if err != nil || v.String() != tc.want {
			t.Fatalf("%s: %v %v", tc.code, v, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	vm.Set("fetch", FetchFactory(vm, vendors.WithContext(ctx, nil), interfaces.ROptionsTCP))
	v, err := vm.RunString(`fetch(base,{detailed:true}).errorCode`)
	if err != nil || v.String() != "cancelled" {
		t.Fatalf("cancel: %v %v", v, err)
	}
}
