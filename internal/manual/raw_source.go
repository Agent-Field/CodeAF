//go:build !aforge_packed_manual

package manual

import (
	"embed"
	"io/fs"
)

// rawPages keeps ordinary go build, go test, and go vet usable immediately
// after checkout. The release binary does not carry these raw bytes: make
// build selects packed_source.go with the aforge_packed_manual build tag.
//
//go:embed pages/*.md chat/*.md
var rawPages embed.FS

type embeddedPages struct{ fs embed.FS }

func (pages embeddedPages) Glob(pattern string) ([]string, error) {
	return fs.Glob(pages.fs, pattern)
}

func (pages embeddedPages) ReadFile(name string) ([]byte, error) {
	return pages.fs.ReadFile(name)
}

var (
	residentFiles corpusFiles = embeddedPages{fs: rawPages}
	chatFiles     corpusFiles = embeddedPages{fs: rawPages}
)
