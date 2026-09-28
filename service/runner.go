package service

import (
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/service/macros"
	"github.com/silencoo/speed-probe/utils/structs"
)

func ExtractMacrosFromMatrices(matrices []interfaces.SlaveRequestMatrix) []interfaces.SlaveRequestMacroType {
	macroTypes := structs.NewSet[interfaces.SlaveRequestMacroType]()
	for _, matrix := range matrices {
		macroTypes.Add(matrix.MacroJob())
	}
	return structs.Filter(macroTypes.Digest(), func(m interfaces.SlaveRequestMacroType) bool {
		return macros.Find(m).Type() != interfaces.MacroInvalid
	})
}
