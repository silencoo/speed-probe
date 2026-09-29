package averagespeed

import (
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/service/macros/speed"
)

type AverageSpeed struct {
	interfaces.AverageSpeedDS
	TotalBytes    uint64
	ElapsedMillis int64
	StopReason    string
	ErrorCode     string `json:",omitempty"`
	HTTPCode      int    `json:",omitempty"`
}

func (m *AverageSpeed) Type() interfaces.SlaveRequestMatrixType {
	return interfaces.MatrixAverageSpeed
}

func (m *AverageSpeed) MacroJob() interfaces.SlaveRequestMacroType {
	return interfaces.MacroSpeed
}

func (m *AverageSpeed) Extract(entry interfaces.SlaveRequestMatrixEntry, macro interfaces.SlaveRequestMacro) {
	if mac, ok := macro.(*speed.Speed); ok {
		m.Value = mac.AvgSpeed
		m.TotalBytes = mac.TotalSize
		m.ElapsedMillis = mac.ElapsedMillis
		m.StopReason = mac.StopReason
		m.ErrorCode = mac.ErrorCode
		m.HTTPCode = mac.HTTPCode
	}
}
