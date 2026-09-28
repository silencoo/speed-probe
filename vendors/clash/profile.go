package clash

import (
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/constant"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/utils"
	"gopkg.in/yaml.v2"
)

func parseProxy(proxyName, proxyPayload string) constant.Proxy {
	var payload map[string]any
	yaml.Unmarshal([]byte(proxyPayload), &payload)
	proxy, err := adapter.ParseProxy(payload)

	if err != nil {
		utils.DErrorf("Vendor Parser | Parse clash profile error! proxyName=%s, error=%v", proxyName, err.Error())
		utils.DLogf("Vendor Parser | Failed Payload: %s", proxyPayload)
	}

	return proxy
}

func extractFirstProxy(proxyName, proxyPayload string) constant.Proxy {
	proxy := parseProxy(proxyName, proxyPayload)

	if proxy != nil && interfaces.Parse(proxy.Type().String()) != interfaces.ProxyInvalid {
		return proxy
	}

	return nil
}
