package subharness

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Saved-list budgets bound disk work before pages or traces enter memory.
const (
	SavedListRows      = 128
	SavedListFileBytes = 1 << 20
	SavedListReadBytes = 16 << 20
	SavedListEntries   = 32768
)

// ErrSavedListTooLarge refuses the whole list rather than hiding saved work.
var ErrSavedListTooLarge = errors.New("saved harness list exceeds its read budget")

type savedReadBudget struct {
	entries int
	bytes   int64
}

// SavedBounded reads the same heads and histories as Saved, admitting directory
// entries and file bytes before loading. Immutable files are still read through
// a limit so replacement or growth cannot bypass the stat check.
func (s *Store) SavedBounded() ([]SavedHarness, error) {
	return s.saved(&savedReadBudget{})
}

// saved is the single head and history projection for both surfaces. A nil
// budget retains the terminal's unrestricted local read behavior.
func (s *Store) saved(budget *savedReadBudget) ([]SavedHarness, error) {
	var names []string
	err := budget.scan(s.dir, func(entry os.DirEntry) error {
		if entry.IsDir() && ValidName(entry.Name()) {
			names = append(names, entry.Name())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	rows := make([]SavedHarness, 0)
	for _, name := range names {
		head, err := s.savedHead(name, budget)
		if err != nil {
			return nil, err
		}
		if head == 0 {
			continue
		}
		if budget != nil && len(rows) >= SavedListRows {
			return nil, ErrSavedListTooLarge
		}
		data, err := budget.read(s.page(name, head))
		if errors.Is(err, ErrSavedListTooLarge) {
			return nil, err
		}
		if err != nil {
			continue
		}
		page, err := Decode(data)
		if err != nil || page.Id.Version != head {
			continue
		}
		row := SavedHarness{Harness: page}
		if err := s.savedHistory(name, budget, &row); errors.Is(err, ErrSavedListTooLarge) {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// savedHead keeps only the highest pointer, regardless of the number of versions.
func (s *Store) savedHead(name string, budget *savedReadBudget) (int, error) {
	head := 0
	err := budget.scan(s.harnessDir(name), func(entry os.DirEntry) error {
		if v, ok := pageVersion(entry.Name()); !entry.IsDir() && ok && v > head {
			head = v
		}
		return nil
	})
	return head, err
}

// savedHistory counts traces without retaining their paths or older content.
func (s *Store) savedHistory(name string, budget *savedReadBudget, row *SavedHarness) error {
	latest := ""
	err := budget.scan(filepath.Join(s.harnessDir(name), RunDir), func(entry os.DirEntry) error {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), pageSuffix) {
			row.Runs++
			if entry.Name() > latest {
				latest = entry.Name()
			}
		}
		return nil
	})
	if err != nil {
		row.Runs = 0
		return err
	}
	if latest == "" {
		return nil
	}
	data, err := budget.read(filepath.Join(s.harnessDir(name), RunDir, latest))
	if err != nil {
		return err
	}
	trace, err := decodeTrace(data)
	if err == nil {
		row.Last = &trace
	}
	return err
}

// scan retains only one batch of directory entries, including on large histories.
func (b *savedReadBudget) scan(path string, visit func(os.DirEntry) error) error {
	dir, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		entries, err := dir.ReadDir(128)
		if b != nil {
			b.entries += len(entries)
			if b.entries > SavedListEntries {
				return ErrSavedListTooLarge
			}
		}
		for _, entry := range entries {
			if err := visit(entry); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (b *savedReadBudget) read(path string) ([]byte, error) {
	if b == nil {
		return os.ReadFile(path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular saved harness file: %s", path)
	}
	remaining := int64(SavedListReadBytes) - b.bytes
	limit := min(int64(SavedListFileBytes), remaining)
	if info.Size() > limit {
		return nil, ErrSavedListTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	b.bytes += int64(len(data))
	if int64(len(data)) > limit {
		return nil, ErrSavedListTooLarge
	}
	return data, err
}
