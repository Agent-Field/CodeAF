package manual

// THE APP'S OWN PAGES. A desktop app that holds this engine has a surface of its
// own — its windows, its assistant, its settings — and the chat's manual says
// nothing about it, because the pages here describe only what ships in this
// binary. So the app hands its pages over at start ([SetAppPages]) and the
// manual tool reads them beside the chat's, each filed under [AppPrefix] so a
// reader can always tell an app page from one of codeaf's. They are kept in
// memory only and replaced whole by every set; the chat corpus itself is never
// changed, so nothing else that reads it (a self-question cue, a truth test)
// moves when an app registers pages.

import (
	"fmt"
	"path"
	"regexp"
	"sync/atomic"
)

const (
	// AppPrefix is the folder every app page is filed under.
	AppPrefix = "workaf/"
	// MaxAppPages bounds one set; MaxAppPageBytes bounds one page and
	// MaxAppPagesBytes the whole set. A page is written under the same
	// two-thousand-character section law as the chat's own.
	MaxAppPages      = 64
	MaxAppPageBytes  = 32 << 10
	MaxAppPagesBytes = 512 << 10
)

// AppPage is one page the app registers: a slug and its Markdown.
type AppPage struct {
	Name     string
	Markdown string
}

// appPageName is a page's slug: lowercase words joined by hyphens.
var appPageName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// joined is the chat corpus with the app's pages beside it, nil when the app
// registered none.
var joined atomic.Pointer[Corpus]

// SetAppPages replaces every app page with pages, checked whole first: a set
// that breaks a bound changes nothing. An empty set removes them all.
func SetAppPages(pages []AppPage) error {
	files, err := appPageFiles(pages)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		joined.Store(nil)
		return nil
	}
	joined.Store(newCorpus(withPages{base: chatFiles, extra: files}, "chat/*.md"))
	return nil
}

// WithApp is the corpus the manual tool reads: the chat's own pages and the
// app's, or [Chat] itself while the app has registered none.
func WithApp() *Corpus {
	if c := joined.Load(); c != nil {
		return c
	}
	return Chat()
}

// appPageFiles is pages as files under the chat folder's own glob, filed at
// chat/workaf/<name>.md so their page names carry the prefix.
func appPageFiles(pages []AppPage) (map[string][]byte, error) {
	if len(pages) > MaxAppPages {
		return nil, fmt.Errorf("at most %d app pages", MaxAppPages)
	}
	files := make(map[string][]byte, len(pages))
	total := 0
	for _, page := range pages {
		if !appPageName.MatchString(page.Name) || len(page.Name) > 64 {
			return nil, fmt.Errorf("app page name %q is not a lowercase slug of at most 64 characters", page.Name)
		}
		if len(page.Markdown) > MaxAppPageBytes {
			return nil, fmt.Errorf("app page %s is over %d bytes", page.Name, MaxAppPageBytes)
		}
		total += len(page.Markdown)
		key := "chat/" + AppPrefix + page.Name + ".md"
		if _, twice := files[key]; twice {
			return nil, fmt.Errorf("two app pages are named %s", page.Name)
		}
		files[key] = []byte(page.Markdown)
	}
	if total > MaxAppPagesBytes {
		return nil, fmt.Errorf("the app pages are over %d bytes together", MaxAppPagesBytes)
	}
	return files, nil
}

// withPages is a corpus's files with more beside them, matched by the same
// glob one folder down.
type withPages struct {
	base  corpusFiles
	extra map[string][]byte
}

func (w withPages) Glob(pattern string) ([]string, error) {
	found, err := w.base.Glob(pattern)
	if err != nil {
		return nil, err
	}
	below := path.Join(path.Dir(pattern), "*", path.Base(pattern))
	for name := range w.extra {
		if ok, _ := path.Match(below, name); ok {
			found = append(found, name)
		}
	}
	return found, nil
}

func (w withPages) ReadFile(name string) ([]byte, error) {
	if raw, ok := w.extra[name]; ok {
		return raw, nil
	}
	return w.base.ReadFile(name)
}
