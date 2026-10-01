package httpping

import (
	"testing"

	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/service/macros/ping"
)

func TestHTTPOnlyTimeoutRetainsConnectionEvidence(t *testing.T) {
	for _, rtt := range []uint16{0, 12} {
		macro := &ping.Ping{RTT: rtt, Attempts: 3, Failures: 3, ErrorCode: "timeout", ErrorPhase: "http"}
		matrix := &HTTPPing{}
		matrix.Extract(interfaces.SlaveRequestMatrixEntry{}, macro)
		if matrix.Connected != (rtt > 0) || matrix.Value != 0 || matrix.Failures != 3 ||
			matrix.ErrorCode != "timeout" || matrix.ErrorPhase != "http" {
			t.Fatalf("%+v", matrix)
		}
	}
}
