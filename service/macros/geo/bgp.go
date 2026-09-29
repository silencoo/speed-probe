package geo

import (
	"encoding/json"
	"net"
	"net/url"
	"strconv"

	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/vendors"
)

// RIPE RIS reports the most specific observed route and all its origin ASNs.
// This is metadata about the exit, not a route trace or a residential-IP verdict.
func enrichBGP(p interfaces.Vendor, info *interfaces.GeoInfo) {
	info.BGPSource = "RIPE RIS"
	info.BGPStatus = "unavailable"
	addr := net.ParseIP(info.IP)
	if addr == nil || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return
	}
	body, resp, _, err := vendors.RequestWithRetryDetailed(p, 1, 4000, &interfaces.RequestOptions{
		URL:     "https://stat.ripe.net/data/network-info/data.json?resource=" + url.QueryEscape(info.IP),
		Network: interfaces.ROptionsTCP,
	})
	if err != nil || resp == nil || resp.StatusCode != 200 {
		return
	}
	prefix, asns, ok := parseBGP(body, addr)
	if !ok {
		return
	}
	info.Prefix = prefix
	info.RoutingASNs = asns
	info.BGPStatus = "available"
}

func parseBGP(body []byte, addr net.IP) (string, []uint32, bool) {
	var response struct {
		Status string `json:"status"`
		Data   struct {
			Prefix string            `json:"prefix"`
			ASNs   []json.RawMessage `json:"asns"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil || response.Status != "ok" {
		return "", nil, false
	}
	_, network, err := net.ParseCIDR(response.Data.Prefix)
	if err != nil || !network.Contains(addr) {
		return "", nil, false
	}
	asns := []uint32{}
	seen := map[uint32]bool{}
	for _, raw := range response.Data.ASNs {
		var value string
		if len(raw) > 0 && raw[0] == '"' {
			if json.Unmarshal(raw, &value) != nil {
				return "", nil, false
			}
		} else {
			value = string(raw)
		}
		asn, err := strconv.ParseUint(value, 10, 32)
		if err != nil || asn == 0 {
			return "", nil, false
		}
		if !seen[uint32(asn)] {
			asns = append(asns, uint32(asn))
			seen[uint32(asn)] = true
		}
	}
	if len(asns) == 0 {
		return "", nil, false
	}
	return network.String(), asns, true
}
