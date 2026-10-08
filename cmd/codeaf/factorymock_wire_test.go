//go:build factorymock

package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// The moving mock wins over the person's own store when it is switched on, so
// a tagged build with CODEAF_FACTORY_MOCK=1 draws the floor it always drew.
func TestFactoryMockWinsOverAStore(t *testing.T) {
	t.Setenv("CODEAF_FACTORY_MOCK", "1")
	t.Setenv(factoryFixtureEnv, "1")
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seam := factorySeam(st, "", "")
	if !seam.Has("launch") || !seam.Has("sleep") {
		t.Fatal("the mock was switched on and the floor is not the moving mock")
	}
}
