//go:build relayurl

package relayconf

import (
	"flag"
	"testing"

	"github.com/Agent-Field/codeaf/internal/relayserve/fabrictest"
)

var fabricUnsupported = flag.Bool("relay-fabric-unsupported", false, "relay has not implemented scoped clients, slots and queue yet")

func TestFabricConformance(t *testing.T) {
	if *fabricUnsupported {
		t.Skip("Worker scoped clients, slots and queue are not implemented yet")
	}
	base := baseURL(t)
	fabrictest.Run(t, func(t *testing.T) *fabrictest.Rig {
		return &fabrictest.Rig{URL: func() string { return base }, Client: httpClient, Now: wall}
	})
}
