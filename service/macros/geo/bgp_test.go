package geo

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"testing"

	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/vendors"
)

func TestBGPResponseValidation(t *testing.T) {
	for _, tc := range []struct {
		body, ip, prefix string
		valid            bool
		count            int
	}{
		{`{"status":"ok","data":{"prefix":"1.1.1.0/24","asns":["13335",13335,64500]}}`, "1.1.1.1", "1.1.1.0/24", true, 2},
		{`{"status":"ok","data":{"prefix":"2606:4700::/32","asns":[13335]}}`, "2606:4700:4700::1111", "2606:4700::/32", true, 1},
		{`{"status":"ok","data":{"prefix":"8.8.8.0/24","asns":[15169]}}`, "1.1.1.1", "", false, 0},
		{`{"status":"error","data":{"prefix":"1.1.1.0/24","asns":[13335]}}`, "1.1.1.1", "", false, 0},
		{`{"status":"ok","data":{"prefix":"1.1.1.0/24","asns":[]}}`, "1.1.1.1", "", false, 0},
		{`{"status":"ok","data":{"prefix":"1.1.1.0/24","asns":[4294967296]}}`, "1.1.1.1", "", false, 0},
		{`<html>rate limited</html>`, "1.1.1.1", "", false, 0},
	} {
		prefix, asns, ok := parseBGP([]byte(tc.body), net.ParseIP(tc.ip))
		if ok != tc.valid || prefix != tc.prefix || len(asns) != tc.count {
			t.Fatalf("%s: %s %v %v", tc.body, prefix, asns, ok)
		}
	}
}

func TestMetadataFailureRetainsObservedIP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := RunGeoCheck(vendors.WithContext(ctx, nil), "", "192.0.2.34", 1, interfaces.ROptionsTCP)
	if got.IP != "192.0.2.34" || got.LookupStatus != "unavailable" || got.BGPStatus != "unavailable" {
		t.Fatalf("%+v", got)
	}
	key := fmt.Sprintf("%s:%x", got.IP, sha256.Sum256(nil))
	if _, ok := GeoCache.Get(key); ok {
		t.Fatal("failed metadata cached")
	}
}

func TestCacheReturnsIsolatedCopyWithOriginalQueryTime(t *testing.T) {
	ip := "192.0.2.35"
	key := fmt.Sprintf("%s:%x", ip, sha256.Sum256(nil))
	original := &interfaces.GeoInfo{IP: ip, QueriedAt: "2026-09-29T00:00:00Z", RoutingASNs: []uint32{64500}}
	GeoCache.Set(key, original)
	got := RunGeoCheck(nil, "", ip, 1, interfaces.ROptionsTCP)
	got.RoutingASNs[0] = 64501
	if !got.Cached || original.Cached || original.RoutingASNs[0] != 64500 || got.QueriedAt != original.QueriedAt {
		t.Fatal("cache result mutated")
	}
}
