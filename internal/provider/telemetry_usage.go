package provider

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/telemetry"
)

// usageRoutingProvider preserves a named Ollama connection without guessing
// from a loopback address or port, which may serve any compatible API. An
// attempt on another billing road must be classified from its actual address.
func (c *Client) usageRoutingProvider(base string) string {
	if base == c.config.BaseURL && strings.EqualFold(strings.TrimSpace(c.config.SourceID), "ollama") {
		return "ollama"
	}
	return telemetry.RoutingProvider(base)
}
