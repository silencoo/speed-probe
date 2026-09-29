package uploadspeed

import (
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/service/macros/upload"
)

type UploadSpeed struct{ upload.Result }

func (m *UploadSpeed) Type() interfaces.SlaveRequestMatrixType    { return interfaces.MatrixUploadSpeed }
func (m *UploadSpeed) MacroJob() interfaces.SlaveRequestMacroType { return interfaces.MacroUpload }
func (m *UploadSpeed) Extract(_ interfaces.SlaveRequestMatrixEntry, macro interfaces.SlaveRequestMacro) {
	if mac, ok := macro.(*upload.Upload); ok {
		m.Result = mac.Result
	}
}
