package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// WHAT A CHANGE ENTRY IS FOR, WHICH IS NOT WHAT A COMMIT MESSAGE IS FOR. The
// commit says what somebody did. This says what somebody else — usually a model
// opening this repository with a fortnight-old memory of it — now believes
// wrongly. Those are different documents and only one of them can be generated.
//
// Everything here is small on purpose. Five fields, three of them usually, and a
// paragraph. A form long enough to resent is a form that gets a placeholder
// typed into it, and a changelog of placeholders is worse than none because it
// reads as though somebody checked.
type Entry struct {
	Kind        string   `yaml:"kind"`
	Title       string   `yaml:"title"`
	PR          int      `yaml:"pr"`
	Surface     []string `yaml:"surface"`
	Invalidates []string `yaml:"invalidates"`

	// Everything after the frontmatter: why, in a person's words. Optional, and
	// genuinely optional — most changes are exactly their title.
	Body string `yaml:"-"`
	// Where it was read from, so a failure can name the file to open.
	Path string `yaml:"-"`
}

// The kinds, in the order they are rendered. `renamed` earns its own kind rather
// than folding into `changed` because a name that moved is the single most
// common way a stale memory turns into a wasted hour — a branch, a tool, a
// command, a file that answers to something else now.
var kinds = []struct {
	key     string
	heading string
}{
	{"added", "Added"},
	{"changed", "Changed"},
	{"renamed", "Renamed"},
	{"fixed", "Fixed"},
	{"removed", "Removed"},
	{"internal", "Internal"},
}

// The surfaces, spelled the way CLAUDE.md spells them. A closed set and not free
// text, because the whole value of the field is that `grep 'surface:.*chat'`
// finds every one of them — a field where three people write "tui3", "chat v3"
// and "v3" is a field that answers no question.
var surfaces = map[string]string{
	"chat":     "internal/tui3 — the v3 surface",
	"engine":   "internal/session — the turn loop, the toolbelt, tasks",
	"resident": "internal/head, internal/resident",
	"remote":   "--host, --at, attachments",
	"build":    "the Makefile, CI, the branch rules, the shipped binary",
	"docs":     "prose, the manual, the rules",
}

var (
	fileName  = regexp.MustCompile(`^(\d+)-[a-z0-9]+(-[a-z0-9]+)*\.md$`)
	frontOpen = "---\n"
)

// ParseEntry reads one entry and says everything wrong with it at once. Not the
// first thing wrong — everything, because a gate that reveals one fault per run
// is a gate somebody bounces off four times and then works around.
func ParseEntry(path string) (Entry, []error) {
	var e Entry
	e.Path = path

	raw, err := os.ReadFile(path)
	if err != nil {
		return e, []error{err}
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")

	if !strings.HasPrefix(text, frontOpen) {
		return e, []error{fmt.Errorf("does not start with a `---` frontmatter block")}
	}
	rest := text[len(frontOpen):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return e, []error{fmt.Errorf("the frontmatter block is never closed with a `---` line")}
	}
	if err := yaml.Unmarshal([]byte(rest[:end+1]), &e); err != nil {
		return e, []error{fmt.Errorf("the frontmatter is not valid YAML: %w", err)}
	}
	e.Body = strings.TrimSpace(rest[end+len("\n---\n"):])
	e.Path = path

	return e, e.validate()
}

func (e Entry) validate() []error {
	var errs []error
	bad := func(f string, a ...any) { errs = append(errs, fmt.Errorf(f, a...)) }

	known := false
	for _, k := range kinds {
		if k.key == e.Kind {
			known = true
		}
	}
	if !known {
		bad("kind is %q; it has to be one of %s", e.Kind, strings.Join(kindKeys(), ", "))
	}

	// Independent tests and not a switch, here and below: a title can be both
	// too long and punctuated like a sentence, and being told about one of those
	// per run is the drip-feed this whole function exists to avoid.
	if strings.TrimSpace(e.Title) == "" {
		bad("title is empty")
	} else {
		if len(e.Title) > 100 {
			bad("title is %d characters; keep it under 100 so it reads as one line", len(e.Title))
		}
		if strings.HasSuffix(e.Title, ".") {
			bad("title ends with a full stop; it is a headline, not a sentence")
		}
	}

	if e.PR <= 0 {
		bad("pr is missing — every entry names the pull request it arrived in")
	}

	base := filepath.Base(e.Path)
	m := fileName.FindStringSubmatch(base)
	switch {
	case m == nil:
		bad("the file is named %q; it has to be <pr>-<a-lowercase-slug>.md", base)
	default:
		if n, _ := strconv.Atoi(m[1]); n != e.PR {
			bad("the file is named for pull request %s and the frontmatter says %d", m[1], e.PR)
		}
	}

	for _, s := range e.Surface {
		if _, ok := surfaces[s]; !ok {
			bad("surface %q is not one of %s", s, strings.Join(surfaceKeys(), ", "))
		}
	}

	// THE ONE QUALITY GATE, AND IT IS ON THE ONE FIELD THAT MATTERS. An
	// invalidation has to be a claim somebody can check — "the trunk was
	// chat-v3-task and it no longer exists" — and not a label like "branch
	// names". A fragment here costs the reader the whole lookup it was supposed
	// to save them, so a fragment is refused.
	for i, s := range e.Invalidates {
		s = strings.TrimSpace(s)
		if !strings.HasSuffix(s, ".") {
			bad("invalidates[%d] does not end in a full stop; write the whole claim: what used to be true, and what is true now", i)
		}
		if len(s) < 25 {
			bad("invalidates[%d] is %d characters, which is a label rather than a claim: %q", i, len(s), s)
		}
	}

	return errs
}

func kindKeys() []string {
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, k.key)
	}
	return out
}

func surfaceKeys() []string {
	out := make([]string, 0, len(surfaces))
	for s := range surfaces {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// LoadDir reads every entry in a directory and reports every fault in all of
// them together, for the same reason validate does.
func LoadDir(dir string) ([]Entry, []error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, []error{err}
	}
	sort.Strings(names)

	var (
		entries []Entry
		errs    []error
	)
	for _, n := range names {
		if strings.EqualFold(filepath.Base(n), "README.md") {
			continue
		}
		e, es := ParseEntry(n)
		for _, err := range es {
			errs = append(errs, fmt.Errorf("%s: %w", n, err))
		}
		if len(es) == 0 {
			entries = append(entries, e)
		}
	}
	return entries, errs
}
