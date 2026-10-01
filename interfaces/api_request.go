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
	Path    *TestPath `json:",omitempty"`
}

func (srn *SlaveRequestNode) Clone() *SlaveRequestNode {
	ret := &SlaveRequestNode{
		Name:    srn.Name,
		Payload: srn.Payload,
	}
	if srn.Path != nil {
		path := *srn.Path
		path.Hops = cloneSlice(path.Hops)
		ret.Path = &path
	}
	return ret
}

type SlaveRequest struct {
	Options SlaveRequestOptions
	Configs SlaveRequestConfigs

	Vendor VendorType
	Nodes  []SlaveRequestNode
}

func (sr *SlaveRequest) Clone() *SlaveRequest {
	nodes := make([]SlaveRequestNode, len(sr.Nodes))
	for i := range sr.Nodes {
		nodes[i] = *sr.Nodes[i].Clone()
	}
	return &SlaveRequest{
		Options: *sr.Options.Clone(),
		Configs: *sr.Configs.Clone(),
		Vendor:  sr.Vendor,
		Nodes:   nodes,
	}
}
