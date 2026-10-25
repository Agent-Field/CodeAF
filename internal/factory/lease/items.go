package lease

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// pullItems avoids decoding finished history on an unchanged pull. Writers
// publish by rename, so identity, size and modification time detect changes
// across store instances and processes, even when timestamps are preserved.
// The desk's flock protects the cache; reserve still rechecks eligibility
// under the item's own lock before granting any lease.
func (d *Desk) pullItems() ([]factory.Item, error) {
	entries, err := os.ReadDir(d.store.Root())
	if err != nil {
		return nil, err
	}
	files := make(map[string]os.FileInfo)
	changed := d.files == nil
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(name); err != nil {
			// List also resolves legacy repository aliases from these files.
			if entry.Name() != "repos.json" && entry.Name() != "checkouts.json" {
				continue
			}
		}
		info, err := os.Stat(filepath.Join(d.store.Root(), entry.Name()))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		files[entry.Name()] = info
		old := d.files[entry.Name()]
		if old == nil || !os.SameFile(old, info) || store.FileStamp(old) != store.FileStamp(info) {
			changed = true
		}
	}
	if !changed && len(files) == len(d.files) {
		return d.items, nil
	}
	items, err := d.store.List()
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Marked != items[j].Marked {
			return items[i].Marked
		}
		// Priority is a view mark. Execution remains oldest first unless a
		// person explicitly moved queued work to the top (Marked above).
		if items[i].Created.Equal(items[j].Created) {
			return items[i].ID < items[j].ID
		}
		return items[i].Created.Before(items[j].Created)
	})
	d.items, d.files = items, files
	return items, nil
}
