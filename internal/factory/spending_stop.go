package factory

// MayStopAfterStep is true while a job can accept a hold on its next boundary.
func MayStopAfterStep(it Item) bool {
	return (it.State == StateRunning || it.State == StateQueued) && !it.Publishing
}
