// Package celltest provides temporary folders for tests that spawn engine daemons.
package celltest

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/cellstore"
)

// EngineDir returns a fresh folder and stops this process's spawned daemons
// before testing removes its temporary folders. Call it before spawning engines.
func EngineDir(t testing.TB) string {
	t.Helper()
	// TempDir registers removal of the shared parent on its first call.
	dir := t.TempDir()
	t.Cleanup(cellstore.StopSpawned)
	return dir
}
