package geo

import (
	"context"
	"github.com/silencoo/speed-probe/vendors"
	"net"
	"strings"
	"time"

	"github.com/silencoo/speed-probe/engine"
	"github.com/silencoo/speed-probe/engine/helpers"
	"github.com/silencoo/speed-probe/interfaces"
)

func ExecIpCheck(p interfaces.Vendor, script string, network interfaces.RequestOptionsNetwork) (ipstacks *interfaces.IPStacks) {
	ipstacks = (&interfaces.IPStacks{}).Init()

	ctx, cancel := context.WithTimeout(vendors.Context(p), 30*time.Second)
	defer cancel()
	p = vendors.WithContext(ctx, p)
	vm := engine.VMNewWithVendor(p, network)
	stop := context.AfterFunc(ctx, func() { vm.Interrupt("geo script cancelled") })
	defer stop()
	if _, err := vm.RunString(engine.PREDEFINED_SCRIPT + engine.DEFAULT_IP_SCRIPT + script); err != nil {
		return
	}
	caller := "ip_resolve_default"
	if engine.HasFunction(vm, "ip_resolve") {
		caller = "ip_resolve"
	}

	ret, err := engine.ExecTaskCallback(vm, caller)
	if engine.ThrowExecTaskErr("IPResolve", err) {
		return
	} else {
		ipQuery := []string{}
		helpers.VMSafeMarshal(&ipQuery, ret, vm)
		for _, ip := range ipQuery {
			if net.ParseIP(ip) != nil {
				if !strings.Contains(ip, ":") {
					ipstacks.IPv4 = append(ipstacks.IPv4, ip)
				} else {
					ipstacks.IPv6 = append(ipstacks.IPv6, ip)
				}
			}
		}
	}

	return
}

func ExecGeoCheck(p interfaces.Vendor, script string, ip string, network interfaces.RequestOptionsNetwork) *interfaces.GeoInfo {
	ctx, cancel := context.WithTimeout(vendors.Context(p), 30*time.Second)
	defer cancel()
	p = vendors.WithContext(ctx, p)
	vm := engine.VMNewWithVendor(p, network)
	stop := context.AfterFunc(ctx, func() { vm.Interrupt("geo script cancelled") })
	defer stop()
	if script == "" {
		script = engine.DEFAULT_GEOIP_SCRIPT
	}
	if _, err := vm.RunString(engine.PREDEFINED_SCRIPT + script); err != nil {
		return nil
	}

	ret, err := engine.ExecTaskCallback(vm, "handler", ip)
	if engine.ThrowExecTaskErr("GeoCheck", err) {
		return nil
	} else {
		geoInfo := &interfaces.GeoInfo{}
		if err := helpers.VMSafeMarshal(geoInfo, ret, vm); err == nil {
			return geoInfo
		}
	}

	return nil
}
