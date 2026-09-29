package script

import (
	"github.com/silencoo/speed-probe/interfaces"
	"testing"
)

func TestLocalScriptDefaultDeadline(t *testing.T) {
	result := ExecScript(nil, &interfaces.Script{Content: "function handler(){return 'ok'}"})
	if result.Text != "ok" {
		t.Fatalf("zero timeout broke CLI script: %+v", result)
	}
}

func TestStructuredStatusAndClone(t *testing.T) {
	result := ExecScript(nil, &interfaces.Script{Content: `function handler(){return {text:"页面可达",status:"reachable",background:"1,2,3"}}`})
	if result.Status != "reachable" || result.Text != "页面可达" || result.Clone().Status != "reachable" {
		t.Fatalf("script status was lost: %+v", result)
	}
	failed := ExecScript(nil, &interfaces.Script{Content: `function handler(){throw new Error("test")}`})
	if failed.Status != "network_error" || failed.Text != "脚本错误" {
		t.Fatalf("script failure was not classified: %+v", failed)
	}
}
