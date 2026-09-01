//go:build aforge_packed_manual

package manual

import (
	_ "embed"

	"github.com/Agent-Field/aforge-v2/internal/packed"
)

// The shipped binary carries the generated archives, not the raw Markdown.
// They are ignored build products: make build always regenerates them before
// selecting this file, so branches no longer edit the same binary on every
// manual change.

//go:embed pages.pack.gz
var residentArchive []byte

//go:embed chat.pack.gz
var chatArchive []byte

var (
	residentFiles corpusFiles = packed.New(residentArchive)
	chatFiles     corpusFiles = packed.New(chatArchive)
)
