package session

// automaticFactoryRefusal is separate from chat budget: a person may still
// ask a stopped job's manager questions without granting execution authority.
func (a *Agent) automaticFactoryRefusal() string {
 if a.config.FactoryWorkRefusal != nil { return a.config.FactoryWorkRefusal() }
	if door, ok := a.config.FactoryRun.(interface{ AutomaticRefusal(string) string }); ok {
		return door.AutomaticRefusal(a.config.SessionFile)
	}
	return ""
}
