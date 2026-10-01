package rttping

import (
	"testing"

	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/service/macros/ping"
)

func TestHTTPFailureDoesNotLeakIntoConnectionMatrix(t *testing.T) {
	for _, failed := range []int{0, 1} {
		macro := &ping.Ping{RTT: 12, Attempts: 3, RTTFailures: failed,
			Failures: 3, ErrorCode: "timeout", ErrorPhase: "http"}
		if failed > 0 {
			macro.RTTErrorCode = "tls_error"
			macro.RTTErrorPhase = "tls"
		}
		matrix := &RTTPing{}
		matrix.Extract(interfaces.SlaveRequestMatrixEntry{}, macro)
		if matrix.Value != 12 || matrix.Attempts != 3 || matrix.Failures != failed ||
			matrix.ErrorCode != macro.RTTErrorCode || matrix.ErrorPhase != macro.RTTErrorPhase {
			t.Fatalf("%+v", matrix)
		}
	}
}
