//go:build aforge_packed_manual

package manual

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/packed"
)

// TestThePackedPagesAreTheFoldersOnDisk checks the exact source mode make
// build ships. The archives are generated and ignored rather than committed,
// so this is a build-product check, not a repository freshness check.
func TestThePackedPagesAreTheFoldersOnDisk(t *testing.T) {
	for archive, folder := range map[*[]byte]string{
		&residentArchive: "pages",
		&chatArchive:     "chat",
	} {
		if err := packed.Verify(*archive, folder); err != nil {
			t.Errorf("%v\n\nrun: go generate ./internal/manual", err)
		}
	}
}
