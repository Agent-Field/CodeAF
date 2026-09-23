package delegate

// The command line every program answers: `codeaf <name> [command] [flags]
// [--] <brief>`. codeaf owns the verb, the dispatch and the four flags every
// program shares; the program owns its commands and their flags. The same
// line is what a person types at a shell and what a chat's run starts its
// child with ([ChildArgs]), so there is one parser for both.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrHelp is Parse's answer when the line asked for help and got it.
var ErrHelp = flag.ErrHelp

// Invocation is one `codeaf <name> …` line, parsed.
type Invocation struct {
	Program Delegate
	Command Command
	// Workspace is --dir, absolute; the current folder when it was not given.
	Workspace string
	// Ceilings are --max-cost and --max-hours.
	Ceilings Ceilings
	// JSON is --json: the records on stdout instead of readable lines. A child
	// of a host always writes records, so for it the flag only says so aloud.
	JSON bool
	// Args is what the flags left: the brief's words.
	Args []string
	// Line is the arguments exactly as given after the name, so a host can hand
	// its child the same line it was handed.
	Line []string
	body Body
}

// Brief is the brief's words, joined.
func (inv *Invocation) Brief() string { return strings.TrimSpace(strings.Join(inv.Args, " ")) }

// Parse reads the arguments after `codeaf <name>`. The first word picks a
// command when it names one; otherwise the program's default command runs on
// the whole line, so `codeaf senior-dev fix the flaky test` is its `run`. Help
// (`-h`, `--help`, or `help` as the first word) is written to out and answered
// as ErrHelp.
func Parse(program Delegate, line []string, out io.Writer) (*Invocation, error) {
	rest := line
	if len(rest) > 0 {
		switch rest[0] {
		case "help", "-h", "-help", "--help":
			// THE PROGRAM'S OWN PAGE FOR A BARE ASK. `codeaf <name> --help` is
			// asked before any command is named, so it answers with what the
			// program is and every command it has; a command's own flags are
			// one `codeaf <name> <command> --help` away, as the page ends by
			// saying.
			Help(program, out)
			return nil, ErrHelp
		}
	}
	command, named := program.Command(program.Default)
	if len(rest) > 0 {
		if c, ok := program.Command(rest[0]); ok {
			command, named, rest = c, true, rest[1:]
		}
	}
	if !named || command.Bind == nil {
		return nil, fmt.Errorf("%s has no command %q", program.Name, program.Default)
	}
	fs := flag.NewFlagSet(program.Name+" "+command.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "the folder to work in (default: the current folder)")
	cost := fs.Float64("max-cost", 0, "a dollar ceiling; codeaf refuses the call that would cross it")
	hours := fs.Float64("max-hours", 0, "a ceiling in hours of wall-clock time")
	asJSON := fs.Bool("json", false, "write the records on stdout instead of readable lines")
	body := command.Bind(fs)
	if body == nil {
		return nil, fmt.Errorf("%s %s: %w", program.Name, command.Name, errNoBody)
	}
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			commandHelp(program, command, fs, out)
			return nil, ErrHelp
		}
		return nil, fmt.Errorf("%s %s: %w", program.Name, command.Name, err)
	}
	if *cost < 0 || *hours < 0 {
		return nil, fmt.Errorf("%s %s: a ceiling cannot be negative", program.Name, command.Name)
	}
	workspace := *dir
	if strings.TrimSpace(workspace) == "" {
		workspace = "."
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("%s %s: --dir: %w", program.Name, command.Name, err)
	}
	return &Invocation{
		Program: program, Command: command,
		Workspace: abs,
		Ceilings:  Ceilings{CostUSD: *cost, Hours: *hours},
		JSON:      *asJSON,
		Args:      fs.Args(),
		Line:      append([]string(nil), line...),
		body:      body,
	}, nil
}

// ChildArgs is the line a host starts a program's process with, after
// codeaf's own executable: the name, the default command, --json, the folder,
// the ceilings that are set, and the brief after `--`, so no word of it can be
// read as a flag. [Parse] reads it back to the same invocation.
//
// AN UNSET CEILING IS NOT ON THE LINE. A program handed `--max-cost 0` might
// read it as a ceiling of nothing; one handed no flag reads no ceiling.
func ChildArgs(program Delegate, workspace, brief string, ceilings Ceilings) []string {
	args := []string{program.Name, program.Default, "--json", "--dir", workspace}
	if ceilings.CostUSD > 0 {
		args = append(args, "--max-cost", strconv.FormatFloat(ceilings.CostUSD, 'f', -1, 64))
	}
	if ceilings.Hours > 0 {
		args = append(args, "--max-hours", strconv.FormatFloat(ceilings.Hours, 'f', -1, 64))
	}
	return append(args, "--", brief)
}

// Help writes a program's help: what it is, its commands, and the flags every
// command takes.
//
// EVERY LINE FITS EIGHTY CELLS, the width codeaf's own help pages are held to
// (cmd/codeaf's helpwidth law); the build's list holds every carried program's
// pages to it (internal/delegate/builtin).
func Help(program Delegate, out io.Writer) {
	fmt.Fprintf(out, "codeaf %s: %s\n\n", program.Name, program.Summary)
	fmt.Fprintf(out, "usage:\n  codeaf %s [flags] <brief>          the same as %s\n", program.Name, program.Default)
	for _, c := range program.Commands {
		fmt.Fprintf(out, "  codeaf %s %s %s\n      %s\n", program.Name, c.Name, c.Usage, c.Summary)
	}
	fmt.Fprintf(out, "\nflags every command takes:\n")
	fmt.Fprintf(out, "  --dir DIR        the folder to work in (default: the current folder)\n")
	fmt.Fprintf(out, "  --max-cost USD   a dollar ceiling; codeaf refuses the call that would cross it\n")
	fmt.Fprintf(out, "  --max-hours H    a ceiling in hours of wall-clock time\n")
	fmt.Fprintf(out, "  --json           write the records on stdout instead of readable lines\n")
	fmt.Fprintf(out, "\n`codeaf %s <command> --help` lists a command's own flags.\n", program.Name)
}

// commandHelp is one command's help, with its own flags.
func commandHelp(program Delegate, command Command, fs *flag.FlagSet, out io.Writer) {
	fmt.Fprintf(out, "codeaf %s %s %s\n    %s\n\nflags:\n", program.Name, command.Name, command.Usage, command.Summary)
	fs.VisitAll(func(f *flag.Flag) {
		fmt.Fprintf(out, "  --%-14s %s\n", f.Name, f.Usage)
	})
}

// RunChild runs a parsed invocation as the child of a host: its records go to
// stdout as JSON lines and its models come from the environment. It answers
// the status of the ending it wrote, and the caller turns that into the exit
// code.
//
// EXACTLY ONE TERMINAL, ON EVERY PATH. A body that returns without writing one
// gets one written for it here — the context's end, the error it returned, or
// the plain fact that it said nothing — because a host reads a missing
// terminal as work that did not finish and says only that, and the reason the
// body knew would be lost.
func RunChild(ctx context.Context, inv *Invocation, stdout io.Writer) string {
	api, _ := ModelAPIFromEnv()
	emitter := NewEmitter(stdout)
	host := &childHost{inv: inv, emitter: emitter, api: api, ending: StatusFail}
	var err error
	if !api.Ready() {
		err = errors.New("this run has no model API: codeaf starts " + inv.Program.Name + " with one, and a shell run hosts its own")
	} else {
		err = inv.body(ctx, host, inv.Args)
	}
	if !emitter.Ended() {
		switch {
		case ctx.Err() != nil:
			host.Terminal(Ending{Status: StatusFail, Message: "stopped before it finished"})
		case err != nil:
			host.Terminal(Ending{Status: StatusCrashed, Message: firstLineOf(err.Error())})
		default:
			host.Terminal(Ending{Status: StatusFail, Message: "it ended without saying how"})
		}
	}
	return host.ending
}

// childHost is the Host of a program running as a child: records to stdout,
// models from the environment.
type childHost struct {
	inv     *Invocation
	emitter *Emitter
	api     ModelAPI
	ending  string
}

func (h *childHost) Workspace() string  { return h.inv.Workspace }
func (h *childHost) Ceilings() Ceilings { return h.inv.Ceilings }
func (h *childHost) Models() ModelAPI   { return h.api }
func (h *childHost) Hello(stages []string) {
	_ = h.emitter.Hello(h.inv.Program.Name, stages)
}
func (h *childHost) Stage(stage, status string)       { _ = h.emitter.Stage(stage, status) }
func (h *childHost) Step(command, observation string) { _ = h.emitter.Step(command, observation) }
func (h *childHost) Terminal(end Ending) {
	if h.emitter.Ended() {
		return
	}
	if !KnownStatus(end.Status) {
		end.Status = StatusCrashed
	}
	h.ending = end.Status
	_ = h.emitter.Terminal(end)
}

// firstLineOf is an error's first line, because an ending's message is one
// sentence.
func firstLineOf(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
