package apppackage

import (
	"embed"
	"fmt"
)

//go:embed firstparty/*.codeaf-app
var releases embed.FS

// Builtins reads the release catalog through the same verifier as an import.
func Builtins() ([]Archive, error) {
	entries, err := releases.ReadDir("firstparty")
	if err != nil {
		return nil, err
	}
	out := make([]Archive, 0, len(entries))
	for _, entry := range entries {
		path := "firstparty/" + entry.Name()
		f, err := releases.Open(path)
		if err != nil {
			return nil, err
		}
		a, err := Read(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		if a.Manifest.Publisher != BuiltinPublisher {
			return nil, fmt.Errorf("%s needs a release signature", entry.Name())
		}
		out = append(out, a)
	}
	return out, nil
}
