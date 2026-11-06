package tui3

// configuredModel keeps reconciliation anchored to the person's saved choice.
// Agents predating the run-only door keep the existing live-model behavior.
func (a *app) configuredModel() string {
	if door, ok := a.agent.(interface{ ConfiguredModel() string }); ok {
		return door.ConfiguredModel()
	}
	if a.model != "" {
		return a.model
	}
	return a.agent.Model()
}

func (a *app) useModelForNow(id string, window int) {
	wasSwitching := a.creditSwitching
	a.creditSwitching = true
	a.switchModel(id, window)
	a.creditSwitching = wasSwitching
}
