// Package delegate is the half of a delegate that everything else in the
// binary needs: what one IS (a manifest beside its manual page), how the
// installed ones are found, the stdout protocol every delegate speaks and the
// one reader over it, and the launch of the program as a child process that
// streams, stops on SIGTERM and ends with one terminal record.
//
// A DELEGATE IS AN OUTSIDE PROGRAM CODEAF HANDS A WHOLE TASK TO. codeaf designs
// nothing about it and cannot see inside it; it starts it in a working copy,
// reads its stdout, stops it when a limit is reached and takes its result. The
// contract is docs/DELEGATE-PROTOCOL.md, and this package is its
// implementation. What runs a delegate AS A WORKER of a run — the live step,
// the trajectory, the spend bank — is internal/run's, which builds on this
// package; nothing here knows what a task is.
//
// THIS PACKAGE IS A LEAF ON PURPOSE. The session door lists delegates and
// checks a name; the run engine seats one; neither may import the other, so
// what they share lives here and imports neither.
package delegate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The two things a delegate can leave behind, named on its manifest.
const (
	// LandsTree is a delegate that works in the working copy it is given and
	// leaves its changes there: codeaf squashes them into one commit and merges
	// that home the way every task lands.
	LandsTree = "tree"
	// LandsText is a delegate that changes nothing in the copy and puts its
	// answer in the terminal record's deliverable: codeaf folds the text into
	// the conversation the way a quick task's answer arrives.
	LandsText = "text"
)

// The placeholders a manifest's argv and env may carry, filled at launch. They
// are spelled here once so the loader can refuse one this build does not know
// rather than hand a program a literal `{{typo}}`.
const (
	FillBrief     = "{{brief}}"
	FillWorkspace = "{{workspace}}"
	FillCostUSD   = "{{cost_usd}}"
	FillHours     = "{{hours}}"
	// FillKey is the person's API key, resolved by the caller through the same
	// door every lane resolves one (config.APIKeyAt). `{{key:openrouter}}` is
	// accepted as the same thing, because that is how the protocol page spells
	// it and a manifest copied from there must load.
	FillKey = "{{key}}"
)

// Manifest is one delegate as its manifest file states it. Every field a
// person writes is here; nothing is inferred from the binary.
type Manifest struct {
	// Name is one lowercase word: the file's name, the command a person types
	// (`/<name> <brief>`) and the word every row says out loud.
	Name string `json:"name"`
	// Description is one sentence saying what the delegate does, in a person's
	// words. It is the command row's tail and the offer's second line.
	Description string `json:"description"`
	// Bin is the program: a bare name resolved on PATH or a path.
	Bin string `json:"bin"`
	// Argv is the argument list, with placeholders. It never includes the
	// program itself.
	Argv []string `json:"argv"`
	// Env is what is added to the child's environment, with placeholders. The
	// child also inherits the parent's environment.
	Env map[string]string `json:"env,omitempty"`
	// Lands is LandsTree or LandsText. Empty reads as LandsTree, because a
	// delegate that edits a tree is the one this was built for.
	Lands string `json:"lands,omitempty"`
	// Limits says which bounds the program honours itself. They are recorded
	// for the manual page and the offer; codeaf enforces cost and time from
	// outside whatever they say.
	Limits Limits `json:"limits,omitempty"`

	// Path is the manifest file this was read from, and ManualPath the page
	// beside it; Manual is that page's text, kept so the chat's manual can
	// layer it over the packed corpus (internal/manual's overlay). All three
	// are the loader's, never the file's.
	Path       string `json:"-"`
	ManualPath string `json:"-"`
	Manual     string `json:"-"`
	// BinPath is the program as it resolved at load time. The loader fills it;
	// a manifest whose Bin is not found is not in the registry at all.
	BinPath string `json:"-"`
}

// Limits is the manifest's own account of which bounds the program keeps.
type Limits struct {
	Cost      bool `json:"cost"`
	Elapsed   bool `json:"elapsed"`
	Steps     bool `json:"steps"`
	Questions bool `json:"questions"`
}

// nameShape is the one shape a name may have: lowercase letters, digits and
// single hyphens, starting with a letter. It is a command word, so it has to be
// something a person can type after a slash without quoting.
var nameShape = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// knownFills is every placeholder the launch fills. A manifest naming any
// other `{{…}}` is refused at load, so a typo is a sentence to the person and
// not a literal handed to the program.
var knownFills = map[string]bool{
	FillBrief: true, FillWorkspace: true, FillCostUSD: true, FillHours: true, FillKey: true, "{{key:openrouter}}": true,
}

var fillShape = regexp.MustCompile(`\{\{[^}]*\}\}`)

// Validate says whether a manifest is one the launch can run, naming the first
// thing wrong with it in a sentence a person can act on. It does not touch the
// disk: whether the binary exists and whether the page is there are the
// loader's readings, made beside this one.
func (m Manifest) Validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("the manifest names no delegate: `name` is empty")
	}
	if !nameShape.MatchString(m.Name) {
		return fmt.Errorf("%q is not a delegate name: one lowercase word, letters, digits and hyphens", m.Name)
	}
	if strings.TrimSpace(m.Description) == "" {
		return fmt.Errorf("%s: `description` is empty, and it is what the command row says", m.Name)
	}
	if strings.TrimSpace(m.Bin) == "" {
		return fmt.Errorf("%s: `bin` is empty, so there is nothing to run", m.Name)
	}
	if len(m.Argv) == 0 {
		return fmt.Errorf("%s: `argv` is empty; it must at least carry %s", m.Name, FillBrief)
	}
	if !strings.Contains(strings.Join(m.Argv, "\x00"), FillBrief) {
		return fmt.Errorf("%s: `argv` never says %s, so the task would never reach the program", m.Name, FillBrief)
	}
	switch m.Lands {
	case "", LandsTree, LandsText:
	default:
		return fmt.Errorf("%s: `lands` is %q; it is %q or %q", m.Name, m.Lands, LandsTree, LandsText)
	}
	for _, arg := range m.Argv {
		if err := checkFills(m.Name, arg); err != nil {
			return err
		}
	}
	for key, value := range m.Env {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%s: `env` carries an empty variable name", m.Name)
		}
		if err := checkFills(m.Name, value); err != nil {
			return err
		}
	}
	return nil
}

// checkFills refuses a placeholder the launch does not fill.
func checkFills(name, text string) error {
	for _, fill := range fillShape.FindAllString(text, -1) {
		if !knownFills[fill] {
			return fmt.Errorf("%s: %s is not a placeholder this build fills (they are %s, %s, %s, %s and %s)",
				name, fill, FillBrief, FillWorkspace, FillCostUSD, FillHours, FillKey)
		}
	}
	return nil
}

// LandsTree answers whether this delegate's work is a tree to land, which is
// the reading of an empty Lands too.
func (m Manifest) LandsTree() bool { return m.Lands == "" || m.Lands == LandsTree }
