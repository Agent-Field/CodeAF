package manual

// AN OVERLAY IS THE PACKED CORPUS PLUS PAGES THAT EXIST ONLY ON THIS MACHINE.
// The chat's manual is compiled into the binary and never changes at run time,
// which is the right property for every page about codeaf itself. A delegate
// (internal/delegate) is not codeaf: it is an outside program installed by the
// person, with a command row that exists only where it does, and the page that
// explains it ships beside its manifest. So the corpus the chat answers from is
// the packed one with those pages layered over it — searched, listed and read
// with the packed pages and by the same law — and the packed corpus itself is
// untouched.

import (
	"errors"
	"os"
	"path"
	"sort"
	"strings"
)

// layeredFiles is a corpusFiles over another, with pages of its own that are
// listed and read as though they sat in the same folder. A page whose name is
// already in the base is the base's: the overlay adds and never replaces, so
// nothing installed on a machine can rewrite what the binary says about itself.
type layeredFiles struct {
	base  corpusFiles
	dir   string
	extra map[string]string
}

func (f layeredFiles) Glob(pattern string) ([]string, error) {
	entries, err := f.base.Glob(pattern)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry] = true
	}
	names := make([]string, 0, len(f.extra))
	for name := range f.extra {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := path.Join(f.dir, name+".md")
		if !seen[entry] {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (f layeredFiles) ReadFile(name string) ([]byte, error) {
	if data, err := f.base.ReadFile(name); err == nil {
		return data, nil
	}
	if path.Dir(name) == f.dir {
		if text, ok := f.extra[strings.TrimSuffix(path.Base(name), ".md")]; ok {
			return []byte(text), nil
		}
	}
	return nil, errors.Join(os.ErrNotExist, errors.New("manual: no page "+name))
}

// WithPages is this corpus with extra pages layered over it, keyed by page
// name (no folder, no `.md`). It is a new corpus, built lazily on its first
// question like any other, and the receiver is not changed. Extra pages follow
// every rule the packed ones do — a `# ` title, `## ` headings as the search
// index — because they are indexed by the same code. A name the packed corpus
// already has is left to the packed page.
//
// No pages is the receiver itself, so a caller may ask unconditionally.
func (c *Corpus) WithPages(extra map[string]string) *Corpus {
	if len(extra) == 0 {
		return c
	}
	pages := make(map[string]string, len(extra))
	for name, text := range extra {
		name = strings.TrimSpace(name)
		if name == "" || strings.TrimSpace(text) == "" {
			continue
		}
		pages[name] = text
	}
	if len(pages) == 0 {
		return c
	}
	return newCorpus(layeredFiles{base: c.files, dir: path.Dir(c.glob), extra: pages}, c.glob)
}
