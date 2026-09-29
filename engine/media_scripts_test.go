package engine

import (
	"github.com/dop251/goja"
	"os"
	"path/filepath"
	"testing"
)

// Cross-repository regression: run the shipped JavaScript in the real Goja runtime.
func TestBundledMediaScripts(t *testing.T) {
	root := os.Getenv("SPEED_CONTROL_SCRIPTS")
	if root == "" {
		t.Skip("SPEED_CONTROL_SCRIPTS not set")
	}
	for _, name := range []string{"netflix", "youtube", "disneyplus", "openai"} {
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join(root, name+".js"))
			if err != nil {
				t.Fatal(err)
			}
			vm := VMNew()
			calls := 0
			vm.Set("fetch", func(call goja.FunctionCall) goja.Value {
				calls++
				code := 200
				body := `<title>Netflix</title><script src="recaptcha/api.js"></script>`
				switch name {
				case "youtube":
					body = `ytcfg Premium ad-free "contentRegion":"JP"`
				case "disneyplus":
					body = `{"assertion":"test"}`
					if calls == 2 {
						body = `{"refresh_token":"test"}`
					}
					if calls == 3 {
						body = `{"extensions":{"sdk":{"session":{"inSupportedLocation":true,"location":{"countryCode":"JP"}}}}}`
					}
				case "openai":
					code = 401
					body = `{"error":{"message":"Missing API key"}}`
					if calls == 2 {
						code = 200
						body = `{"auth0":{"id":"auth0","signinUrl":"https://chatgpt.com/api/auth/signin/auth0"}}`
					}
				}
				return vm.ToValue(map[string]interface{}{"statusCode": code, "body": body})
			})
			if _, err = vm.RunString(PREDEFINED_SCRIPT + string(source)); err != nil {
				t.Fatal(err)
			}
			ret, err := ExecTaskCallback(vm, "handler")
			if err != nil {
				t.Fatal(err)
			}
			if ret.ToObject(vm).Get("status").String() != "reachable" {
				t.Fatal(ret)
			}
		})
	}
}
