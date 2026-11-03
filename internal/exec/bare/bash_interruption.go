package bare

import "context"

type bashInterruptionKey struct{}

// WithBashInterruption observes only endings decided by the shell producer.
// The callback runs before Execute returns and never reads command output.
func WithBashInterruption(ctx context.Context, report func(string)) context.Context {
	return context.WithValue(ctx, bashInterruptionKey{}, report)
}

func reportBashInterruption(ctx context.Context, outcome string) {
	if report, _ := ctx.Value(bashInterruptionKey{}).(func(string)); report != nil {
		report(outcome)
	}
}
