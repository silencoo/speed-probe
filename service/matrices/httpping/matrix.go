package httpping

import (
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/service/macros/ping"
)

type HTTPPing struct {
	interfaces.HTTPPingDS
	Max       uint16
	Attempts  int
	Failures  int
	HTTPCode  int
	StdDev    uint16
	ErrorCode string `json:",omitempty"`
}

func (m *HTTPPing) Type() interfaces.SlaveRequestMatrixType {
	return interfaces.MatrixHTTPPing
}

func (m *HTTPPing) MacroJob() interfaces.SlaveRequestMacroType {
	return interfaces.MacroPing
}

func (m *HTTPPing) Extract(entry interfaces.SlaveRequestMatrixEntry, macro interfaces.SlaveRequestMacro) {
	if mac, ok := macro.(*ping.Ping); ok {
		m.Value = mac.Request
		m.Max = mac.RequestMax
		m.Attempts = mac.Attempts
		m.Failures = mac.Failures
		m.HTTPCode = mac.HTTPCode
		m.StdDev = mac.RequestStd
		m.ErrorCode = mac.ErrorCode
	}
}
