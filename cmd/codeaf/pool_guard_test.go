package main

// The package's floor against the pool's relay, read back where a reader can
// see it fail.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/poolcfg"
)

// TestPoolSubmitAddressUnderThePackageTestEnvironmentIsNeverTheRelay resolves
// the pool's config the way the binary resolves one — the stored words beside
// the process environment, over whatever profile the test machine answers —
// with no pin of its own, and refuses the submit address that names the relay.
// A sweep or a hook that records rows and pins no destination of its own sends
// whatever it recorded to that address, and the rows a fixture built have no
// business leaving this package for the public pool: TestMain holds the floor
// that keeps the address off the relay, and this test is the proof it holds
// for the one test that would forget.
func TestPoolSubmitAddressUnderThePackageTestEnvironmentIsNeverTheRelay(t *testing.T) {
	cfg := config.ModelPoolAt(config.ProfileDir())
	if cfg.SubmitURL == poolcfg.DefaultSubmitURL {
		t.Fatalf("the package's test environment resolves the pool's submit address to the relay's own %s", poolcfg.DefaultSubmitURL)
	}
	if strings.HasPrefix(strings.ToLower(cfg.SubmitURL), "https://") {
		t.Fatalf("the package's test environment resolves the pool's submit address to the https address %s", cfg.SubmitURL)
	}
}
