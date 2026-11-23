package vaultsync

// BestEffort keeps optional sharing out of the error path of other carried
// state. The local switch is checked again before every operation, including
// a restore after an in-flight request. Failed work retries on the next round.
func BestEffort(carrier Carrier, enabled func() bool, report func(error)) Carrier {
	return bestEffort{Carrier: carrier, enabled: enabled, report: report}
}

type bestEffort struct {
	Carrier
	report  func(error)
	enabled func() bool
}

func (b bestEffort) finish(err error) error {
	if err != nil && b.report != nil {
		b.report(err)
	}
	return nil
}
func (b bestEffort) capture(v VaultFile) error {
	if !b.enabled() {
		return nil
	}
	return b.finish(b.Carrier.capture(v))
}
func (b bestEffort) captureEdits(v VaultFile) error {
	if !b.enabled() {
		return nil
	}
	return b.finish(b.Carrier.captureEdits(v))
}
func (b bestEffort) restore(v VaultFile) error {
	if !b.enabled() {
		return nil
	}
	return b.finish(b.Carrier.restore(v))
}
