package vaultsync

// BestEffort keeps optional sharing out of the error path of other carried
// state. Failed work is attempted again at the next ordinary capture or restore.
func BestEffort(carrier Carrier, report func(error)) Carrier {
	return bestEffort{Carrier: carrier, report: report}
}

type bestEffort struct {
	Carrier
	report func(error)
}

func (b bestEffort) finish(err error) error {
	if err != nil && b.report != nil {
		b.report(err)
	}
	return nil
}
func (b bestEffort) capture(v VaultFile) error      { return b.finish(b.Carrier.capture(v)) }
func (b bestEffort) captureEdits(v VaultFile) error { return b.finish(b.Carrier.captureEdits(v)) }
func (b bestEffort) restore(v VaultFile) error      { return b.finish(b.Carrier.restore(v)) }
