package ping

import (
	"github.com/silencoo/speed-probe/interfaces"
)

type Ping struct {
	RTT           uint16
	Request       uint16
	RTTStd        uint16
	RTTMax        uint16
	RequestStd    uint16
	RequestMax    uint16
	Attempts      int
	Failures      int
	RTTFailures   int
	HTTPCode      int
	ErrorCode     string
	ErrorPhase    string
	RTTErrorCode  string
	RTTErrorPhase string
}

func (m *Ping) Type() interfaces.SlaveRequestMacroType {
	return interfaces.MacroPing
}

func (m *Ping) Run(proxy interfaces.Vendor, r *interfaces.SlaveRequest) error {
	measure(m, proxy, r.Configs)
	return nil
}
