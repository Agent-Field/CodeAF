package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// This file is the ONE SEAM every subcommand's flags are built and parsed at.
//
// It exists because the same three lines were copied into eight doors and left
// out of seven others, and the two halves disagreed about the one gesture every
// developer makes first. `aforge doctor --help` printed Go's internal string
// `flag: help requested` and nothing else; `aforge do --help` printed a usage
// block and then the same internal string under `error:` and left with 1. A
// Makefile that runs `aforge do --help` to see whether the binary is healthy
// read a failing command, and a person read the word "error" under text that
// was not one.

// usageOut and usageErr are where help is written. They are variables rather
// than os.Stdout and os.Stderr spelled inline so a test can read back what was
// on the screen, which is the only way to check a claim about printed text.
var (
	usageOut io.Writer = os.Stdout
	usageErr io.Writer = os.Stderr
)

// exitHelped is what a door leaves with when the only thing it was asked for
// was its own usage.
//
// ASKING FOR HELP IS NOT A FAILURE. The text has already gone to stdout, the
// process has done exactly what it was told to, and the code is zero. It is an
// error value at all only because that is how a door hands the dispatch an exit
// code without printing an `error:` line over the top of its own output (see
// [exitStatus] and the switch in [execute]).
const exitHelped exitStatus = 0

// commandFlags builds a subcommand's flag set.
//
// The flag package's own output is discarded HERE, once, rather than at eight
// call sites and nowhere else: on a bad flag the package printed its message
// and the whole flag list, and then the dispatch printed the same message a
// second time under `error:`, leaving the reader to work out that the two were
// one fact.
func commandFlags(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags
}

// parseCommandFlags is the other half of the seam, and the only place in this
// binary that decides what asking for help costs.
//
// Three outcomes and three exits: a clean parse returns nil and the command
// runs; `--help` prints the command's usage on STDOUT and leaves with 0; a real
// flag error prints one sentence and the same usage on STDERR and leaves with
// 1. Callers pass the arguments already reordered where they take positionals
// ([reorder]), because that reading belongs to the door's own grammar and not
// to this seam.
func parseCommandFlags(flags *flag.FlagSet, args []string) error {
	err := flags.Parse(args)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, flag.ErrHelp):
		writeCommandUsage(usageOut, flags)
		return exitHelped
	default:
		// One sentence, and then the command's own text — never the flag
		// package's dump of the same list under a different spelling.
		fmt.Fprintln(usageErr, "error:", err)
		writeCommandUsage(usageErr, flags)
		// A flag that could not be read is the first rung of the one ladder:
		// nothing was attempted, so nothing ran (envelope.go).
		return exitCannotRun
	}
}

// writeCommandUsage is one command's whole account of itself: the shape it is
// called with, lifted out of [usageText] so the two can never disagree, then
// its flags, then the one line that says where the rest is.
func writeCommandUsage(w io.Writer, flags *flag.FlagSet) {
	shape := usageForCommand(flags.Name())
	if shape == "" {
		// A door with no line in the table — `aforge engine`, which is
		// machinery a surface dials rather than a thing a person runs — still
		// says what it is called and what it takes.
		shape = "  aforge " + flags.Name()
	}
	fmt.Fprintln(w, shape)
	if rows := flagRows(flags); rows != "" {
		fmt.Fprint(w, rows)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "run `aforge --help` for every command, `aforge help env` for the environment table.")
}

// flagRows writes a flag set the way the usage table spells flags — two dashes
// for a word, one for a single letter. The flag package writes one dash for
// everything, so `aforge --help` and `aforge do --help` showed two conventions
// for the same flag and left a reader guessing whether both worked. They do;
// only one of them is written down.
func flagRows(flags *flag.FlagSet) string {
	var rows strings.Builder
	flags.VisitAll(func(f *flag.Flag) {
		// A HIDDEN FLAG IS NOT PRINTED. An old spelling kept working for one
		// release, and a single letter kept working forever, are both flags a
		// door answers to and neither is a flag a person should be taught to
		// type — printing them would make `--budget` and `--token-budget` read
		// as two knobs (rename.go).
		if _, _, hidden := hiddenFlag(f); hidden {
			return
		}
		placeholder, usage := flag.UnquoteUsage(f)
		// Two dashes for a word and one for a letter, which is exactly how the
		// table spells them: `--json`, `--timeout`, `-w`.
		head := "  --" + f.Name
		if len(f.Name) == 1 {
			head = "  -" + f.Name
		}
		if placeholder != "" {
			head += " " + placeholder
		}
		if shown := shownDefault(f); shown != "" {
			usage = strings.TrimSpace(usage) + " (default " + shown + ")"
		}
		rows.WriteString(head + "\n")
		for _, line := range wrapAt(usage, 74) {
			rows.WriteString("      " + line + "\n")
		}
	})
	if rows.Len() == 0 {
		return ""
	}
	return "\nflags:\n" + rows.String()
}

// shownDefault is the emptiness law on a usage page: a default of zero, empty
// or false is not a fact worth a parenthesis, and printing `(default 0)` beside
// half the flags in the binary buried the three defaults that matter.
func shownDefault(f *flag.Flag) string {
	switch strings.TrimSpace(f.DefValue) {
	case "", "0", "false", "0s":
		return ""
	}
	return f.DefValue
}

// wrapAt folds one flag's sentence to a width that fits an eighty-column
// terminal under the six-space indent the rows are written at.
func wrapAt(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	lines := []string{words[0]}
	for _, word := range words[1:] {
		last := len(lines) - 1
		if len(lines[last])+1+len(word) > width {
			lines = append(lines, word)
			continue
		}
		lines[last] += " " + word
	}
	return lines
}

// longerCommands are the spellings that begin with another command's whole name
// and are dispatched somewhere else entirely. They are the only reason
// [commandLine] has to look past the words it was asked about.
//
// `run subharness` WAS THE FIRST ENTRY AND IS GONE, because the thing it was
// working around is gone. Its whole job was to stop `aforge run --help`
// printing the saved-program runner's line as though it were the graph
// runner's, and a verb whose help needs a special case to say which of two
// commands it is, is a verb wearing two meanings: `run` now means one thing —
// run a saved program — and the static pipeline is `aforge plan new|show|
// revise|run`, four lines that all begin `aforge plan` and therefore cannot be
// mistaken for `aforge run`'s.
//
// `cache clean` STAYS, and it is a different shape: `aforge cache` and `aforge
// cache clean` are one noun with two verbs on it, not one word meaning two
// things, and without this entry `aforge cache --help` would print the
// destructive command's line under the harmless one's name.
// `devices revoke` is the same shape as `cache clean` and is here for the same
// reason: one noun, a harmless reading verb and a destructive one, and without
// the entry `aforge devices --help` would print the revoking line under the
// listing's name.
var longerCommands = []string{"cache clean", "devices revoke"}

// usageForCommand lifts one command's lines out of [usageText].
//
// ONE SOURCE OF TRUTH: the shape of a command — what it is called, what it
// takes, what its exit codes mean — is written once, in the table `aforge
// --help` prints, and every per-command usage is a reading of that table. A
// synopsis typed out a second time beside the flags would be stale by the next
// flag anybody added, which is the same defect the environment table's
// interpolated dollar figures were fixed for.
func usageForCommand(name string) string {
	lines := strings.Split(usageText, "\n")
	var blocks []string
	for index := 0; index < len(lines); index++ {
		if !strings.HasPrefix(lines[index], "  aforge ") {
			continue
		}
		block := []string{lines[index]}
		for next := index + 1; next < len(lines); next++ {
			following := lines[next]
			// A continuation is indented under the command it belongs to. A
			// blank line, the environment table, or the next command ends it.
			if strings.TrimSpace(following) == "" ||
				strings.HasPrefix(following, "  aforge ") ||
				!strings.HasPrefix(following, "   ") {
				break
			}
			block = append(block, following)
			index = next
		}
		if commandLine(block[0], name) {
			blocks = append(blocks, strings.Join(block, "\n"))
		}
	}
	return strings.Join(blocks, "\n")
}

// commandLine reports whether one line of the table is this command's own.
func commandLine(line, name string) bool {
	fields := strings.Fields(strings.TrimSpace(line))
	wanted := strings.Fields(name)
	if len(wanted) == 0 || len(fields) < 1+len(wanted) || fields[0] != "aforge" {
		return false
	}
	for index, word := range wanted {
		if fields[1+index] != word {
			return false
		}
	}
	for _, longer := range longerCommands {
		if strings.HasPrefix(longer, name+" ") && commandLine(line, longer) {
			return false
		}
	}
	return true
}

// askedForHelp is the same gesture read by a door that parses NO flags at all.
//
// `show`, `manual` and `models` take a positional and nothing else, so
// `aforge show --help` answered `open --help: no such file or directory` — a
// filesystem error about a flag — and `aforge models --help` ran the command
// with the flag silently ignored. A person probing an unfamiliar command types
// this first and is owed the usage, not a stat error.
func askedForHelp(args []string) bool {
	for _, argument := range args {
		switch argument {
		case "-h", "-help", "--help":
			return true
		}
	}
	return false
}

// commandHelp answers that gesture for a door with no flag set of its own.
func commandHelp(name string) error {
	writeCommandUsage(usageOut, commandFlags(name))
	return exitHelped
}

// unknownCommand is what a typo is answered with.
//
// It used to be `unknown command "lgos"` followed by the entire usage text —
// every command and the whole environment table — so the one line that mattered
// scrolled off the top of the terminal and the obvious next step was never
// named. Now it is the miss, the nearest thing to it, and where the rest is.
func unknownCommand(typed string) error {
	if nearest := nearestCommand(typed); nearest != "" {
		return fmt.Errorf("there is no `aforge %s`. did you mean `aforge %s`?\n"+
			"run `aforge --help` for every command", typed, nearest)
	}
	return fmt.Errorf("there is no `aforge %s`.\nrun `aforge --help` for every command", typed)
}

// nearestCommand is the one thing a person wants after a typo: the command they
// meant. It answers only when the miss is close enough to be a slip of the
// fingers — one or two edits — because a confident wrong suggestion is worse
// than none.
func nearestCommand(typed string) string {
	typed = strings.ToLower(strings.TrimSpace(typed))
	if typed == "" {
		return ""
	}
	// TWO EDITS AND NO MORE. A slip of the fingers is one or two characters —
	// `lgos`, `doo`, `doctro` — and past that the nearest word in the list is
	// not what anybody meant: `quux` is three edits from `run`, and answering
	// with it would send somebody confidently to the wrong command.
	best, distance := "", 3
	for _, candidate := range knownCommands {
		if measured := editDistance(typed, candidate); measured < distance {
			best, distance = candidate, measured
		}
	}
	return best
}

// knownCommands is every word the dispatch answers to, in the order the table
// introduces them. `engine` and `tick` are deliberately absent for the same
// reason they are absent from the usage text: nothing types them.
var knownCommands = []string{
	"chat", "resume", "serve", "devices", "do", "plan", "revise", "run", "exec",
	"show", "models", "notebook", "competence", "services", "wake", "doctor",
	"logs", "cache", "rebuild", "why", "manual", "version", "help",
}

// editDistance is the ordinary Levenshtein distance, one row at a time.
func editDistance(from, to string) int {
	previous := make([]int, len(to)+1)
	current := make([]int, len(to)+1)
	for index := range previous {
		previous[index] = index
	}
	for row := 1; row <= len(from); row++ {
		current[0] = row
		for column := 1; column <= len(to); column++ {
			cost := 1
			if from[row-1] == to[column-1] {
				cost = 0
			}
			current[column] = min(previous[column]+1, min(current[column-1]+1, previous[column-1]+cost))
		}
		previous, current = current, previous
	}
	return previous[len(to)]
}
