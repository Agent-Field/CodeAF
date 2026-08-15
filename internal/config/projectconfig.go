package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The project-local settings layer: <workspace>/.openaf/config.json.
//
// It is omp's <repo>/.omp/config.yml in aforge's file format, and it answers the
// question the profile cannot: which settings belong to the REPOSITORY rather
// than to the person. A team's tool approvals, the cheap model a repo's small
// calls ride, the ceiling one sitting in this codebase may spend — those are
// facts about the work, and they should travel with the work rather than being
// re-typed into every machine that checks it out.
//
// Three laws, and they are the whole of it.
//
//  1. RESOLUTION IS PROJECT → PROFILE → BUILT-IN DEFAULT, which is exactly omp's
//     layering and exactly the order docs/CHAT-V3.md Decision 6 states. The
//     environment still outranks all three where a row has a pin: a variable is
//     the operator's explicit instruction for this process, and a file checked
//     into a repository must not be able to overrule the shell the binary was
//     launched from. (Five of the eight rows here have no pin at all — see the
//     v3 section of settings.go for why a tool gate the environment can widen is
//     a gate with a bypass.)
//
//  2. NOTHING DEEP-MERGES. A scalar replaces the layer below it; a map replaces
//     the layer below it WHOLESALE. This is omp's merge law and it is worth the
//     sentence: a project file that said `tools.approval: {write: deny}` while
//     the profile said `read: allow` resolves to write:deny and NOTHING ELSE.
//     Half a rule set assembled from two files is a rule set nobody wrote and
//     nobody can read back — the person who opened the project file would be
//     looking at a complete answer that is not the one in force.
//
//  3. A MISSING FILE IS AN EMPTY LAYER AND NEVER AN ERROR. A file that is there
//     and cannot be read is a LOUD error naming the path. The two halves are one
//     rule: most repositories have no project settings and must cost nothing,
//     and a settings file that silently does not apply is how a rule somebody
//     wrote down gets skipped. That is the failure this layer exists to make
//     impossible, so it is the one thing it refuses to be quiet about.
//
// KEYS ARE DOTTED, not nested: `"tools.approvalMode": "allow"`, the same flat
// spelling the registry already uses (KeyToolApprovalMode) and the same one the
// profile's own config.json stores. One spelling means one lookup and one thing
// to learn — a nested form would need a second reader, a second writer, and a
// rule about which wins when a file contains both. A nested file is NOT silently
// ignored, though: [LoadProjectConfig] flattens what it finds and refuses when a
// flattened path names a real row, because law 3 is the point of the layer.
//
// The file is read at <cwd> and nowhere above it. There is no walk up to a git
// root: a settings file that applied from a directory the person never opened is
// a rule arriving from off-screen, and the session's workspace IS the directory
// they opened.

const (
	// ProjectConfigDir is the per-repository settings directory.
	ProjectConfigDir = ".openaf"
	// ProjectConfigFile is the one file inside it this layer reads.
	ProjectConfigFile = "config.json"
)

// ProjectKeys is the allowlist: the rows that may be answered by a repository.
//
// It is an allowlist rather than "whatever the registry has" for two reasons.
// A checked-in file must not be able to move somebody's daily budget or point
// their vision model at a model they pay for — those are the PERSON's rows.
// And an unrecognised key here is not an error (a later version's row, another
// tool's section), so without a list of what this build honors there would be
// no way to tell a row that does not apply yet from a row that never will.
var ProjectKeys = []string{
	KeyToolApprovalMode,
	KeyToolApprovals,
	KeyTierLowModel,
	KeyTierHighModel,
	KeyModelRoles,
	KeySpendRail,
	KeyHistoryEnabled,
	KeyDraftPersist,
}

// ProjectKeyAllowed reports whether a row may live in a project file.
func ProjectKeyAllowed(key string) bool {
	for _, allowed := range ProjectKeys {
		if allowed == key {
			return true
		}
	}
	return false
}

// ProjectConfigPath is the file this layer reads for one workspace. An empty
// workspace has no path and therefore no layer.
func ProjectConfigPath(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	return filepath.Join(cwd, ProjectConfigDir, ProjectConfigFile)
}

// ProjectConfig is one loaded project layer. The zero value is the empty layer,
// which is what a directory with no settings has and what every read falls
// through.
type ProjectConfig struct {
	path   string
	values map[string]json.RawMessage
}

// LoadProjectConfig reads <cwd>/.openaf/config.json.
//
// Absent is empty; unreadable, unparseable, or written in the nested shape is an
// error naming the path (law 3). Keys this build does not know are kept and
// ignored, the way the profile writer preserves unrelated keys: a file is
// allowed to be from a later version, or to carry another tool's section.
func LoadProjectConfig(cwd string) (ProjectConfig, error) {
	path := ProjectConfigPath(cwd)
	if path == "" {
		return ProjectConfig{}, nil
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ProjectConfig{path: path}, nil
	}
	if err != nil {
		return ProjectConfig{}, fmt.Errorf("read project settings %s: %w", path, err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return ProjectConfig{}, fmt.Errorf("read project settings %s: %w", path, err)
	}
	if values == nil {
		// A literal `null` parses without complaint and means the same as an
		// empty object here: a file that says nothing.
		values = map[string]json.RawMessage{}
	}
	if err := refuseNestedRows(path, values); err != nil {
		return ProjectConfig{}, err
	}
	return ProjectConfig{path: path, values: values}, nil
}

// Path is the file this layer came from, empty when there is no workspace.
func (p ProjectConfig) Path() string { return p.path }

// Has reports whether the project file answers this row at all.
func (p ProjectConfig) Has(key string) bool {
	_, found := p.values[key]
	return found
}

// String reads one text row.
//
// The two MAP rows may be written either as the flat `read:allow, bash:prompt`
// text the settings sheet stores or as a JSON object — the same map, and the
// object is what a person hand-writing a settings file reaches for first.
// Whichever shape it arrives in it REPLACES the profile's answer entirely
// (law 2); the object is flattened to the flat text so exactly one parser
// (ParseToolApprovals, ParseModelRoles) reads it downstream.
func (p ProjectConfig) String(key string) (string, bool, error) {
	encoded, found := p.values[key]
	if !found {
		return "", false, nil
	}
	var text string
	if err := json.Unmarshal(encoded, &text); err == nil {
		return strings.TrimSpace(text), true, nil
	}
	if key == KeyToolApprovals || key == KeyModelRoles {
		var pairs map[string]string
		if err := json.Unmarshal(encoded, &pairs); err == nil {
			flat, err := p.flattenPairs(key, pairs)
			if err != nil {
				return "", false, err
			}
			return flat, true, nil
		}
		return "", false, p.wants(key, encoded, "text, or an object of name to value")
	}
	return "", false, p.wants(key, encoded, "text")
}

// Bool reads one on/off row. The words the sheet shows — on, off, yes, no — are
// accepted beside true and false, because a person writing this file by hand has
// only ever seen the words.
func (p ProjectConfig) Bool(key string) (bool, bool, error) {
	encoded, found := p.values[key]
	if !found {
		return false, false, nil
	}
	var value bool
	if err := json.Unmarshal(encoded, &value); err == nil {
		return value, true, nil
	}
	var text string
	if err := json.Unmarshal(encoded, &text); err == nil {
		if value, err := parseBool(text); err == nil {
			return value, true, nil
		}
	}
	return false, false, p.wants(key, encoded, "true or false")
}

// Float reads one dollar row. A quoted amount is accepted for the reason the
// words are above; a negative or non-finite one is refused with the row named.
func (p ProjectConfig) Float(key string) (float64, bool, error) {
	encoded, found := p.values[key]
	if !found {
		return 0, false, nil
	}
	var value float64
	if err := json.Unmarshal(encoded, &value); err != nil {
		var text string
		if err := json.Unmarshal(encoded, &text); err != nil {
			return 0, false, p.wants(key, encoded, "a dollar amount")
		}
		parsed, err := parseDollars(text)
		if err != nil {
			return 0, false, p.wants(key, encoded, "a dollar amount")
		}
		value = parsed
	}
	if _, err := validateDailyBudget(value, p.where(key)); err != nil {
		return 0, false, err
	}
	return value, true, nil
}

// ResolveString answers one text row through the whole ladder: the project file,
// then the profile reader that already owns this row, then that reader's
// built-in default.
//
// AN EMPTY PROJECT VALUE IS AN ANSWER AND NOT AN ABSENCE. `"models.tiers.low":
// ""` is how a repository says "ignore whatever this machine has pinned and let
// the small calls follow the conversation" — the alternative, treating empty as
// unset, would leave a repository unable to turn a personal setting OFF.
func (p ProjectConfig) ResolveString(profileDir, key string) (string, error) {
	if !ProjectKeyAllowed(key) {
		return "", fmt.Errorf("%q is not a project-local settings row", key)
	}
	value, found, err := p.String(key)
	if err != nil {
		return "", err
	}
	if found {
		if key == KeyToolApprovalMode {
			// The gate's own row is validated here rather than forgiven, unlike
			// the profile's. A garbled personal setting reads as the strictest
			// answer and the person is in front of the sheet; a garbled row in a
			// checked-in file is a rule a team believes is in force, and the
			// only honest thing to do with it is say so.
			mode := strings.ToLower(value)
			if !knownToolApprovalMode(mode) {
				return "", fmt.Errorf("%s: %s is %q, want one of: %s",
					p.path, key, value, strings.Join(ToolApprovalModes, ", "))
			}
			return mode, nil
		}
		return value, nil
	}
	switch key {
	case KeyToolApprovalMode:
		return ToolApprovalModeAt(profileDir), nil
	case KeyToolApprovals:
		return ToolApprovalsAt(profileDir), nil
	case KeyTierLowModel:
		return TierModelAt(profileDir, ModelTierLow), nil
	case KeyTierHighModel:
		return TierModelAt(profileDir, ModelTierHigh), nil
	case KeyModelRoles:
		return ModelRolesAt(profileDir), nil
	}
	return "", fmt.Errorf("%q is not a text row", key)
}

// ResolveBool answers one on/off row through the ladder.
//
// The environment is checked FIRST and outranks the project file, which is the
// only place in this file the order is not simply project-over-profile: these
// two rows are pinned (AFORGE_HISTORY, AFORGE_DRAFT_PERSIST), and a pin is the
// operator speaking about this process. An unreadable pin is not a choice at
// all — it falls through to the project file, and then to the profile reader,
// which lands on the default exactly as it always did.
func (p ProjectConfig) ResolveBool(profileDir, key string) (bool, error) {
	name := ""
	switch key {
	case KeyHistoryEnabled:
		name = "AFORGE_HISTORY"
	case KeyDraftPersist:
		name = "AFORGE_DRAFT_PERSIST"
	default:
		if !ProjectKeyAllowed(key) {
			return false, fmt.Errorf("%q is not a project-local settings row", key)
		}
		return false, fmt.Errorf("%q is not an on/off row", key)
	}
	if value, pinned := environmentBool(name); pinned {
		return value, nil
	}
	value, found, err := p.Bool(key)
	if err != nil {
		return false, err
	}
	if found {
		return value, nil
	}
	if key == KeyHistoryEnabled {
		return HistoryEnabledAt(profileDir), nil
	}
	return DraftPersistAt(profileDir), nil
}

// ResolveFloat answers one dollar row through the ladder. Today that is the
// session ceiling, which has no environment pin by design.
func (p ProjectConfig) ResolveFloat(profileDir, key string) (float64, error) {
	if key != KeySpendRail {
		if !ProjectKeyAllowed(key) {
			return 0, fmt.Errorf("%q is not a project-local settings row", key)
		}
		return 0, fmt.Errorf("%q is not a dollar row", key)
	}
	value, found, err := p.Float(key)
	if err != nil {
		return 0, err
	}
	if found {
		return value, nil
	}
	return SpendRailUSDAt(profileDir), nil
}

// ProjectStringAt resolves one text row for a workspace: the project file's
// answer when it has one, otherwise the profile reader that already owns the
// row, otherwise that reader's default. A missing project file is silent; a
// broken one stops the caller with the path in the message.
func ProjectStringAt(cwd, profileDir, key string) (string, error) {
	project, err := LoadProjectConfig(cwd)
	if err != nil {
		return "", err
	}
	return project.ResolveString(profileDir, key)
}

// ProjectBoolAt is [ProjectStringAt] for the two on/off rows.
func ProjectBoolAt(cwd, profileDir, key string) (bool, error) {
	project, err := LoadProjectConfig(cwd)
	if err != nil {
		return false, err
	}
	return project.ResolveBool(profileDir, key)
}

// ProjectFloatAt is [ProjectStringAt] for the dollar row.
func ProjectFloatAt(cwd, profileDir, key string) (float64, error) {
	project, err := LoadProjectConfig(cwd)
	if err != nil {
		return 0, err
	}
	return project.ResolveFloat(profileDir, key)
}

// environmentBool reads a pin that is set AND readable. Anything else is not a
// choice, and the layer below gets to answer.
func environmentBool(name string) (bool, bool) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false, false
	}
	value, err := parseBool(raw)
	if err != nil {
		return false, false
	}
	return value, true
}

// refuseNestedRows is law 3 applied to the shape of the file. A person who wrote
// `{"tools": {"approvalMode": "allow"}}` wrote a real setting in the wrong
// spelling, and the one unacceptable answer is to load the file, ignore the
// line, and start anyway.
//
// It only ever refuses a nested path that names a row this build honors.
// Unknown nested sections are somebody else's business and stay untouched.
func refuseNestedRows(path string, values map[string]json.RawMessage) error {
	for _, key := range sortedKeys(values) {
		if strings.Contains(key, ".") || ProjectKeyAllowed(key) {
			// A dotted key is already the right spelling, and an allowlisted
			// key may legitimately hold an object: tools.approval is a map.
			continue
		}
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(values[key], &nested); err != nil {
			continue
		}
		for _, leaf := range flattenPaths(key, nested) {
			if ProjectKeyAllowed(leaf) {
				return fmt.Errorf("%s: write %q as one dotted key at the top level, not nested under %q",
					path, leaf, key)
			}
		}
	}
	return nil
}

// flattenPaths lists every dotted path reachable under one nested object.
func flattenPaths(prefix string, values map[string]json.RawMessage) []string {
	paths := make([]string, 0, len(values))
	for _, key := range sortedKeys(values) {
		path := prefix + "." + key
		paths = append(paths, path)
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(values[key], &nested); err == nil && nested != nil {
			paths = append(paths, flattenPaths(path, nested)...)
		}
	}
	return paths
}

// flattenPairs writes an object row as the flat text the one parser reads. The
// separators are refused rather than escaped: a name or a value carrying a comma
// would come back out of that parser as two rules, and a rule nobody wrote is
// exactly what this layer refuses to produce.
func (p ProjectConfig) flattenPairs(key string, pairs map[string]string) (string, error) {
	names := make([]string, 0, len(pairs))
	for name := range pairs {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]string, 0, len(names))
	for _, name := range names {
		value := strings.TrimSpace(pairs[name])
		name = strings.TrimSpace(name)
		if name == "" || value == "" {
			return "", fmt.Errorf("%s: %s has an empty name or value", p.path, key)
		}
		if strings.ContainsAny(name, ",;:\n") || strings.ContainsAny(value, ",;\n") {
			return "", fmt.Errorf("%s: %s cannot contain a comma, a semicolon or a newline — %q: %q",
				p.path, key, name, value)
		}
		entries = append(entries, name+":"+value)
	}
	return strings.Join(entries, ", "), nil
}

// wants is the one shape of type complaint this layer makes: which file, which
// row, what arrived, what was expected.
func (p ProjectConfig) wants(key string, encoded json.RawMessage, want string) error {
	return fmt.Errorf("%s: %s wants %s, got %s", p.path, key, want, strings.TrimSpace(string(encoded)))
}

func (p ProjectConfig) where(key string) string {
	return p.path + " (" + key + ")"
}

func sortedKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
