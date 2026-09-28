// Package skills discovers agent skills where foreign harnesses keep them.
//
// A skill — as agentskills.io spells it, and as Claude Code, Codex, Cursor and
// Gemini all read it — is a directory holding a SKILL.md whose frontmatter
// names it and describes it. The harnesses that install such folders do so in
// a handful of conventional places, and a person who has already collected
// skills there should not have to copy or reinstall them for codeaf to offer
// them: discovery reads them IN PLACE and reports the original directory, so
// the caller can register the folder itself as the artifact.
//
// The package is pure on purpose. It reads directories the foreign harnesses
// own and writes nothing; it takes the project and home directories as
// arguments rather than resolving them; and it knows nothing about the store —
// what a discovery becomes is the resident's decision. That is what keeps the
// scan testable against fixture trees, and keeps the fact shape another
// surface consumes out of the scan's business.
package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Scope values. A skill found under the project directory belongs to that
// project; a skill found under the home directory belongs to the machine.
const (
	ScopeProject = "project"
	ScopeUser    = "user"
)

// Limits from the agentskills.io field spec. They are checked as runes, not
// bytes: the fields are prose a person reads, and a multibyte character is
// one character to the person who wrote it.
const (
	maxNameRunes  = 64
	maxDescRunes  = 1024
	nameRunesBase = "abcdefghijklmnopqrstuvwxyz0123456789-"
)

// skillRoots is issue #1277's day-one list, in the order that decides who wins.
// The first folder that holds a name owns it within its scope, and any project
// root shadows every user root — codeaf's own folder is deliberately first, so
// a skill a person keeps in .codeaf/skills outranks the same name in any
// foreign harness's folder. A root that does not exist is skipped without a
// word, and a skill is always a DIRECT child directory holding a SKILL.md —
// nothing is walked deeper, which is also why the resident's promoted command
// folders on the .codeaf/skills shelf stay invisible to this scan: they have
// no SKILL.md. A direct child that is a LINK to a directory counts as one:
// installers that keep one copy of a skill and link it into every harness's
// folder are the common way a skill reaches several harnesses at once, and a
// scan that skipped links saw none of those.
//
// TWO MORE SOURCES FOLLOW THESE SIX WITHIN EACH SCOPE, and they come last on
// purpose (see [Discover]): the skills that arrived inside an installed and
// enabled Claude Code plugin ([RootClaudePlugins], plugins.go), and the skills
// Codex ships with itself ([RootCodexSystem]). Nobody placed either of them
// by hand, so any skill a person did place by hand, in any of the six folders,
// owns the name over them.
var skillRoots = []string{
	".codeaf/skills",
	".agents/skills",
	".claude/skills",
	".codex/skills",
	".cursor/skills",
	".gemini/skills",
}

// RootCodexSystem is the folder Codex installs its own bundled skills into.
// It sits INSIDE .codex/skills, where the six-root scan sees it as one child
// with no SKILL.md and passes over it, so it is read as a root of its own.
//
// IT RANKS LAST IN ITS SCOPE, below the plugin skills too. A system skill is
// the harness's default and nobody chose it: a person who installed a skill of
// the same name into .codex/skills or anywhere else meant theirs.
const RootCodexSystem = ".codex/skills/.system"

// Options names where to look: the project's own directory and the login home
// directory, not any skills folder under them.
type Options struct {
	ProjectDir string
	HomeDir    string
}

// Skill is one discovered skill. The shape is FROZEN — the resident registers
// facts from it and other surfaces read those facts, so a field may be added
// but not renamed, reshaped or dropped.
type Skill struct {
	// Name is the frontmatter name, as written.
	Name string
	// Description is the frontmatter description, one line.
	Description string
	// Dir is the absolute path of the original skill folder. The skill is
	// never copied; this is where it lives.
	Dir string
	// Scope is ScopeProject or ScopeUser.
	Scope string
	// Root is the skills folder the skill was read from, e.g. ".claude/skills".
	Root string
	// SizeBytes is the total size of the regular files under Dir.
	SizeBytes int64
	// Warning says what is wrong with a skill that still loaded — a name that
	// does not match its folder, or one that breaks the field rules. A skill
	// that was skipped rather than loaded carries the reason here too.
	Warning string
	// Shadowed is true when another folder owns this skill's name: a project
	// skill over a user one, or an earlier root over a later one within a
	// scope. A shadowed skill stays in the result rather than being silently
	// dropped, because "why is my skill not working" deserves an answer.
	Shadowed bool
	// Plugin names the Claude Code plugin a skill arrived inside, spelled the
	// way Claude Code keys it (`name@marketplace`), and is empty for a skill
	// read from a skills folder. Root is [RootClaudePlugins] whenever this is
	// set.
	Plugin string
}

// Discover scans the conventional skill folders under one project directory
// and one home directory, in issue #1277's order, and returns what it found:
// within each scope the six hand-kept folders, then the skills of every
// installed and enabled Claude Code plugin, then Codex's bundled skills —
// every folder that holds a SKILL.md, winners first, losers marked Shadowed,
// and unreadable ones carried with a Warning rather than dropped. It never
// fails because one folder is broken — the worst a malformed skill can do is
// appear with a Warning — and it errors only when the caller named nowhere to
// look at all.
func Discover(opts Options) ([]Skill, error) {
	projectDir := strings.TrimSpace(opts.ProjectDir)
	homeDir := strings.TrimSpace(opts.HomeDir)
	if projectDir == "" && homeDir == "" {
		return nil, fmt.Errorf("discover skills: neither a project nor a home directory was given")
	}
	type scanBase struct {
		dir   string
		scope string
	}
	bases := make([]scanBase, 0, 2)
	if projectDir != "" {
		bases = append(bases, scanBase{dir: absolute(projectDir), scope: ScopeProject})
	}
	// The project bases come first, so collection order is precedence order:
	// anything found under the project shadows the same name under the home,
	// and within a scope the earlier root in skillRoots wins.
	//
	// The two bases are deduplicated, because codeaf opened in the home
	// directory itself would otherwise report every user skill twice — once as
	// a project skill and once as its own shadow. The winner is the same
	// either way, so the duplicate is pure noise. Keeping scope beside its
	// base also matters to callers that scan only HomeDir: it remains user
	// scope rather than becoming project scope merely by being first.
	if homeDir != "" && absolute(homeDir) != absolute(projectDir) {
		bases = append(bases, scanBase{dir: absolute(homeDir), scope: ScopeUser})
	}

	// The plugin registry is read once for both scopes: it lives under the
	// home directory whichever scope a plugin was installed for.
	userPlugins, projectPlugins := claudePlugins(homeDir, projectDir)
	homeFolded := homeDir != "" && projectDir != "" && absolute(homeDir) == absolute(projectDir)

	result := make([]Skill, 0)
	owner := make(map[string]int)
	// take reads one skill folder into the result, settling its name against
	// every skill collected before it.
	take := func(dir, root, scope, plugin string) {
		skill, state := readSkill(dir, root, scope)
		skill.Plugin = plugin
		switch state {
		case stateNotASkill:
		case stateSkipped:
			result = append(result, skill)
		case stateLoaded:
			if _, seen := owner[skill.Name]; seen {
				skill.Shadowed = true
				result = append(result, skill)
				return
			}
			owner[skill.Name] = len(result)
			result = append(result, skill)
		}
	}
	// collect reads every skill folder directly inside one folder.
	collect := func(folder, root, scope, plugin string) {
		entries, err := os.ReadDir(folder)
		if err != nil {
			// A missing folder is skipped without a word.
			return
		}
		for _, entry := range entries {
			if isDirectory(folder, entry) {
				take(filepath.Join(folder, entry.Name()), root, scope, plugin)
			}
		}
	}
	for _, base := range bases {
		scope := base.scope
		for _, root := range skillRoots {
			collect(filepath.Join(base.dir, root), root, scope, "")
		}
		// THE PLUGIN SKILLS, after every hand-kept folder in the scope and
		// before the harness's own bundled skills. A project scope carries
		// the plugins installed for this project; when the home directory
		// folded into the project base above, it carries the person's own
		// plugins after them too, the same way it already carries their six
		// home folders.
		plugins := userPlugins
		if scope == ScopeProject {
			plugins = projectPlugins
			if homeFolded {
				plugins = append(append([]claudePlugin(nil), projectPlugins...), userPlugins...)
			}
		}
		for _, plugin := range plugins {
			for _, folder := range plugin.skillFolders {
				// A folder a plugin names may be one skill rather than a
				// folder of them, and then it is read as the one skill.
				if _, err := os.Stat(filepath.Join(folder, "SKILL.md")); err == nil {
					take(folder, RootClaudePlugins, scope, plugin.id)
					continue
				}
				collect(folder, RootClaudePlugins, scope, plugin.id)
			}
		}
		collect(filepath.Join(base.dir, RootCodexSystem), RootCodexSystem, scope, "")
	}
	return result, nil
}

// isDirectory reports whether one child of a skills folder is a directory,
// following a link to find out. A link that points nowhere, or at a file, is
// not a skill folder and is passed over the way a stray file is.
func isDirectory(parent string, entry fs.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&fs.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(parent, entry.Name()))
	return err == nil && info.IsDir()
}

// absolute is filepath.Abs with the failure swallowed: a discovery handed a
// relative path deserves the same absolute answer in the common case, and a
// working directory that cannot be read is no reason to refuse the whole scan.
func absolute(path string) string {
	if resolved, err := filepath.Abs(path); err == nil {
		return resolved
	}
	return path
}

// The three verdicts one child folder can receive.
type skillState int

const (
	// stateNotASkill: no SKILL.md, so not a skill and not a complaint either —
	// the resident's own promoted command folders live on the shelf without
	// one, on purpose.
	stateNotASkill skillState = iota
	// stateSkipped: a SKILL.md that does not parse into a skill. The folder is
	// reported with the reason in Warning and nothing more is claimed for it.
	stateSkipped
	// stateLoaded: a skill. It may still carry a Warning — a name that breaks
	// the field rules or does not match its folder loads with a warning, the
	// way the spec's client guide recommends, rather than being dropped.
	stateLoaded
)

// MaxSkillFileBytes bounds the text read from one SKILL.md on every door.
const MaxSkillFileBytes = 64 * 1024

func readSkillFile(path string) ([]byte, error) {
	return ReadRegularHead(path, MaxSkillFileBytes)
}

// readSkill reads one direct child directory of a skills root.
func readSkill(dir, root, scope string) (Skill, skillState) {
	skill := Skill{Dir: dir, Scope: scope, Root: root}
	data, err := readSkillFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		if errors.Is(err, ErrNotRegular) {
			skill.Warning = "SKILL.md is not a regular file"
			return skill, stateSkipped
		}
		return Skill{}, stateNotASkill
	}
	name, description, warning := parseSkillMarkdown(string(data))
	// The name and its folder are compared HERE rather than in the parser
	// because the parser has no folder to compare against — and the mismatch
	// is a warning, not a refusal: the spec's client guide loads such a skill
	// and lets the person see what is odd about it.
	if name != "" && name != filepath.Base(dir) {
		if warning == "" {
			warning = "name does not match folder " + filepath.Base(dir)
		} else {
			warning += "; name does not match folder " + filepath.Base(dir)
		}
	}
	skill.Name = name
	skill.Description = description
	skill.SizeBytes = directorySize(dir)
	if warning != "" {
		skill.Warning = warning
	}
	if name == "" || description == "" {
		return skill, stateSkipped
	}
	return skill, stateLoaded
}

// parseSkillMarkdown reads the frontmatter of one SKILL.md leniently, the way
// the agentskills.io client guide recommends. Two kinds of defect, two
// dispositions: a skill whose name breaks the field rules or does not match
// its folder still LOADS, with the defect in the returned warning; a skill
// with no description, no name, or frontmatter that will not parse is SKIPPED,
// and the returned warning says why.
func parseSkillMarkdown(text string) (name, description, warning string) {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", "", "SKILL.md does not start with a --- frontmatter block"
	}
	rest := normalized[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return "", "", "the frontmatter block is never closed with a --- line"
	}
	var parsed struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(repairColonDescription(rest[:end])), &parsed); err != nil {
		return "", "", "the frontmatter is not valid YAML: " + strings.TrimSpace(err.Error())
	}

	name = strings.TrimSpace(parsed.Name)
	description = oneLine(strings.TrimSpace(parsed.Description))
	switch {
	case name == "":
		return "", description, "SKILL.md has no name"
	case utf8.RuneCountInString(name) > maxNameRunes || !validSkillName(name):
		warning = fmt.Sprintf("name %q is not 1-%d chars of lowercase letters, digits and hyphens", name, maxNameRunes)
	}
	switch {
	case description == "":
		return name, "", "SKILL.md has no description"
	case utf8.RuneCountInString(description) > maxDescRunes:
		return name, "", fmt.Sprintf("description is %d chars (limit %d)", utf8.RuneCountInString(description), maxDescRunes)
	}
	return name, description, warning
}

// repairColonDescription fixes the one common malformation before the YAML
// parse: an unquoted description whose value contains a colon. YAML refuses a
// plain scalar with a ": " in it, so a person writing
//
//	description: Redact PDFs: forms, headers and footers
//
// has written a skill no harness can read. Quoting the value — with the
// quotes and backslashes inside it escaped — is the whole repair, and a value
// that was already quoted is left exactly as its author wrote it.
func repairColonDescription(frontmatter string) string {
	const key = "description:"
	lines := strings.Split(frontmatter, "\n")
	for index, line := range lines {
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		trimmed := line[indent:]
		if !strings.HasPrefix(trimmed, key) {
			continue
		}
		value := strings.TrimPrefix(trimmed, key)
		value = strings.TrimSpace(value)
		if value == "" || value[0] == '"' || value[0] == '\'' || !strings.Contains(value, ":") {
			continue
		}
		quoted := strings.ReplaceAll(value, `\`, `\\`)
		quoted = strings.ReplaceAll(quoted, `"`, `\"`)
		lines[index] = line[:indent] + key + ` "` + quoted + `"`
	}
	return strings.Join(lines, "\n")
}

// oneLine collapses the newlines a YAML block scalar can produce: the fact
// shelf requires a skill's doc to be one line, and a folded multi-line
// description becomes its one-line form rather than being refused.
func oneLine(text string) string {
	if !strings.ContainsAny(text, "\r\n") {
		return text
	}
	return strings.Join(strings.Fields(text), " ")
}

func validSkillName(name string) bool {
	for _, char := range name {
		if !strings.ContainsRune(nameRunesBase, char) {
			return false
		}
	}
	return name != ""
}

// directorySize sums the regular files under dir, best effort. It is the
// catalog tier's cost signal — what a skill weighs, not what it does — so a
// file that cannot be read contributes nothing and stops nothing.
func directorySize(dir string) int64 {
	var total int64
	// A skill folder reached through a link is walked at the folder it names:
	// WalkDir does not descend through a link at its root, and a linked skill
	// would otherwise weigh nothing at all.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}
