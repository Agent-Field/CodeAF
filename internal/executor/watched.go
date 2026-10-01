package executor

import (
	"context"
	"encoding/json"
	"strings"
)

// A shell tool starts its process through Command, which the observer never
// hears of (it sees only what Exec ran), and the process it starts is the shell:
// the tools a command line runs inside `bash -c` are the shell's own children.
// So the seat reports them from the boundary that has the command line, the one
// place a tool call is told to the seat ([Seat.Around]).

// shellTool is the tool whose arguments carry a shell command line.
const shellTool = "bash"

// Watching is seat with every tool a shell call ran told to obs, as though each
// had been run on its own. A nil obs leaves the seat as it was.
func Watching(seat Seat, obs Observer) Seat {
	if obs == nil {
		return seat
	}
	return watched{Seat: seat, obs: obs}
}

type watched struct {
	Seat
	obs Observer
}

// ForSetup implements SetupSeat: the setup form of the seat is watched too.
func (w watched) ForSetup() Seat { return Watching(ForSetup(w.Seat), w.obs) }

// NoteModelCall implements ModelNoter: the seat beneath keeps the record.
func (w watched) NoteModelCall(m ModelCall) { NoteModelCall(w.Seat, m) }

// Interrupted implements Interruptible: watching adds nothing to the record, so
// the answer is the seat beneath's.
func (w watched) Interrupted() Interrupted { return InterruptedOn(w.Seat) }

// Around implements Seat: the call runs on the seat beneath, and the commands
// its command line named, and whatever it left running, are observed as soon as
// it has run, each with the whole of its words and the call's own outcome. That
// is inside the seat beneath and not after it, because the seat seals the tree
// once the call has run: a record told afterwards would miss the one seal that
// can carry it to another machine. A call the seat refused ran nothing, so
// nothing is observed.
func (w watched) Around(ctx context.Context, call Call, run func() ([]byte, bool)) error {
	return w.Seat.Around(ctx, call, func() ([]byte, bool) {
		out, failed := run()
		w.observe(call, failed)
		return out, failed
	})
}

// observe tells the observer each command the call ran and each service it left.
func (w watched) observe(call Call, failed bool) {
	for _, words := range shellCommands(call) {
		w.obs.Observe(ExecRequest{Argv: words}, ExecResult{Exit: exitOf(failed)})
	}
	for _, s := range Left(call) {
		w.obs.Observe(ExecRequest{Argv: s.Argv}, ExecResult{Exit: exitOf(failed), Services: []Service{s}})
	}
}

// exitOf is the exit status a shell call's outcome stands for: the call is the
// unit that succeeded or failed, so each command it ran shares its verdict.
func exitOf(failed bool) int {
	if failed {
		return 1
	}
	return 0
}

// shellCommands is the words of every simple command a shell call's command
// line runs, and nothing for any other call.
func shellCommands(call Call) [][]string {
	if call.Tool != shellTool {
		return nil
	}
	var args struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(call.Args, &args) != nil {
		return nil
	}
	return commandWords(args.Command)
}

// SimpleCommands is each simple command of a shell line as its own words joined
// by single spaces, leading VAR=value words and quotes kept as written. A line
// is cut at ; & | ( ) and newlines that are not quoted, so a command substitution
// or a pipe to another program is a segment of its own and never hides inside
// the words of another. It is what a caller compares exactly against a command
// it has been told to expect.
func SimpleCommands(line string) []string {
	var out []string
	for _, segment := range splitCommands(line) {
		if words := strings.Fields(segment); len(words) > 0 {
			out = append(out, strings.Join(words, " "))
		}
	}
	return out
}

// commandNames is the first word of every simple command in a shell line, after
// any leading VAR=value words. It reads the line's shape and not its meaning:
// a name that is not an executable is dropped by whoever resolves it.
func commandNames(line string) []string {
	var names []string
	for _, words := range commandWords(line) {
		names = append(names, words[0])
	}
	return names
}

// commandWords is the words of each simple command in a shell line, from its
// command name on: the leading VAR=value words are not part of the command.
func commandWords(line string) [][]string {
	var commands [][]string
	for _, segment := range splitCommands(line) {
		if words := commandOf(strings.Fields(segment)); len(words) > 0 {
			commands = append(commands, words)
		}
	}
	return commands
}

// commandOf drops the assignments a command starts with and the quotes around
// the command's name, which is the word an observer resolves on PATH.
func commandOf(words []string) []string {
	for i, word := range words {
		if !isAssignment(word) {
			out := append([]string(nil), words[i:]...)
			out[0] = strings.Trim(out[0], `'"`)
			return out
		}
	}
	return nil
}

// splitCommands cuts a line at ; & | ( ) and newlines that are not quoted or
// escaped.
func splitCommands(line string) []string {
	var segments []string
	var current strings.Builder
	var quote rune
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case strings.ContainsRune(";&|()\n", r):
			segments = append(segments, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(r)
	}
	return append(segments, current.String())
}

func isAssignment(word string) bool {
	name, _, found := strings.Cut(word, "=")
	return found && name != "" && !strings.ContainsAny(name, `/'"$`)
}

// Settle implements Settler: the seat beneath keeps the record.
func (w watched) Settle(ctx context.Context) { Settle(ctx, w.Seat) }
