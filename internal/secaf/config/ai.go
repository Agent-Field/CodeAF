package config

// AIIntegrationConfig is what remains of the original AIIntegrationConfig once
// the provider machinery is gone: the retry schedule the gates apply around a
// model call, and the model the AI gate asks for.
//
// The sec-af node built this from a dozen environment variables that chose an
// external coding-agent CLI, its binary, its model and the credentials it was
// handed. Inside codeaf none of that exists — the App behind the audit runs
// codeaf's own agent loop and model API, which already know their model, their
// key and their turn budget — so the provider, harness model, turn count,
// binary paths and provider environment were deleted rather than carried as
// settings nothing reads. What is left is plain configuration the codeaf side
// may set; DefaultAIConfig gives the values the node defaulted to.
type AIIntegrationConfig struct {
	// AIModel is the model the AI gate names on its `.ai()` calls. Empty means
	// "name none", leaving the choice to the App's AI implementation, which is
	// what every agent outside the gate already does.
	AIModel string `json:"ai_model"`
	// MaxRetries is a count of RETRIES after the first attempt, so 0 means one
	// attempt and a negative value means none (see gates.runWithRetry).
	MaxRetries int `json:"max_retries"`
	// InitialBackoffSeconds and MaxBackoffSeconds bound the exponential delay
	// between attempts: min(initial * 2**attempt, max).
	InitialBackoffSeconds float64 `json:"initial_backoff_seconds"`
	MaxBackoffSeconds     float64 `json:"max_backoff_seconds"`
}

// The retry defaults the node read from SEC_AF_AI_MAX_RETRIES,
// SEC_AF_AI_INITIAL_BACKOFF_SECONDS and SEC_AF_AI_MAX_BACKOFF_SECONDS when those
// variables were unset. They are constants now because nothing in codeaf sets
// them from the environment.
const (
	DefaultAIMaxRetries            = 3
	DefaultAIInitialBackoffSeconds = 2.0
	DefaultAIMaxBackoffSeconds     = 8.0
)

// DefaultAIConfig returns the configuration a gate built without one uses: the
// node's retry schedule, and no model of its own.
func DefaultAIConfig() AIIntegrationConfig {
	return AIIntegrationConfig{
		MaxRetries:            DefaultAIMaxRetries,
		InitialBackoffSeconds: DefaultAIInitialBackoffSeconds,
		MaxBackoffSeconds:     DefaultAIMaxBackoffSeconds,
	}
}
