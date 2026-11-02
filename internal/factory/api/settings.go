package api

import "github.com/Agent-Field/codeaf/internal/factory/store"

// FactorySettings carries only runner policy, never host credentials.
type FactorySettings = store.RunnerSettings

type PatchFactorySettings struct {
	RunnerTimeoutSeconds *int  `json:"runner_timeout_s,omitempty"`
	KeepOutputDays       *int  `json:"keep_output_days,omitempty"`
	KeepOutputMB         *int  `json:"keep_output_mb,omitempty"`
	ReadOnArrival        *bool `json:"read_on_arrival,omitempty"`
}
