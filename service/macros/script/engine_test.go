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
