package clash

import (
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/metacubex/mihomo/constant"
)

func urlToMetadata(rawURL string, network constant.NetWork) (addr constant.Metadata, err error) {
	if !strings.Contains(rawURL, "://") {
		rawURL = "tcp://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}

	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		default:
			return
		}
	}

	// Convert port string to uint16
	p, _ := strconv.Atoi(port)
	portNum := uint16(p)

	addr = constant.Metadata{
		NetWork: network,
		Host:    u.Hostname(),
		DstIP:   netip.Addr{},
		DstPort: portNum,
	}
	return
}
