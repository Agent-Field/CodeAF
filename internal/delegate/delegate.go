// Package delegate is what codeaf needs of the programs it carries and can hand
// a whole task to — senior-dev first (docs/design/delegate/PROTOCOL.md): what
// one IS (a Go value in the build's list, internal/delegate/builtin), the
// command line every one of them answers (`codeaf <name> …`), the records a
// running one writes on its stdout and the one reader over them, the model API
// that is its only road to a model, the log of the conversation it holds over
// that road, and the launch of it as a child process that streams, stops on
// SIGTERM and ends with one terminal record.
//
// "DELEGATE" IS A WORKING TITLE. Everything a person reads names the program
// itself — `/senior-dev`, `codeaf senior-dev`, its own manual page — and only
// code says delegate, where a later rename is one package move.
//
// A PROGRAM CODEAF CARRIES IS STILL A PROGRAM APART. It is compiled into this
// binary, but it runs as a child process of it (`codeaf <name> run --json …`),
// so a crash in its engine cannot take the chat down, and it reaches a model
// only through the API codeaf serves it for that one run, so it never holds a
// key. What runs one AS A WORKER of a run — the live step, the trajectory, the
// spend bank — is internal/run's; nothing here knows what a task is.
//
// THIS PACKAGE IS A LEAF ON PURPOSE. The session door lists the programs and
// checks a name; the run engine seats one; the command line runs one; none of
// them may import the others, so what they share lives here and imports none
// of them.
package delegate

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// The two things a program can leave behind.
const (
	// LandsTree is a program that works in the working copy it is given and
	// leaves its changes there: codeaf squashes them into one commit and merges
	// that home the way every task lands.
	LandsTree = "tree"
	// LandsText is a program that changes nothing in the folder and puts its
	// answer in the terminal record's deliverable: codeaf folds the text into
	// the conversation the way a quick task's answer arrives.
	LandsText = "text"
)

// Delegate is one program this build carries. It is a value in the build's
// list (internal/delegate/builtin), never a file on the machine: there is
// nothing to install, and no version of it that differs from the codeaf it
// ships in.
type Delegate struct {
	// Name is one lowercase word with single hyphens: the chat command
	// (`/<name> <brief>`), the command line's verb (`codeaf <name>`) and the
	// word every row says out loud.
	Name string
	// Summary is one sentence saying what it does, in a person's words: the
	// command row's tail and its line in `codeaf --help`.
	Summary string
	// Guide is the program describing itself to the model that hands it work:
	// what it is for, what its brief must hold, and what it needs of its
	// folder. The conversation prints it under the program's name, where the
	// model reads which programs it can name in `via`, and says nothing about
	// the program of its own.
	//
	// THE PROGRAM OWNS WHAT IS TRUE OF IT, AND CODEAF OWNS WHAT IS TRUE OF
	// EVERY PROGRAM. The copy a program works in, what lands from it and the
	// fact that nobody can be asked anything are codeaf's mechanics, stated
	// once beside the list; a guide that restated them would be one more copy
	// to drift. A second program brings its own guide, and the conversation's
	// page never has to learn its name.
	//
	// IT RIDES EVERY REQUEST OF EVERY TURN, because the paragraph is part of
	// the conversation's fixed prefix (internal/session's prefixbudget_test.go
	// weighs it), so it is one paragraph of at most [GuideMax] bytes.
	Guide string
	// Lands is LandsTree or LandsText. Empty reads as LandsTree, because a
	// program that edits a tree is the one this was built for.
	Lands string
	// PlainFolder is the flags the default command takes to work in a folder
	// with no git history, which codeaf puts on the line itself when the folder
	// it hands a tree program is one ([ChildArgs]). Empty is a program that
	// needs no flag for it, or cannot work there and says so in its ending.
	//
	// CODEAF DECIDES, BECAUSE CODEAF KNOWS. The folder is the one the task was
	// proposed on, and whether it has a history to cut a working copy from is
	// read by codeaf before the program starts: a plain folder is worked in
	// where it is, and the program is told so on its line. The program says
	// only how it is told, so codeaf never has to learn its flag's name.
	PlainFolder []string
	// Notes is the folder, relative to the folder it works in, where the
	// program keeps its own records while it works: its copy of the brief, its
	// checklist, its session's database and its whole conversation with its
	// model. Empty is a program that keeps nothing there.
	//
	// A PLAIN FOLDER'S NOTES ARE MOVED OUT OF IT. A program handed a folder with
	// no git history works in the person's folder itself, so its records were
	// left there when it ended — 46 files for one senior-dev run, a database and
	// the full conversation among them — where a `git add -A` would commit them.
	// codeaf moves the folder this names into the task's own record folder when
	// such a run ends. In a copy they stay in the copy, which the person's
	// folder never sees.
	Notes string
	// CrewFlags is the flags the default command takes to use the models of
	// the conversation's crew ([Crew]), which codeaf puts on the line of every
	// run it starts from a conversation. Nil is a program that picks its own
	// models whatever the crew says.
	//
	// THE PERSON'S CREW IS THE DEFAULT, AND THE PROGRAM SAYS HOW IT HEARS IT.
	// A person who set which models do the thinking and the typing expects a
	// program they hand work to to use them too, rather than a list of its
	// own they never chose; codeaf knows the crew and nothing of the program's
	// flags, so the program turns the one into the other.
	CrewFlags func(Crew) []string
	// StageWords is the word a person reads for each stage the program reports
	// (its `stage` record), keyed by the stage's own name. The task's row and
	// the line over its conversation show the word, never the name: a program's
	// stages are its machinery — senior-dev's say `agent-runtime` and
	// `router-cancellation` — and this house draws no machinery vocabulary.
	// A stage with no word leaves the word shown before it standing, so a
	// program's inner phases need not each be named. Nil shows every stage by
	// its own name, for a program that has not said.
	StageWords map[string]string
	// Present is the program's own vocabulary for its task's page: it makes a
	// reader ([ActionReader]) that turns each line of its action log
	// ([Action]) — a stage, a step or its ending — into the words a person
	// reads, under the word for the step of its process it served ([Shown]),
	// and answers false for a line the page leaves out. Nil reads every line
	// plainly ([Delegate.Reader]).
	//
	// THE PROGRAM KNOWS WHAT ITS RECORDS MEAN, AND CODEAF KNOWS HOW A PAGE IS
	// DRAWN. A stage named `submit` with `patch_files` in its data is
	// senior-dev's machinery; that it reads `handed in its work · 4 files` is
	// senior-dev's to say, once, beside the words it gives its stages. The page
	// draws whatever a program says here, and the same step word leads the
	// task's row while the program is in that step.
	Present func() ActionReader
	// Default is the command a bare brief runs: `/<name> <brief>` in the chat
	// and `codeaf <name> <brief>` in a shell. It names one of Commands.
	Default string
	// Commands is the program's own verbs, each with its own flags. codeaf owns
	// the dispatch and the flags every program shares; the program owns these.
	Commands []Command
	// Page is the name of its page in the chat's manual (internal/manual/chat):
	// what it does, how to ask it, what a run costs, where the work lands. It is
	// compiled in with the rest of the manual, so the manual law's own gates
	// hold it to that.
	Page string
}

// Command is one verb a program answers to:
// `codeaf <name> <command> [flags] -- <brief>`.
type Command struct {
	Name string
	// Usage is the shape of the line after the command's name, for its help:
	// `[flags] -- <brief>`.
	Usage   string
	Summary string
	// Bind declares the command's own flags on fs and answers its body, which
	// reads them once the line has been parsed. It is called once per
	// invocation, so the values live in the closure and never in package
	// state. codeaf's shared flags (--dir, --max-cost, --max-hours, --json) are
	// already on fs; a command may not declare them again.
	Bind func(fs *flag.FlagSet) Body
}

// Body is a command's work. It runs to its ending and reports through the host
// — the ending included, as one [Host.Terminal] — and answers an error only
// for a failure it could not put into that record itself. args is what the
// flags left on the line: the brief's words.
type Body func(ctx context.Context, host Host, args []string) error

// nameShape is the one shape a name may have: lowercase letters, digits and
// single hyphens, starting with a letter. It is a command word twice over — a
// slash command and a shell verb — so it has to be something a person can
// type without quoting.
var nameShape = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// Crew is the models a conversation's crew seats, by what each is for, as ids
// on the service codeaf's model API speaks for (`vendor/model`), with no
// effort suffix. An empty field is a seat the crew leaves unset.
type Crew struct {
	// Brain is the planning seat: the model the crew thinks hardest with.
	Brain string
	// Hands is the working seat: the model the crew does the work with.
	Hands string
	// Light is the cheap seat: summaries, and whatever needs no depth.
	Light string
	// Asked is the models the person asked this run to work with, in their
	// words' order, already resolved to ids. When it is set it is the working
	// seat in place of Hands, and a program may not swap any of it for another:
	// one it cannot use is a refusal, said before anything is spent.
	Asked []string
}

// IsZero says the crew names no model at all, so no flag is owed for it.
func (c Crew) IsZero() bool {
	return c.Brain == "" && c.Hands == "" && c.Light == "" && len(c.Asked) == 0
}

// GuideMax is the most bytes a program's [Delegate.Guide] may take. It is a
// paragraph a model reads on every turn of every conversation that carries the
// program, so it is held to what a model needs to choose the program and brief
// it, and the program's manual page carries the rest.
const GuideMax = 400

// sharedFlags are the flags codeaf puts on every command's line. A command
// declaring one of them again would panic inside the flag package at parse
// time, so Validate refuses it by name first.
var sharedFlags = []string{"dir", "max-cost", "max-hours", "json"}

// Validate names the first thing wrong with a program's definition in a
// sentence the person who wrote it can act on. The build's own test runs it on
// every program the list carries (internal/delegate/builtin), so a definition
// that could not run never reaches a person.
func (d Delegate) Validate() error {
	if !nameShape.MatchString(d.Name) {
		return fmt.Errorf("%q is not a program name: one lowercase word, letters, digits and single hyphens", d.Name)
	}
	if strings.TrimSpace(d.Summary) == "" {
		return fmt.Errorf("%s: the summary is empty, and it is what the command row says", d.Name)
	}
	switch guide := strings.TrimSpace(d.Guide); {
	case guide == "":
		return fmt.Errorf("%s: the guide is empty, so the model that hands it work is told nothing but its name", d.Name)
	case strings.Contains(guide, "\n"):
		return fmt.Errorf("%s: the guide is one paragraph and has no line breaks, because it is printed as one item of a list", d.Name)
	case len(guide) > GuideMax:
		return fmt.Errorf("%s: the guide is %d bytes; it rides every request of every turn, so it is held to %d", d.Name, len(guide), GuideMax)
	}
	switch d.Lands {
	case "", LandsTree, LandsText:
	default:
		return fmt.Errorf("%s: lands is %q; it is %q or %q", d.Name, d.Lands, LandsTree, LandsText)
	}
	if strings.TrimSpace(d.Page) == "" {
		return fmt.Errorf("%s: it names no manual page, and the chat can only say what a page says", d.Name)
	}
	if len(d.Commands) == 0 {
		return fmt.Errorf("%s: it has no commands, so there is nothing to run", d.Name)
	}
	seen := map[string]bool{}
	for _, c := range d.Commands {
		if !nameShape.MatchString(c.Name) {
			return fmt.Errorf("%s: %q is not a command name", d.Name, c.Name)
		}
		if seen[c.Name] {
			return fmt.Errorf("%s: the command %q is defined twice", d.Name, c.Name)
		}
		seen[c.Name] = true
		if c.Bind == nil {
			return fmt.Errorf("%s %s: the command has no body", d.Name, c.Name)
		}
		fs := flag.NewFlagSet(d.Name+" "+c.Name, flag.ContinueOnError)
		if c.Bind(fs) == nil {
			return fmt.Errorf("%s %s: binding the command answered no body", d.Name, c.Name)
		}
		for _, shared := range sharedFlags {
			if fs.Lookup(shared) != nil {
				return fmt.Errorf("%s %s: --%s is codeaf's own flag and may not be declared again", d.Name, c.Name, shared)
			}
		}
	}
	if !seen[d.Default] {
		return fmt.Errorf("%s: the default command %q is not one of its commands", d.Name, d.Default)
	}
	if err := d.validateLineFlags(); err != nil {
		return err
	}
	return nil
}

// validateLineFlags holds the flags codeaf puts on the program's line for it —
// for a plain folder, and for the conversation's crew — to its default command:
// a flag the command does not take would end every such run at its first line,
// so it fails here, in the build's own test.
func (d Delegate) validateLineFlags() error {
	command, _ := d.Command(d.Default)
	parses := func(flags []string) bool {
		fs := flag.NewFlagSet(d.Name+" "+command.Name, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		command.Bind(fs)
		return fs.Parse(flags) == nil && fs.NArg() == 0
	}
	if d.CrewFlags != nil {
		for _, sample := range []Crew{
			{Brain: "vendor/brain", Hands: "vendor/hands", Light: "vendor/light"},
			{Hands: "vendor/hands", Light: "vendor/light", Asked: []string{"vendor/one", "vendor/two"}},
		} {
			if flags := d.CrewFlags(sample); !parses(flags) {
				return fmt.Errorf("%s: the crew flags %q are not flags its %s command takes", d.Name, strings.Join(flags, " "), command.Name)
			}
		}
	}
	if len(d.PlainFolder) > 0 && !parses(d.PlainFolder) {
		return fmt.Errorf("%s: the plain folder flags %q are not flags its %s command takes", d.Name, strings.Join(d.PlainFolder, " "), command.Name)
	}
	return nil
}

// LandsTree answers whether this program's work is a tree to land, which is
// the reading of an empty Lands too.
func (d Delegate) LandsTree() bool { return d.Lands == "" || d.Lands == LandsTree }

// Command finds one of the program's commands by name.
func (d Delegate) Command(name string) (Command, bool) {
	for _, c := range d.Commands {
		if c.Name == name {
			return c, true
		}
	}
	return Command{}, false
}

// errNoBody is what binding a command without a body answers, so a definition
// Validate never saw still fails in words rather than with a nil call.
var errNoBody = errors.New("the command has no body")
