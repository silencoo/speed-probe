package interfaces

type SlaveEntrySlot struct {
	Error          string     `json:"error,omitempty"`
	ExitCheck      *ExitCheck `json:"exit_check,omitempty"`
	Index          int        `json:"index"`
	Grouping       string
	ProxyInfo      ProxyInfo
	InvokeDuration int64
	Matrices       []MatrixResponse
}

func (ses *SlaveEntrySlot) Get(idx int) *MatrixResponse {
	if idx < len(ses.Matrices) {
		return &ses.Matrices[idx]
	}
	return nil
}
