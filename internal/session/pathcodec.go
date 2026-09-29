package session

// pathcodec.go is how a sealed file names a folder. A path written into .cell/
// travels to machines where the folder is somewhere else, so it is never
// absolute (law L1): it is a base's NAME and the remainder beneath it, spelled
// "workspace:sub/dir". One codec encodes every path on save and resolves every
// one on load; a writer supplies the fields, never the spelling.
//
// A path under no base (another drive, a system folder) has no honest relative
// spelling: it is machine-local by nature and is not persisted.

import (
	"os"
	"path/filepath"
	"strings"
)

const refSeparator = ":"

// pathBase is one named root a path may be spelled against.
type pathBase struct{ name, root string }

// pathCodec is the bases of one session folder, most specific first, so the
// smallest remainder wins.
type pathCodec []pathBase

// codecFor builds the codec of the session folder dir whose local workspace is
// workspace. A base with no root on this machine is left out: nothing is
// spelled against it and nothing resolves through it.
func codecFor(dir, workspace string) pathCodec {
	home, _ := os.UserHomeDir()
	var c pathCodec
	for _, b := range []pathBase{{"session", dir}, {"workspace", workspace}, {"home", home}} {
		if strings.TrimSpace(b.root) != "" {
			c = append(c, pathBase{b.name, filepath.Clean(b.root)})
		}
	}
	return c
}

// encode spells abs against the first base that holds it.
func (c pathCodec) encode(abs string) (string, bool) {
	abs = filepath.Clean(abs)
	for _, b := range c {
		if rel, err := filepath.Rel(b.root, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return b.name + refSeparator + filepath.ToSlash(rel), true
		}
	}
	return "", false
}

// resolve is encode's inverse. A value that is already absolute is a file an
// older build wrote, and it stands as written; a name no base answers to is
// unresolvable.
func (c pathCodec) resolve(ref string) (string, bool) {
	if filepath.IsAbs(ref) {
		return ref, true
	}
	name, rel, _ := strings.Cut(ref, refSeparator)
	for _, b := range c {
		if b.name == name {
			return filepath.Join(b.root, filepath.FromSlash(rel)), true
		}
	}
	return "", false
}

// pathFields is a record that names folders: pointers to every one of them.
type pathFields interface{ pathFields() []*string }

func (p *PlaceRef) pathFields() []*string { return []*string{&p.Path} }

func (t *StandingTree) pathFields() []*string { return []*string{&t.Folder, &t.Dir, &t.Root} }

// mapPaths returns a copy of in with every non-empty path field rewritten by
// f. A record with a path f refuses is dropped: half a record would name a
// folder it does not stand on.
func mapPaths[T any, P interface {
	*T
	pathFields
}](in []T, f func(string) (string, bool)) []T {
	var out []T
	for _, rec := range in {
		if mapRecord[T, P](&rec, f) {
			out = append(out, rec)
		}
	}
	return out
}

func mapRecord[T any, P interface {
	*T
	pathFields
}](rec *T, f func(string) (string, bool)) bool {
	for _, field := range P(rec).pathFields() {
		if *field == "" {
			continue
		}
		mapped, ok := f(*field)
		if !ok {
			return false
		}
		*field = mapped
	}
	return true
}

// mapTruth rewrites every path the truth holds through f; the launch
// directory is dropped when f refuses it.
func mapTruth(t SessionTruth, f func(string) (string, bool)) SessionTruth {
	if t.LaunchDir != "" {
		t.LaunchDir, _ = f(t.LaunchDir)
	}
	t.Places = mapPaths[PlaceRef](t.Places, f)
	t.Trees = mapPaths[StandingTree](t.Trees, f)
	return t
}
