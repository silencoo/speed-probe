package geo

import (
	"crypto/sha256"
	"fmt"
	"net"
	"time"

	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/utils"
	"github.com/silencoo/speed-probe/utils/structs"
	"github.com/silencoo/speed-probe/utils/structs/memutils"
	"github.com/silencoo/speed-probe/utils/structs/obliviousmap"
	"github.com/silencoo/speed-probe/vendors"
)

var GeoCache *obliviousmap.ObliviousMap[*interfaces.GeoInfo]

func RunGeoCheck(p interfaces.Vendor, script string, ip string, retry int, network interfaces.RequestOptionsNetwork) *interfaces.GeoInfo {
	addr := net.ParseIP(ip)
	if addr == nil {
		return &interfaces.GeoInfo{LookupStatus: "unavailable"}
	}
	ip = addr.String()
	key := fmt.Sprintf("%s:%x", ip, sha256.Sum256([]byte(script)))
	var ret *interfaces.GeoInfo = nil
	if r, ok := GeoCache.Get(key); ok && r != nil {
		copy := *r
		copy.Cached = true
		copy.RoutingASNs = append([]uint32(nil), r.RoutingASNs...)
		return &copy
	}

	// use mmdb first, if cannot get record, try remote query 3 times
	if ret = RunMMDBCheck(ip); ret == nil {
		if script == "" {
			retry = 1
		} // The built-in script already tries two providers.
		for i := 0; i < structs.WithIn(retry, 1, 3) && vendors.Context(p).Err() == nil && (ret == nil || ret.IP == ""); i++ {
			ret = ExecGeoCheck(p, script, ip, network)
		}
	}

	if ret == nil || net.ParseIP(ret.IP) == nil || !net.ParseIP(ret.IP).Equal(addr) {
		ret = &interfaces.GeoInfo{IP: ip, LookupStatus: "unavailable"}
	}
	ret.IP = ip // Preserve the observed exit even when metadata providers fail.
	ret.QueriedAt = time.Now().UTC().Format(time.RFC3339)
	if ret.LookupStatus == "" {
		ret.LookupStatus = "available"
		if ret.Source == "" {
			ret.Source = "custom"
		}
	}
	enrichBGP(p, ret)

	proxyName := "NoProxy"
	if p != nil {
		proxyName = p.ProxyInfo().Name
	}

	if ret.LookupStatus == "available" && ret.BGPStatus == "available" {
		GeoCache.Set(key, ret)
		utils.DLogf("GetIP Resolver | Resolved IP=%s proxy=%v ASN=%d ASOrg=%s", ip, proxyName, ret.ASN, ret.ASNOrg)
	} else {
		utils.DWarnf("GeoIP Resolver | Fail to resolve IP=%s proxy=%v", ip, proxyName)
	}
	return ret
}

func init() {
	memGeoInfo := memutils.MemDriverMemory[*interfaces.GeoInfo]{}
	memGeoInfo.Init()
	GeoCache = obliviousmap.NewObliviousMap[*interfaces.GeoInfo]("GeoCache/", time.Hour*6, true, &memGeoInfo)
}
