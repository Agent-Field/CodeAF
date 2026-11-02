package handoff

import "time"

// Phase records the local work around a continuation. An unfinished phase marks
// the instant of the first lease request, before the relay can delay its answer.
type Phase struct {
	Name    string
	Started time.Time
	Ended   time.Time
}

// Trace measures one phase only when a caller has requested observations.
func Trace(report func(Phase), name string) func() {
	if report == nil {
		return func() {}
	}
	started := time.Now()
	return func() { report(Phase{Name: name, Started: started, Ended: time.Now()}) }
}
