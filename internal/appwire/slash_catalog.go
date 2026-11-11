package appwire

// ── THE SLASH CATALOGUE: the words the terminal chat answers to ─────────────
//
// slash.catalog lists every slash command the terminal chat accepts — name,
// aliases, argument forms and the terminal's one-line description — so the
// desktop's command menu is drawn from the engine's words and not from a copy
// of them. The static rows are internal/slashcat's one table (the terminal
// dispatches on the very same rows); the program rows (`/review`, `/sec`,
// `/senior-dev`) are the running build's, handed in by the engine through
// [SlashCataloger], because a program is a property of the build and not of the
// table.
//
// IT LISTS WORDS AND DOES NOT RUN THEM. The terminal's `app.slash` is a
// Bubble Tea dispatcher, not a shared engine call, and nothing on the wire
// interprets a command line: sending "/cost" through agents.send is a message,
// not a command. Each action a command stands for has its own typed method
// where one exists (docs/design/app-v3/PROTOCOL.md), and is the client's own
// action where it does not.
//
// WHAT THE ROWS DO NOT CLAIM.
//   - Icons, groups and fates are the desktop's, so no row carries one.
//   - description is the TERMINAL's wording, chords included (`ctrl+.`); a place
//     the terminal reaches by an alt+digit key has the key left off.
//   - host says whether the command works against a connection to another machine
//     (`--host`). That is the terminal client's state, which this engine cannot
//     see, so every row says "unknown"; it is a field so a later engine can say
//     otherwise without a new method.
//
// BOUNDED. At most [SlashCatalogCommandsMax] commands, [SlashCatalogFormsMax]
// forms and [SlashCatalogAliasesMax] aliases per command, names of at most
// [SlashCatalogWordMax] bytes and text of at most [SlashCatalogTextMax] bytes.
// The table is the same size on every call; a program row that would pass a
// bound is left out and named under `skipped`, never cut mid-word. The call
// reads no file, transcript or model, starts no watcher and takes no params.
//
// AN ENGINE WITHOUT THE CAPABILITY DOES NOT HAVE THE VERB: slash.catalog answers
// unknown_method from an engine that is not a [SlashCataloger], and hello omits
// the feature.

import (
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/slashcat"
)

// FeatureSlashCatalog is the hello feature an engine serving slash.catalog
// announces.
const FeatureSlashCatalog = "slash.catalog"

// The most a slash.catalog answer may hold.
const (
	SlashCatalogCommandsMax = 128
	SlashCatalogFormsMax    = 16
	SlashCatalogAliasesMax  = 8
	SlashCatalogWordMax     = 64
	SlashCatalogTextMax     = 512
)

// Where a command comes from, and what is known about it over a connection.
const (
	SlashSourceTable   = "table"
	SlashSourceProgram = "program"
	SlashHostUnknown   = "unknown"
)

// SlashProgram is one program the running build carries, as the engine reports
// it. Name is the command word; Args is what the row shows after it, and empty
// means `<brief>`.
type SlashProgram struct {
	Name          string
	Description   string
	Args          string
	BriefOptional bool
	// Lands is what the program leaves behind: "tree" or "text".
	Lands string
}

// SlashCataloger is the engine's half of slash.catalog. It is optional: an
// [Engine] that is not one refuses slash.catalog as unknown_method. It reports
// programs only; the static commands are the wire's own.
type SlashCataloger interface {
	SlashPrograms() []SlashProgram
}

// SlashForm is one way to type a command. args is empty for the bare form.
type SlashForm struct {
	Args        string `json:"args,omitempty"`
	Description string `json:"description"`
}

// SlashCommand is one canonical command. aliases are other words that run it
// (without the slash). door "task" marks a command that also acts inside a
// sentence. brief_optional and lands are set on programs only: a bare
// `/<name>` of a program with brief_optional runs its default brief.
type SlashCommand struct {
	Name          string      `json:"name"`
	Aliases       []string    `json:"aliases,omitempty"`
	Forms         []SlashForm `json:"forms"`
	Source        string      `json:"source" enum:"table,program"`
	Door          string      `json:"door,omitempty" enum:"task"`
	BriefOptional bool        `json:"brief_optional,omitempty"`
	Lands         string      `json:"lands,omitempty" enum:"tree,text"`
	Host          string      `json:"host" enum:"unknown"`
}

// SlashCatalogLimits are the bounds the answer was made under.
type SlashCatalogLimits struct {
	MaxCommands int `json:"max_commands"`
	MaxForms    int `json:"max_forms"`
	MaxAliases  int `json:"max_aliases"`
	MaxWord     int `json:"max_word"`
	MaxText     int `json:"max_text"`
}

// SlashCatalogResult is slash.catalog's answer: the table's commands in
// declaration order, then the build's programs by name. skipped names the
// programs left out and why, as the terminal answers a program whose word is
// already a command.
type SlashCatalogResult struct {
	Commands []SlashCommand     `json:"commands"`
	Skipped  []string           `json:"skipped,omitempty"`
	Limits   SlashCatalogLimits `json:"limits"`
}

func init() {
	methods["slash.catalog"] = typed(func(e Engine, _ struct{}) (*SlashCatalogResult, error) {
		c, ok := e.(SlashCataloger)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", "slash.catalog")
		}
		result := BuildSlashCatalog(c.SlashPrograms())
		return &result, nil
	})
}

// withSlashCatalogFeature is hello carrying [FeatureSlashCatalog] when the
// engine serves it.
func withSlashCatalogFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(SlashCataloger); !ok {
		return hello
	}
	return withFeature(hello, FeatureSlashCatalog)
}

// BuildSlashCatalog is the answer for a build carrying these programs. It is
// exported so the terminal's tests and the engine's can fold the same rows the
// wire does.
func BuildSlashCatalog(programs []SlashProgram) SlashCatalogResult {
	rows := slashcat.Rows()
	words := slashcat.Words(rows)
	out := SlashCatalogResult{
		Limits: SlashCatalogLimits{
			MaxCommands: SlashCatalogCommandsMax, MaxForms: SlashCatalogFormsMax, MaxAliases: SlashCatalogAliasesMax,
			MaxWord: SlashCatalogWordMax, MaxText: SlashCatalogTextMax,
		},
	}
	for _, c := range slashcat.Commands(rows) {
		cmd := SlashCommand{Name: c.Name, Aliases: c.Aliases, Door: c.Door, Source: SlashSourceTable, Host: SlashHostUnknown}
		for _, f := range c.Forms {
			cmd.Forms = append(cmd.Forms, SlashForm{Args: f.Args, Description: f.Desc})
		}
		out.Commands = append(out.Commands, cmd)
	}
	for _, p := range programs {
		name := strings.TrimSpace(p.Name)
		switch {
		case name == "":
			continue
		case !slashWord(name) || len(name) > SlashCatalogWordMax:
			out.Skipped = append(out.Skipped, clipText(name, SlashCatalogWordMax)+": not a command word — not added")
			continue
		case words[name]:
			out.Skipped = append(out.Skipped, name+": its name is already a command here — not added")
			continue
		case len(out.Commands) >= SlashCatalogCommandsMax:
			out.Skipped = append(out.Skipped, name+": the catalogue is full — not added")
			continue
		}
		words[name] = true
		args := strings.TrimSpace(p.Args)
		if args == "" {
			args = "<brief>"
		}
		out.Commands = append(out.Commands, SlashCommand{
			Name: name, Source: SlashSourceProgram, Host: SlashHostUnknown,
			Forms:         []SlashForm{{Args: clipText(args, SlashCatalogTextMax), Description: clipText(p.Description, SlashCatalogTextMax)}},
			BriefOptional: p.BriefOptional, Lands: slashLands(p.Lands),
		})
	}
	out.Skipped = clipList(out.Skipped, SlashCatalogCommandsMax)
	return out
}

// slashWord says a string is made of what the terminal's command list opens
// on: letters, digits and '-'.
func slashWord(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return s != ""
}

// slashLands is a program's landing in the wire's two words, and nothing for
// any other.
func slashLands(s string) string {
	if s == "tree" || s == "text" {
		return s
	}
	return ""
}

// clipText cuts s to at most max bytes on a rune boundary, with an ellipsis
// when it cut.
func clipText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func clipList(list []string, max int) []string {
	if len(list) > max {
		return list[:max]
	}
	return list
}
