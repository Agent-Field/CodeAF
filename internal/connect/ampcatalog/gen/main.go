// Command gen writes internal/connect/ampcatalog/providers.json from the
// amp-labs catalog.
//
// It is the only thing in this repository that imports the catalog library, and
// it is a main package that nothing imports, so the library is a build-time
// requirement and not a linked dependency of the aforge binary. That is the
// whole point of the exercise — see ../ampcatalog.go.
//
// Run it with `go generate ./internal/connect/ampcatalog` and read the diff.
// The rows are re-encoded through the mirror types rather than copied field by
// field, so a field the mirror does not declare cannot leak into the snapshot,
// and a field it declares under the library's own JSON name arrives without
// this file mentioning it.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	amp "github.com/amp-labs/connectors/providers"

	"github.com/Agent-Field/aforge-v2/internal/connect/ampcatalog"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ampcatalog:", err)
		os.Exit(1)
	}
}

func run() error {
	names := amp.AllNames()
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })

	rows := make(map[ampcatalog.Provider]*ampcatalog.ProviderInfo, len(names))
	for _, name := range names {
		info, err := amp.ReadInfo(name)
		if err != nil || info == nil {
			// A name the library will not read is a name aforge already skips.
			continue
		}
		cut, err := narrow(info)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		rows[ampcatalog.Provider(name)] = cut
	}
	if len(rows) == 0 {
		return fmt.Errorf("the catalog came back empty; refusing to write a snapshot")
	}

	// Indented and key-sorted, because the point of committing this file is
	// that a refresh can be reviewed as a diff.
	encoded, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')

	destination := filepath.Join("providers.json")
	if err := os.WriteFile(destination, encoded, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "ampcatalog: wrote %d providers to %s\n", len(rows), destination)
	return nil
}

// narrow re-encodes one library row through the mirror types. The library's
// JSON names are the mirror's JSON names, so this is a projection: every field
// the mirror declares is carried, and every field it does not is dropped.
func narrow(info *amp.ProviderInfo) (*ampcatalog.ProviderInfo, error) {
	full, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	var cut ampcatalog.ProviderInfo
	if err := json.Unmarshal(full, &cut); err != nil {
		return nil, err
	}
	return &cut, nil
}
