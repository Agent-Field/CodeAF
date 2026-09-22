package delegate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/home"
)

// dirName is the folder under the state root the manifests live in: one
// `<name>.json` and one `<name>.md` per delegate.
const dirName = "delegates"

// Dir is where this machine's delegates are: `$CODEAF_HOME/delegates`, which
// is `~/.codeaf/delegates` for a person and a throwaway root under test.
func Dir() string { return filepath.Join(home.Dir(), dirName) }

// Refusal is one manifest the loader would not admit, and why, in the sentence
// `/delegate` draws for it. A refusal is never an error to the caller: a
// registry with a bad file in it is still a registry, and the person is told
// which file and what is wrong rather than losing every delegate to one typo.
type Refusal struct {
	// Name is the file's stem, which is what the person will look for.
	Name   string
	Reason string
}

func (r Refusal) String() string { return r.Name + ": " + r.Reason }

// Absent is a manifest whose program is not on this machine. It is not a
// refusal — the file is fine — and it is not offered either: A CAPABILITY THAT
// CANNOT WORK IS ABSENT, NOT BROKEN. It is kept so `/delegate` can draw one dim
// line naming the binary it looked for.
type Absent struct {
	Name string
	Bin  string
}

func (a Absent) String() string { return a.Name + ": " + a.Bin + " is not on this machine" }

// Registry is what one launch knows about the delegates installed here: the
// ones it can run, the ones whose program is missing, and the files it would
// not admit. It is read once at launch and never watched; a manifest added
// while codeaf runs is seen at the next launch, which the manual page says.
type Registry struct {
	entries  map[string]Manifest
	absent   []Absent
	refusals []Refusal
}

// Load reads every `<name>.json` in dir. A missing directory is MADE, so the
// person who goes to install a delegate finds the folder waiting rather than
// reading its name off a page; it is then an empty registry and no error, since
// most machines have no delegates. An error is only a directory that exists and
// cannot be read: a folder that cannot be made is read as missing, because the
// registry is not worth failing a launch over.
//
// THE LAW IS CHECKED HERE, at the moment the command comes into existence: a
// manifest whose manual page is missing or does not spell `/<name>` is refused
// with the same shape of sentence the compile-time gate prints for a built-in
// command. The static command table keeps its static test; this is that test
// moved to load time for rows that cannot be in the table.
func Load(dir string) (*Registry, error) {
	registry := &Registry{entries: map[string]Manifest{}}
	_ = os.MkdirAll(dir, 0o755)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return registry, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), ".json")
		manifest, err := readManifest(filepath.Join(dir, entry.Name()))
		if err != nil {
			registry.refusals = append(registry.refusals, Refusal{Name: stem, Reason: err.Error()})
			continue
		}
		if manifest.Name != stem {
			registry.refusals = append(registry.refusals, Refusal{Name: stem,
				Reason: fmt.Sprintf("the file is %s.json but the manifest says its name is %q; the two must agree", stem, manifest.Name)})
			continue
		}
		manifest.ManualPath = filepath.Join(dir, stem+".md")
		page, reason := readManualPage(manifest)
		if reason != "" {
			registry.refusals = append(registry.refusals, Refusal{Name: stem, Reason: reason})
			continue
		}
		manifest.Manual = page
		bin, err := resolveBin(manifest.Bin, dir)
		if err != nil {
			registry.absent = append(registry.absent, Absent{Name: stem, Bin: manifest.Bin})
			continue
		}
		manifest.BinPath = bin
		registry.entries[manifest.Name] = manifest
	}
	sort.Slice(registry.absent, func(i, j int) bool { return registry.absent[i].Name < registry.absent[j].Name })
	sort.Slice(registry.refusals, func(i, j int) bool { return registry.refusals[i].Name < registry.refusals[j].Name })
	return registry, nil
}

// readManifest parses and validates one file.
func readManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("the manifest does not parse: %v", err)
	}
	manifest.Path = path
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// readManualPage is the load-time manual law. The page must exist and must
// say the command, because the chat answers "what does /<name> do" from it and
// nowhere else. It answers the page's text, or the refusal.
func readManualPage(m Manifest) (string, string) {
	page, err := os.ReadFile(m.ManualPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Sprintf("no manual page beside it — write %s.md saying what /%s does — not added", m.Name, m.Name)
	}
	if err != nil {
		return "", "its manual page could not be read: " + err.Error()
	}
	if !strings.Contains(string(page), "/"+m.Name) {
		return "", fmt.Sprintf("its manual page does not say /%s — not added", m.Name)
	}
	return strings.TrimSpace(strings.ReplaceAll(string(page), "\r\n", "\n")), ""
}

// resolveBin finds the program. A name with no separator is looked up on
// PATH; a relative path is taken from the manifest's own directory, so a
// delegate can ship its binary beside its manifest; an absolute path is
// itself. Whatever is found must be a regular executable file.
func resolveBin(bin, dir string) (string, error) {
	if !strings.ContainsRune(bin, os.PathSeparator) {
		return exec.LookPath(bin)
	}
	if !filepath.IsAbs(bin) {
		bin = filepath.Join(dir, bin)
	}
	info, err := os.Stat(bin)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not an executable file", bin)
	}
	return bin, nil
}

// Find answers the manifest for a name, and false when this machine has none
// by that name (including one that is absent or refused).
func (r *Registry) Find(name string) (Manifest, bool) {
	if r == nil {
		return Manifest{}, false
	}
	m, ok := r.entries[name]
	return m, ok
}

// Names is every runnable delegate, sorted, which is the order rows are drawn
// in.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// All is every runnable manifest in Names order.
func (r *Registry) All() []Manifest {
	names := r.Names()
	all := make([]Manifest, 0, len(names))
	for _, name := range names {
		all = append(all, r.entries[name])
	}
	return all
}

// Absent is the manifests whose program is not here, sorted by name.
func (r *Registry) Absent() []Absent {
	if r == nil {
		return nil
	}
	return append([]Absent(nil), r.absent...)
}

// Refusals is the files the loader would not admit, sorted by name.
func (r *Registry) Refusals() []Refusal {
	if r == nil {
		return nil
	}
	return append([]Refusal(nil), r.refusals...)
}

// PagePrefix is what a delegate's manual page is called in the chat's corpus:
// `delegate-<name>`, so a delegate can never wear a packed page's name.
const PagePrefix = "delegate-"

// Pages is every runnable delegate's manual page, keyed by its corpus name,
// for the chat's manual to layer over its own (internal/manual's overlay).
func (r *Registry) Pages() map[string]string {
	if r == nil {
		return nil
	}
	pages := make(map[string]string, len(r.entries))
	for name, m := range r.entries {
		if m.Manual != "" {
			pages[PagePrefix+name] = m.Manual
		}
	}
	return pages
}

// Empty is a registry with nothing runnable, nothing absent and nothing
// refused: the machine has no delegates at all, which is most machines.
func (r *Registry) Empty() bool {
	return r == nil || (len(r.entries) == 0 && len(r.absent) == 0 && len(r.refusals) == 0)
}
