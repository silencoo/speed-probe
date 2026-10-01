package interfaces

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Hops are in physical travel order: probe -> first hop -> final exit.
type PathHop struct {
	ID      string
	Name    string
	Payload string
}
type TestPath struct {
	ID         string
	Name       string
	Hops       []PathHop
	CheckURL   string `json:",omitempty"`
	ExpectedIP string `json:",omitempty"`
}
type ExitCheck struct {
	IP    string `json:"ip,omitempty"`
	State string `json:"state"`
}

var pathID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func unsafePathValue(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for k, x := range v {
			if strings.HasSuffix(k, "_path") || k == "command" || k == "detour" || k == "domain_resolver" ||
				k == "bind_interface" || k == "routing_mark" || k == "netns" || k == "inet4_bind_address" || k == "inet6_bind_address" || k == "plugin" || k == "plugin_opts" {
				return true
			}
			if unsafePathValue(x) {
				return true
			}
		}
	case []any:
		for _, x := range v {
			if unsafePathValue(x) {
				return true
			}
		}
	}
	return false
}

func permitsNetwork(v map[string]any, required string) bool {
	value, exists := v["network"]
	if !exists || value == nil {
		return true
	}
	switch list := value.(type) {
	case string:
		return list == required || list == ""
	case []any:
		if len(list) == 0 {
			return true
		}
		for _, item := range list {
			if item == required {
				return true
			}
		}
	}
	return false
}

// No direct/selector outbound is added; failure cannot change the selected route.
func (p *TestPath) Compile(udp bool) ([]string, error) {
	if p == nil || !pathID.MatchString(p.ID) || strings.TrimSpace(p.Name) == "" || len(p.Hops) < 1 || len(p.Hops) > 8 {
		return nil, errors.New("invalid path identity or hop count")
	}
	if p.ExpectedIP != "" && (net.ParseIP(p.ExpectedIP) == nil || p.CheckURL == "") {
		return nil, errors.New("expected IP requires an IP check URL")
	}
	if p.CheckURL != "" {
		u, e := url.Parse(p.CheckURL)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return nil, errors.New("invalid IP check URL")
		}
	}
	seen := map[string]bool{}
	raw := make([]map[string]any, len(p.Hops))
	out := []string{}
	for i, h := range p.Hops {
		if !pathID.MatchString(h.ID) || seen[h.ID] || strings.TrimSpace(h.Name) == "" {
			return nil, errors.New("missing or repeated hop identity")
		}
		seen[h.ID] = true
		if json.Unmarshal([]byte(h.Payload), &raw[i]) != nil || raw[i] == nil || unsafePathValue(raw[i]) {
			return nil, errors.New("invalid hop or unmanaged routing/file reference")
		}
		v := raw[i]
		typ, _ := v["type"].(string)
		switch typ {
		case "shadowsocks", "anytls", "socks", "http", "trojan", "vmess", "vless":
		default:
			return nil, errors.New("path supports TCP-carried proxy protocols only")
		}
		host, _ := v["server"].(string)
		port, _ := v["server_port"].(float64)
		if host == "" || port < 1 || port > 65535 || port != float64(int(port)) || !permitsNetwork(v, "tcp") {
			return nil, errors.New("hop cannot carry TCP")
		}
		v["tag"] = fmt.Sprintf("hop-%d", i)
		if i > 0 {
			v["detour"] = fmt.Sprintf("hop-%d", i-1)
		}
		b, _ := json.Marshal(v)
		out = append(out, string(b))
	}
	// UDP needs only TCP upstream when a hop wraps datagrams in its TCP protocol.
	needUDP := udp
	for i := len(raw) - 1; i >= 0 && needUDP; i-- {
		v := raw[i]
		if v["type"] == "http" || !permitsNetwork(v, "udp") {
			return nil, errors.New("path cannot carry requested UDP")
		}
		needUDP = v["type"] == "shadowsocks" || v["type"] == "socks"
		if u, ok := v["udp_over_tcp"].(map[string]any); ok && u["enabled"] == true {
			needUDP = false
		}
	}
	return out, nil
}
