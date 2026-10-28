//go:build relayurl

package relayconf

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/relayserve/fabrictest"
)

func TestFabricConformance(t *testing.T) {
	base := baseURL(t)
	fabrictest.Run(t, func(t *testing.T) *fabrictest.Rig {
		return &fabrictest.Rig{URL: func() string { return base }, Client: httpClient, Now: wall}
	})
}
