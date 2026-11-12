package api

import "github.com/Agent-Field/codeaf/internal/factory"

// StepRuntimeHeader carries additive worker detail outside strict legacy JSON.
const StepRuntimeHeader = factory.StepRuntimeHeader

type StepRuntime struct {
	VerificationFailureKey string                    `json:"verification_failure_key,omitempty"`
	RoundLimit             int                       `json:"round_limit,omitempty"`
	Verification           *factory.StepVerification `json:"verification"`
}
