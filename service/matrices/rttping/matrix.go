package rttping

import (
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/service/macros/ping"
)

type RTTPing struct {
	interfaces.RTTPingDS
	Max       uint16
	Attempts  int
	Failures  int
	HTTPCode  int
	ErrorCode string `json:",omitempty"`
}

func (m *RTTPing) Type() interfaces.SlaveRequestMatrixType {
	return interfaces.MatrixRTTPing
}

func (m *RTTPing) MacroJob() interfaces.SlaveRequestMacroType {
	return interfaces.MacroPing
}

func (m *RTTPing) Extract(entry interfaces.SlaveRequestMatrixEntry, macro interfaces.SlaveRequestMacro) {
	if mac, ok := macro.(*ping.Ping); ok {
		m.Value = mac.RTT
		m.Max = mac.RTTMax
		m.Attempts = mac.Attempts
		m.Failures = mac.Failures
		m.HTTPCode = mac.HTTPCode
		m.StdDev = mac.RTTStd
		m.ErrorCode = mac.ErrorCode
	}
}
