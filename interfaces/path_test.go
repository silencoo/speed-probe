package interfaces

import (
	"encoding/json"
	"testing"
)

func pathFixture() *TestPath {
	return &TestPath{ID: "hk-jp", Name: "HK -> JP", Hops: []PathHop{
		{ID: "hk", Name: "HK", Payload: `{"type":"shadowsocks","server":"127.0.0.1","server_port":443,"method":"2022-blake3-aes-128-gcm","password":"TEST"}`},
		{ID: "jp", Name: "JP", Payload: `{"type":"anytls","server":"127.0.0.1","server_port":444,"password":"TEST"}`},
	}}
}
func TestPathCompileOrderAndRejects(t *testing.T) {
	p := pathFixture()
	out, e := p.Compile(false)
	if e != nil {
		t.Fatal(e)
	}
	var a, b map[string]any
	json.Unmarshal([]byte(out[0]), &a)
	json.Unmarshal([]byte(out[1]), &b)
	if a["detour"] != nil || b["detour"] != "hop-0" || b["tag"] != "hop-1" {
		t.Fatal("wrong route")
	}
	for _, mutate := range []func(*TestPath){
		func(p *TestPath) { p.Hops[1].ID = p.Hops[0].ID }, func(p *TestPath) { p.Hops = nil },
		func(p *TestPath) { p.Hops[0].Payload = `{"type":"direct"}` },
		func(p *TestPath) {
			p.Hops[0].Payload = `{"type":"socks","server":"a","server_port":1,"detour":"outside"}`
		},
		func(p *TestPath) { p.ExpectedIP = "127.0.0.1" },
	} {
		p := pathFixture()
		mutate(p)
		if _, e := p.Compile(false); e == nil {
			t.Fatal("accepted invalid path")
		}
	}
}
func TestUDPTransportComposition(t *testing.T) {
	p := pathFixture()
	p.Hops[0].Payload = `{"type":"http","server":"a","server_port":1}`
	if _, e := p.Compile(true); e != nil {
		t.Fatal("AnyTLS carries UDP over upstream TCP", e)
	}
	p.Hops[1].Payload = `{"type":"shadowsocks","server":"b","server_port":1}`
	if _, e := p.Compile(true); e == nil {
		t.Fatal("HTTP cannot carry raw SS UDP")
	}
}
