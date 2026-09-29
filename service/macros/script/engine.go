package script

import (
	"context"
	"time"

	"github.com/dop251/goja"
	"github.com/silencoo/speed-probe/engine"
	"github.com/silencoo/speed-probe/engine/helpers"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/vendors"
)

func ExecScript(p interfaces.Vendor, script *interfaces.Script) interfaces.ScriptResult {
	s := interfaces.ScriptResult{}
	if script == nil {
		return s
	}

	timeout := time.Duration(script.TimeoutMillis) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if timeout > time.Minute {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(vendors.Context(p), timeout)
	defer cancel()
	p = vendors.WithContext(ctx, p)
	vm := engine.VMNewWithVendor(p, interfaces.ROptionsTCP)
	stop := context.AfterFunc(ctx, func() { vm.Interrupt("script cancelled") })
	defer stop()

	startTime := time.Now()
	ret, err := func() (goja.Value, error) {
		if _, err := vm.RunString(engine.PREDEFINED_SCRIPT + script.Content); err != nil {
			return nil, err
		}
		return engine.ExecTaskCallback(vm, "handler")
	}()

	s.TimeElapsed = time.Now().UnixMilli() - startTime.UnixMilli()
	if engine.ThrowExecTaskErr("MediaTest", err) {
		s.Status = "network_error"
		s.Text = "脚本错误"
		if ctx.Err() != nil {
			s.Text = "检测超时"
		}
		s.Background = "142,140,142"
	} else if text, ok := helpers.VMSafeStr(ret); ok {
		s.Text = text
	} else if ro, _ := helpers.VMSafeObj(vm, ret); ro != nil {
		if v, ok := helpers.VMSafeStr(ro.Get("status")); ok {
			s.Status = v
		}
		if v, ok := helpers.VMSafeStr(ro.Get("text")); ok {
			s.Text = v
		}
		if v, ok := helpers.VMSafeStr(ro.Get("color")); ok {
			s.Color = v
		}
		if v, ok := helpers.VMSafeStr(ro.Get("background")); ok {
			s.Background = v
		}
	}

	return s
}
