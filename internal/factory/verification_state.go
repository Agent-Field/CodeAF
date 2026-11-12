package factory

import "time"

// StepVerification is presentation of engine-owned work, never acceptance
// evidence. A step waiting on its checks remains PhaseRunning.
type StepVerification struct {
	State  string         `json:"state"`
	Reason string         `json:"reason,omitempty"`
	Checks []RunningCheck `json:"checks,omitempty"`
}

// RunningCheck deliberately omits environment, output and filesystem paths.
type RunningCheck struct {
	ID        string    `json:"id"`
	Purpose   string    `json:"purpose"`
	StartedAt time.Time `json:"started_at"`
}

const StepRuntimeHeader = "X-Codeaf-Step-Runtime"
