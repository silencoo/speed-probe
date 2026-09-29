package speed

import "github.com/silencoo/speed-probe/interfaces"

type Speed struct {
	SourceHealth  string
	ErrorPhase    string
	AvgSpeed      uint64
	MaxSpeed      uint64
	TotalSize     uint64
	Speeds        []uint64
	ElapsedMillis int64
	StopReason    string
	ErrorCode     string
	HTTPCode      int
}

func (m *Speed) Type() interfaces.SlaveRequestMacroType {
	return interfaces.MacroSpeed
}

func (m *Speed) Run(proxy interfaces.Vendor, r *interfaces.SlaveRequest) error {
	Once(m, proxy, &r.Configs)

	return nil
}
