package clash

import (
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/component/resolver"
	"github.com/metacubex/mihomo/constant"
	"github.com/silencoo/speed-probe/utils"
	"gopkg.in/yaml.v2"
)

func init() { resolver.DisableIPv6 = false }

func parseProxy(proxyName, proxyPayload string) constant.Proxy {
	var payload map[string]any
	if err := yaml.Unmarshal([]byte(proxyPayload), &payload); err != nil {
		return nil
	}
	if payload == nil {
		return nil
	}
	payload["name"] = proxyName
	proxy, err := adapter.ParseProxy(payload)

	if err != nil {
		utils.DErrorf("Mihomo rejected node configuration")
	}

	return proxy
}

func extractFirstProxy(proxyName, proxyPayload string) constant.Proxy {
	proxy := parseProxy(proxyName, proxyPayload)

	if proxy != nil {
		return proxy
	}

	return nil
}
