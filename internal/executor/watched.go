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

// Around implements Seat: the call runs on the seat beneath, then the tools its
// command line named are observed. A call the seat refused ran nothing.
func (w watched) Around(ctx context.Context, call Call, run func() ([]byte, bool)) error {
	if err := w.Seat.Around(ctx, call, run); err != nil {
		return err
	}
	for _, name := range shellTools(call) {
		w.obs.Observe(ExecRequest{Argv: []string{name}}, ExecResult{})
	}
	return nil
}

// shellTools is the executable names a shell call's command line starts.
func shellTools(call Call) []string {
	if call.Tool != shellTool {
		return nil
	}
	var args struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(call.Args, &args) != nil {
		return nil
	}
	return commandNames(args.Command)
}

// commandNames is the first word of every simple command in a shell line, after
// any leading VAR=value words. It reads the line's shape and not its meaning:
// a name that is not an executable is dropped by whoever resolves it.
func commandNames(line string) []string {
	var names []string
	for _, segment := range splitCommands(line) {
		if name := leadWord(segment); name != "" {
			names = append(names, name)
		}
	}
	return names
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

// leadWord is the command a segment starts, without quotes.
func leadWord(segment string) string {
	for _, word := range strings.Fields(segment) {
		if !isAssignment(word) {
			return strings.Trim(word, `'"`)
		}
	}
	return ""
}

func isAssignment(word string) bool {
	name, _, found := strings.Cut(word, "=")
	return found && name != "" && !strings.ContainsAny(name, `/'"$`)
}
