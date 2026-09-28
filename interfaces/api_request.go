package interfaces

type SlaveRequestMatrixEntry struct {
	Type   SlaveRequestMatrixType
	Params string
}

type SlaveRequestOptions struct {
	Filter   string
	Matrices []SlaveRequestMatrixEntry
}

func (sro *SlaveRequestOptions) Clone() *SlaveRequestOptions {
	return &SlaveRequestOptions{
		Filter:   sro.Filter,
		Matrices: cloneSlice(sro.Matrices),
	}
}

type SlaveRequestNode struct {
	Name    string
	Payload string
}

func (srn *SlaveRequestNode) Clone() *SlaveRequestNode {
	return &SlaveRequestNode{
		Name:    srn.Name,
		Payload: srn.Payload,
	}
}

type SlaveRequest struct {
	Options SlaveRequestOptions
	Configs SlaveRequestConfigs

	Vendor VendorType
	Nodes  []SlaveRequestNode
}

func (sr *SlaveRequest) Clone() *SlaveRequest {
	return &SlaveRequest{
		Options: *sr.Options.Clone(),
		Configs: *sr.Configs.Clone(),
		Vendor:  sr.Vendor,
		Nodes:   cloneSlice(sr.Nodes),
	}
}
