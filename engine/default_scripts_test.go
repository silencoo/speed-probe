package engine

import (
	"github.com/dop251/goja"
	"testing"
)

func TestDefaultGeoFallbackDoesNotCacheErrorAsSuccess(t *testing.T) {
	vm := VMNew()
	calls := 0
	vm.Set("fetch", func(call goja.FunctionCall) goja.Value {
		calls++
		body := `{"error":true,"reason":"quota"}`
		if calls == 2 {
			body = `{"ip":"1.1.1.1","country_code":"AU","country_name":"Australia","asn":"AS13335","org":"Cloudflare"}`
		}
		return vm.ToValue(map[string]any{"statusCode": 200, "body": body})
	})
	if _, err := vm.RunString(PREDEFINED_SCRIPT + DEFAULT_GEOIP_SCRIPT); err != nil {
		t.Fatal(err)
	}
	result, err := ExecTaskCallback(vm, "handler", "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	obj := result.ToObject(vm)
	if calls != 2 || obj.Get("asn").ToInteger() != 13335 {
		t.Fatal("fallback failed")
	}
	if obj.Get("source").String() != "ipapi.co" || obj.Get("isp").String() != "" {
		t.Fatal("source lost or ASN organization mislabeled as ISP")
	}
	vm.Set("fetch", func(goja.FunctionCall) goja.Value { return goja.Null() })
	result, err = ExecTaskCallback(vm, "handler", "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToObject(vm).Keys()) != 0 {
		t.Fatal("invented geo success")
	}
}
func TestDefaultIPDetectsBothFamilies(t *testing.T) {
	vm := VMNew()
	calls := 0
	vm.Set("fetch", func(call goja.FunctionCall) goja.Value {
		calls++
		body := `{"ip":"1.1.1.1"}`
		if calls == 2 {
			body = `{"ip":"2606:4700:4700::1111"}`
		}
		return vm.ToValue(map[string]any{"statusCode": 200, "body": body})
	})
	if _, err := vm.RunString(PREDEFINED_SCRIPT + DEFAULT_IP_SCRIPT); err != nil {
		t.Fatal(err)
	}
	result, err := ExecTaskCallback(vm, "ip_resolve_default")
	if err != nil {
		t.Fatal(err)
	}
	if result.ToObject(vm).Get("length").ToInteger() != 2 {
		t.Fatal("lost IP family")
	}
}
