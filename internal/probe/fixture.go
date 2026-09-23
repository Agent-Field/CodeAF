package probe

// fixture.go is the FIXTURE LAYER: reproducible, schema/version aware,
// isolated from the real user home. It is product state a probe session is
// pointed at — never actor behavior, never a persona (the design line: the
// fixture is what the product sees, the profile is what the actor does).
//
// ISOLATION, on the pattern of scripts/hosted-drive.sh: every fixture lives
// under a private per-run root the caller owns (0700 throughout). A fixture's
// home directory IS the HOME the pinned codeaf binary is launched with, so
// everything the product writes — its state, its conversations, its config —
// lands inside the fixture, never in the real user's home. Nothing here reads
// the caller's home, opens a network connection, or makes a model call;
// prepare and reset are pure local file work.
//
// SCENARIOS:
//
//	clean      a fresh, empty HOME: no .codeaf state at all, no history.
//	returning  a home a previous user of the product left behind: one project
//	           workspace on disk and one past conversation written through the
//	           product's own writer (session.SaveMeta), at fixed times.
//
// SEEDING LAW, stated where it binds: a returning-user scenario is seeded ONLY
// through supported product writers or the controlled versioned seed helpers
// in this file. No fixture code opens, copies or edits the product's database
// files (the memory store, the spending ledger, the FTS index) directly to
// fake history — those stores have schemas and seals of their own, and a
// hand-written DB is a fixture that lies.
//
// LIMITS, documented because a caller deserves them in the same breath as the
// capability: this layer CANNOT seed a conversation with real transcript turns
// (the journal writer is unexported and held under a live flock —
// cmd/codeaf-demo-home documents the same limit), memory-store graph rows, the
// spending ledger, standing orders, or FTS content. A caller needing a full
// state can build one with `codeaf-demo-home -into <fixture home>` — but that
// program stamps wall-clock times, so it is NOT deterministic and its result
// verifies by structure, not by content. A returning-user fixture here
// therefore exercises the picker/home surface with a real past conversation
// identity, not a full rehearsed history.
//
// Bucket names embed the workspace's absolute path, so recorded entries carry
// a stable -HOME- placeholder for the fixture home's own encoded path (see
// canonicalPath); the tree verifies against the real one under the real root.
//
// DETERMINISM, and exactly how far it goes. Prepare and reset replay the same
// steps, and the manifest hash covers the SCHEMA and VERSION and the
// deterministic directory tree — every relative path, its kind, its mode —
// never file contents, which carry the product writers' own timestamps. Two
// prepares agree on the tree and therefore on the hash. Reset is
// verify-then-report: the existing tree is checked against its manifest first
// and any drift is reported (never silently repaired), then the steps are
// replayed from an empty directory and the fresh tree is verified against a
// fresh manifest. Deterministic verification here is local file work only; a
// live paid agent campaign over a fixture is a different, nondeterministic
// thing and is not claimed here.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// fixtureSchema is the manifest's schema version. Bump it when the manifest's
// own shape changes; fixtureVersion bumps when a scenario's tree changes.
const fixtureSchema = 1

// fixtureVersion names the current shape of the seeded trees.
const fixtureVersion = "1"

// fixtureEpoch is the fixed time every seeded record is stamped with, so a
// replay of the same scenario writes the same logical history.
var fixtureEpoch = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// FixtureState is one prepared fixture: what a probe session is pointed at.
type FixtureState struct {
	Scenario string `json:"scenario"`
	// Home is the fixture's HOME directory — hand it to the pinned binary as
	// HOME. It is always inside the probe root, never the real one.
	Home string `json:"home"`
	// Seeded says whether the scenario wrote product state (a returning user)
	// rather than an empty home.
	Seeded bool `json:"seeded"`
	// Hash is the manifest hash: schema + version + the deterministic tree.
	Hash string `json:"hash"`
	// Verified reports whether the returned tree matched its manifest.
	Verified bool `json:"verified"`
	// Drift is what reset found WRONG before it rebuilt: "" when the old tree
	// matched its manifest. Verify-then-report: drift is reported, not fixed
	// quietly.
	Drift string `json:"drift,omitempty"`
	// manifest is the built manifest, carried so the caller can verify again.
	manifest *FixtureManifest
}
type FixtureManifest struct {
	Schema         int            `json:"schema"`
	FixtureVersion string         `json:"fixtureVersion"`
	Scenario       string         `json:"scenario"`
	Hash           string         `json:"hash"`
	Entries        []fixtureEntry `json:"entries"`
}

// fixtureEntry is one path in the deterministic tree.
type fixtureEntry struct {
	Path string `json:"path"` // relative to the fixture's HOME
	Kind string `json:"kind"` // "dir" or "file"
	Mode uint32 `json:"mode"` // 0700 for dirs, 0600 for files
}

// scenario is one named fixture recipe.
type scenario struct {
	name        string
	description string
	seed        func(home string) error
}

// scenarios is the registry. Add a scenario by naming a seed that uses only
// supported product writers or the controlled helpers in this file.
var scenarios = map[string]scenario{
	"clean": {
		name:        "clean",
		description: "a fresh isolated HOME with no state and no history",
		seed:        seedClean,
	},
	"returning": {
		name:        "returning",
		description: "a HOME a returning user left: one project, one past conversation (session.SaveMeta)",
		seed:        seedReturning,
	},
}

// FixtureScenarios names the scenarios a caller can ask for.
func FixtureScenarios() []string {
	names := make([]string, 0, len(scenarios))
	for name := range scenarios {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// FixturePrepare prepares the named scenario under the private probe root and
// records its manifest. Idempotent: a root already holding a verified fixture
// of the same scenario and version is returned as it stands, so a second
// prepare answers the same manifest hash without rebuilding. The root is held
// at 0700; nothing outside it is ever touched.
func FixturePrepare(root, name string) (*FixtureState, error) {
	sc, err := lookupScenario(name)
	if err != nil {
		return nil, err
	}
	if err := ensurePrivateRoot(root); err != nil {
		return nil, err
	}
	dir := fixtureDir(root, name)

	// Idempotence: a present manifest of the same schema, version and
	// scenario whose tree still verifies means the work is already done.
	if m, err := readManifest(dir); err == nil {
		if m.Schema == fixtureSchema && m.FixtureVersion == fixtureVersion && m.Scenario == name {
			if err := verifyTree(dir, m); err == nil {
				return &FixtureState{
					Scenario: name,
					Home:     filepath.Join(dir, "home"),
					Seeded:   m.seeded(),
					Hash:     m.Hash,
					Verified: true,
				}, nil
			}
		}
	}

	st, err := buildFixture(dir, name, sc)
	if err != nil {
		return nil, err
	}
	// A manifest that does not describe the tree just written is a fixture
	// that lies, so every prepare verifies itself before it returns.
	if err := verifyTree(dir, st.manifest); err != nil {
		return nil, fmt.Errorf("fixture: prepared tree does not match its own manifest: %w", err)
	}
	st.Verified = true
	return st, nil
}

// FixtureReset replays the prepare steps for the named scenario and reports
// what it found. Verify-then-report: the existing tree is checked against its
// manifest FIRST (any drift is carried back in Drift, never silently
// repaired), then the steps are replayed from an empty directory, a fresh
// manifest is written, and the fresh tree is verified against it. No model
// calls, no network: reset is local file work only.
func FixtureReset(root, name string) (*FixtureState, error) {
	sc, err := lookupScenario(name)
	if err != nil {
		return nil, err
	}
	if err := ensurePrivateRoot(root); err != nil {
		return nil, err
	}
	dir := fixtureDir(root, name)

	drift := ""
	if m, err := readManifest(dir); err == nil {
		if err := verifyTree(dir, m); err != nil {
			drift = err.Error()
		}
	} else {
		drift = "no manifest present"
	}

	st, err := buildFixture(dir, name, sc)
	if err != nil {
		return nil, err
	}
	if err := verifyTree(dir, st.manifest); err != nil {
		return nil, fmt.Errorf("fixture: reset tree does not match its own manifest: %w", err)
	}
	st.Verified = true
	st.Drift = drift
	return st, nil
}

// buildFixture replays a scenario's steps from an empty fixture directory and
// writes a fresh manifest over what was built.
func buildFixture(dir, name string, sc scenario) (*FixtureState, error) {
	if err := os.RemoveAll(dir); err != nil {
		return nil, fmt.Errorf("fixture: clear %s: %w", dir, err)
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("fixture: make home: %w", err)
	}
	if err := sc.seed(home); err != nil {
		return nil, fmt.Errorf("fixture: seed %q: %w", name, err)
	}
	m, err := buildManifest(dir, name)
	if err != nil {
		return nil, err
	}
	if err := writeManifest(dir, m); err != nil {
		return nil, err
	}
	return &FixtureState{
		Scenario: name,
		Home:     home,
		Seeded:   m.seeded(),
		Hash:     m.Hash,
		manifest: m,
	}, nil
}

// FixtureEnviron is the sanitized environment a pinned binary runs with
// inside a fixture: the fixture home as HOME, a trimmed variable set.
// Credentials and whatever the caller's shell happened to hold do not ride
// along; a caller who genuinely needs a variable passes it in extra
// deliberately. CODEAF_HOME and its legacy spelling are left unset, not
// pointed anywhere: the state root must resolve under the fixture HOME, never
// under the real one.
func FixtureEnviron(home string, extra ...string) []string {
	env := []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"TMUX_TMPDIR=" + filepath.Dir(home), // a tmux socket beside the fixture, not in the real one
		"TERM=xterm-256color",
		"LANG=en_US.UTF-8",
	}
	for _, kv := range extra {
		env = append(env, kv)
	}
	return env
}

// lookupScenario resolves a scenario name, refusing an unknown one by naming
// what IS known.
func lookupScenario(name string) (scenario, error) {
	sc, ok := scenarios[name]
	if !ok {
		return scenario{}, fmt.Errorf("fixture: unknown scenario %q (known: %s)", name, strings.Join(FixtureScenarios(), ", "))
	}
	return sc, nil
}

// fixtureDir is where one scenario's fixture lives: a directory of its own
// under the private root, holding home/ and manifest.json.
func fixtureDir(root, name string) string {
	return filepath.Join(root, "fixtures", sanitizeName(name))
}

// sanitizeName keeps a scenario name from walking out of the fixtures
// directory.
func sanitizeName(name string) string {
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return -1
	}, name)
	if clean == "" {
		clean = "unnamed"
	}
	return clean
}

// ensurePrivateRoot makes the per-run root if needed and holds it at 0700.
func ensurePrivateRoot(root string) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("fixture: no probe root")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("fixture: make root: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return fmt.Errorf("fixture: hold root private: %w", err)
	}
	return nil
}

// seedClean is the empty home: nothing on the disk but the directory itself.
func seedClean(home string) error {
	return nil
}

// returningProject is the workspace the returning scenario puts on the disk.
const returningProjectName = "ledger"

// returningSessionID is the past conversation's id, spelled once so a replay
// writes the same folder name and the manifest stays deterministic. It is a
// well-formed 16-hex session id of this fixture's own.
const returningSessionID = "f1x7ure000000001"

// seedReturning writes the state a user of one week leaves behind: a project
// folder with a file in it, and one past conversation recorded through the
// product's own writer, session.SaveMeta, at the fixture's fixed epoch. NO
// database is opened, copied or edited: the memory store, the spending ledger
// and the FTS index are not seeded (the header records the limits).
func seedReturning(home string) error {
	project := filepath.Join(home, returningProjectName)
	if err := os.MkdirAll(project, 0o700); err != nil {
		return fmt.Errorf("make the project folder: %w", err)
	}
	if err := os.WriteFile(filepath.Join(project, "notes.md"),
		[]byte("# ledger\n\nA project a returning user was last working in.\n"), 0o600); err != nil {
		return fmt.Errorf("write the project file: %w", err)
	}

	// The conversation bucket: HOME/.codeaf/v3/projects/<encoded workspace>.
	// The encoding is one-way and spelled the way every writer of it spells
	// it: separators (and colons) become dashes, with a leading dash.
	encoded := "-" + strings.ReplaceAll(strings.ReplaceAll(filepath.Clean(project), string(filepath.Separator), "-"), ":", "-")
	dir := filepath.Join(home, ".codeaf", "v3", "projects", encoded, returningSessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("make the conversation folder: %w", err)
	}

	// The identity, through the engine's own writer, stamped at the fixed
	// epoch so a replay writes the same logical history.
	lastUser := fixtureEpoch.Add(-26 * time.Hour)
	if err := session.SaveMeta(dir, session.Meta{
		ID:         returningSessionID,
		Title:      "What the Ledger Means",
		Workspace:  project,
		LaunchDir:  project,
		Model:      "openai/gpt-5-mini",
		Created:    lastUser.Add(-12 * time.Minute),
		LastUserAt: lastUser,
		SpentUSD:   0.19,
		Tokens:     15_400,
	}); err != nil {
		return fmt.Errorf("write the conversation identity: %w", err)
	}
	return nil
}

// returningBucket is the conversation record of the returning scenario's
// project, under the fixture HOME — the path the tests read back through.
func returningBucket(home string) string {
	project := filepath.Join(home, returningProjectName)
	encoded := "-" + strings.ReplaceAll(strings.ReplaceAll(filepath.Clean(project), string(filepath.Separator), "-"), ":", "-")
	return filepath.Join(home, ".codeaf", "v3", "projects", encoded, returningSessionID)
}

// buildManifest walks the fixture directory and records the deterministic
// tree. The hash covers the schema, the fixture version, the scenario and
// every relative path with its kind and mode — never file contents, which
// carry the product writers' own timestamps.
func buildManifest(dir, name string) (*FixtureManifest, error) {
	m := &FixtureManifest{Schema: fixtureSchema, FixtureVersion: fixtureVersion, Scenario: name}
	home := filepath.Join(dir, "home")
	err := filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(home, path)
		if err != nil {
			return err
		}
		rel = canonicalPath(home, rel)
		if d.IsDir() {
			m.Entries = append(m.Entries, fixtureEntry{Path: rel, Kind: "dir", Mode: 0o700})
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("fixture: %s is not a regular file: fixtures hold no links or devices", rel)
		}
		m.Entries = append(m.Entries, fixtureEntry{Path: rel, Kind: "file", Mode: 0o600})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("fixture: walk the prepared tree: %w", err)
	}
	m.Hash = m.hash()
	return m, nil
}

// canonicalPath makes a path portable across fixture roots. A workspace's
// conversation bucket embeds the workspace's ABSOLUTE path — the home path,
// separators dashed — so a bucket name would otherwise differ from one probe
// root to the next and the manifest would not be comparable. The encoded home
// path is therefore replaced by a stable placeholder in recorded entries, and
// verifyTree canonicalizes actual paths the same way before comparing.
func canonicalPath(home, rel string) string {
	encoded := "-" + strings.ReplaceAll(strings.ReplaceAll(filepath.Clean(home), string(filepath.Separator), "-"), ":", "-")
	return strings.ReplaceAll(rel, encoded, "-HOME-")
}

// hash is the manifest's content address: sha256 over the schema line, the
// version line, the scenario line, and one line per entry.
func (m *FixtureManifest) hash() string {
	entries := make([]fixtureEntry, len(m.Entries))
	copy(entries, m.Entries)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	h := sha256.New()
	fmt.Fprintf(h, "schema %d\n", m.Schema)
	fmt.Fprintf(h, "version %s\n", m.FixtureVersion)
	fmt.Fprintf(h, "scenario %s\n", m.Scenario)
	for _, e := range entries {
		fmt.Fprintf(h, "%s\t%s\t%04o\n", e.Path, e.Kind, e.Mode)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// seeded answers whether the scenario this manifest was built from seeds
// anything beyond the empty home.
func (m *FixtureManifest) seeded() bool { return m.Scenario != "clean" }

// manifestPath is where the manifest lives inside one fixture directory.
func manifestPath(dir string) string { return filepath.Join(dir, "manifest.json") }

// writeManifest records the manifest as a private file, entries sorted so the
// file on disk is the shape the hash was taken over.
func writeManifest(dir string, m *FixtureManifest) error {
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Path < m.Entries[j].Path })
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("fixture: encode the manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath(dir), raw, 0o600); err != nil {
		return fmt.Errorf("fixture: write the manifest: %w", err)
	}
	return nil
}

// readManifest reads a fixture's recorded manifest, if one is there.
func readManifest(dir string) (*FixtureManifest, error) {
	raw, err := os.ReadFile(manifestPath(dir))
	if err != nil {
		return nil, err
	}
	var m FixtureManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("fixture: read the manifest: %w", err)
	}
	return &m, nil
}

// verifyTree compares what IS on the disk under the fixture's home against
// what the manifest says should be there. Deterministic verification: a walk
// of the actual tree against the recorded entries, both sides complete, no
// sampling. It names the first difference it finds.
func verifyTree(dir string, m *FixtureManifest) error {
	home := filepath.Join(dir, "home")

	want := make(map[string]fixtureEntry, len(m.Entries))
	for _, e := range m.Entries {
		want[e.Path] = e
	}

	got := map[string]fixtureEntry{}
	err := filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(home, path)
		if err != nil {
			return err
		}
		rel = canonicalPath(home, rel)
		kind := "file"
		if d.IsDir() {
			kind = "dir"
		} else if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", rel)
		}
		got[rel] = fixtureEntry{Path: rel, Kind: kind}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk the tree: %w", err)
	}

	paths := make([]string, 0, len(want)+len(got))
	seen := map[string]bool{}
	for p := range want {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	for p := range got {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		w, inWant := want[p]
		g, inGot := got[p]
		switch {
		case inWant && !inGot:
			return fmt.Errorf("%s is recorded in the manifest but missing from the tree", p)
		case !inWant && inGot:
			return fmt.Errorf("%s is in the tree but not in the manifest", p)
		case w.Kind != g.Kind:
			return fmt.Errorf("%s is a %s, the manifest records a %s", p, g.Kind, w.Kind)
		}
	}
	return nil
}
