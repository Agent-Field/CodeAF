package prebuilt

import (
	"embed"
	"io/fs"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

//go:embed definition
var definitionFS embed.FS

var (
	loadOnce sync.Once
	loaded   *spec.Bundle
	loadErr  error
)

func bundle() (*spec.Bundle, error) {
	loadOnce.Do(func() {
		files, err := readEmbedded()
		if err != nil {
			loadErr = err
			return
		}
		loaded, loadErr = spec.Decode(files)
	})
	return loaded, loadErr
}

// Files returns a copy of the embedded definition. App ⌄ → Open files shows
// these paths. Callers receive a copy so the embedded bytes stay read-only.
func Files() (spec.Files, error) {
	b, err := bundle()
	if err != nil {
		return nil, err
	}
	out := make(spec.Files, len(b.Files))
	for name, data := range b.Files {
		out[name] = append([]byte(nil), data...)
	}
	return out, nil
}

func readEmbedded() (spec.Files, error) {
	files := spec.Files{}
	err := fs.WalkDir(definitionFS, "definition", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := definitionFS.ReadFile(path)
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(path, "definition/")
		files[name] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
