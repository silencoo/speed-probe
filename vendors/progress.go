package vendors

import (
	"context"
	"github.com/silencoo/speed-probe/interfaces"
)

type progressKey struct{}

func WithProgress(ctx context.Context, report func(string, bool)) context.Context {
	return context.WithValue(ctx, progressKey{}, report)
}

// Only callers' fixed stage identifiers are emitted; never URLs or credentials.
func Report(p interfaces.Vendor, stage string, active bool) {
	if report, ok := Context(p).Value(progressKey{}).(func(string, bool)); ok {
		report(stage, active)
	}
}
