package desktopbridge

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
)

// commonProviderKeyEnv is the floor of provider credential variables a shell
// must never inherit even when no model source names them: these are the ones
// other tools read, so a person's own export of one is still the engine's
// secret once the engine has it. The configured sources add to this list.
var commonProviderKeyEnv = []string{
	"OPENROUTER_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY",
	"GOOGLE_API_KEY", "DEEPSEEK_API_KEY", "MISTRAL_API_KEY", "GROQ_API_KEY",
	"XAI_API_KEY", "TOGETHER_API_KEY",
}

// providerKeyEnvNames is that floor plus every variable a model source names
// as its key, read from internal/config so there is ONE list of what a key
// variable is.
func providerKeyEnvNames() []string {
	return append(append([]string{}, commonProviderKeyEnv...), config.ProviderKeyEnvNames()...)
}

// terminalEnv is the environment for the person's shell: the engine's own,
// minus the bridge's connection secrets and every provider credential, with a
// terminal type that renders.
func terminalEnv() []string {
	drop := map[string]bool{"TERM": true, "COLORTERM": true}
	for _, name := range providerKeyEnvNames() {
		drop[name] = true
	}
	var kept []string
	for _, kv := range env.EnvironWithoutOwnedSecrets() {
		if name, _, _ := strings.Cut(kv, "="); !drop[name] {
			kept = append(kept, kv)
		}
	}
	return append(kept, "TERM=xterm-256color", "COLORTERM=truecolor")
}
