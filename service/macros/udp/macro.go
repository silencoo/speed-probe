package udp

import (
	"strings"
	"time"

	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/preconfigs"
)

type Udp struct {
	NATType   string
	Reachable bool
	ErrorCode string
}

func (m *Udp) Type() interfaces.SlaveRequestMacroType {
	return interfaces.MacroUDP
}

func (m *Udp) Run(proxy interfaces.Vendor, r *interfaces.SlaveRequest) error {
	stunURL := strings.TrimSpace(r.Configs.STUNURL)
	if stunURL == "" {
		stunURL = preconfigs.PROXY_DEFAULT_STUN_SERVER
	}

	timeout := time.Duration(r.Configs.TaskTimeout) * time.Millisecond
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 3 * time.Second
	}
	mapType, filterType, reachable, code := detectNATType(proxy, stunURL, timeout)
	m.NATType = natTypeToString(mapType, filterType)
	m.Reachable, m.ErrorCode = reachable, code

	return nil
}
