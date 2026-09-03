package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// EVERY OLD SPELLING SURVIVES FOR ONE RELEASE, WORKS EXACTLY AS IT DID, IS
// ABSENT FROM `--help`, AND SAYS ONCE WHAT IT IS CALLED NOW.
//
// This file is the whole of that promise: the writer the notice goes to, the
// one sentence it is spelled with, and the two kinds of hidden flag a door can
// carry. It exists because the alternative — a rename that simply refuses the
// old word — breaks every script somebody has in production on the day they
// upgrade, and tells them nothing about what to type instead.
//
// THE NOTICE GOES TO STDERR AND NOWHERE ELSE. stdout carries the answer: the
// deliverable, the rows, the `--json` object. A deprecation sentence printed
// there would be one line of prose in front of an object a harness is piping
// into `jq`, so the rename would break the very callers it exists to protect.
// That is the reason [renameNotice] is a variable rather than os.Stderr spelled
// inline at each call: a test can read back what was on which stream, which is
// the only way to check a claim about printed text.
var renameNotice io.Writer = os.Stderr

// sayRenamed is the one sentence, said once, for a whole command that moved.
//
// ONE LINE, NOT A PARAGRAPH. A person who typed the old spelling wants to keep
// working; what they are owed is the new word and the fact that this grace ends.
// Everything else about the rename is in `aforge --help` and in the manual.
func sayRenamed(old, now string) {
	fmt.Fprintf(renameNotice, "note: `aforge %s` is now `aforge %s` — the old spelling works for one more release.\n",
		old, now)
}

// ── HIDDEN FLAGS, AND THE TWO KINDS THERE ARE ──────────────────────────────
//
// A hidden flag is one a door still answers to and `--help` does not print.
// There are exactly two reasons to have one and they have different lifetimes,
// so they are different functions and a reader can tell them apart at the call
// site:
//
//   - [renamedFlag] is an OLD SPELLING. It works for one release, it says what
//     it is called now the first time it is used, and then it goes.
//   - [shorthandFlag] is a SINGLE LETTER. `-w`, `-o`, `-j` are what fingers
//     already know; they keep working forever and say nothing, because there is
//     nothing to say — they are not deprecated, they are just not the printed
//     name.
//
// Both are registered as real flags writing through to the printed flag's own
// value, so `--budget 9000` and `--token-budget 9000` are the same assignment
// and [reorder] can ask the flag set whether either takes a value.
//
// THE MARK IS CARRIED IN THE FLAG'S OWN USAGE STRING rather than in a map on
// the side, because a map keyed by *flag.FlagSet is state that outlives the
// door it describes and has to be cleaned up by somebody. A hidden flag's usage
// sentence is never printed — that is what hidden means — so the field is free,
// and a NUL byte cannot collide with anything a person would write.
const (
	hiddenRenamed   = "\x00renamed:"
	hiddenShorthand = "\x00shorthand:"
)

// renamedFlag registers `old` as a hidden alias of the printed flag `now`.
func renamedFlag(flags *flag.FlagSet, old, now string) {
	flags.Var(aliasOf(flags, now), old, hiddenRenamed+now)
}

// shorthandFlag registers a single letter as a permanent hidden alias.
func shorthandFlag(flags *flag.FlagSet, letter, long string) {
	flags.Var(aliasOf(flags, long), letter, hiddenShorthand+long)
}

// aliasOf is the value that writes through to the printed flag.
//
// It delegates IsBoolFlag as well as Set, so a hidden alias of a boolean is
// still a boolean and `--contracts` does not swallow the positional after it.
func aliasOf(flags *flag.FlagSet, name string) flag.Value {
	target := flags.Lookup(name)
	if target == nil {
		// A door aliasing a flag it never declared is a programming mistake and
		// not a person's, so it fails loudly at the door rather than quietly at
		// the parse.
		panic("aforge: aliased the undeclared flag --" + name)
	}
	return &aliasValue{target: target.Value}
}

type aliasValue struct{ target flag.Value }

func (a *aliasValue) String() string {
	if a == nil || a.target == nil {
		return ""
	}
	return a.target.String()
}

func (a *aliasValue) Set(text string) error { return a.target.Set(text) }

func (a *aliasValue) IsBoolFlag() bool {
	boolean, ok := a.target.(interface{ IsBoolFlag() bool })
	return ok && boolean.IsBoolFlag()
}

// hiddenFlag reports whether a flag is one `--help` does not print, and what it
// is the alias of.
func hiddenFlag(f *flag.Flag) (now string, renamed, hidden bool) {
	switch {
	case strings.HasPrefix(f.Usage, hiddenRenamed):
		return strings.TrimPrefix(f.Usage, hiddenRenamed), true, true
	case strings.HasPrefix(f.Usage, hiddenShorthand):
		return strings.TrimPrefix(f.Usage, hiddenShorthand), false, true
	}
	return "", false, false
}

// noteRenamedFlags says, once per old spelling actually typed, what it is called
// now. A shorthand says nothing: it is not going away.
//
// It is called by every door immediately after its parse, before a single line
// of the run's own output, so the notice cannot land in the middle of an answer.
func noteRenamedFlags(flags *flag.FlagSet) {
	flags.Visit(func(f *flag.Flag) {
		now, renamed, hidden := hiddenFlag(f)
		if !hidden || !renamed {
			return
		}
		fmt.Fprintf(renameNotice, "note: `%s` is now `%s` — the old spelling works for one more release.\n",
			dashed(f.Name), dashed(now))
	})
}

// dashed spells a flag the way the usage table spells it: two dashes for a word
// and one for a letter.
func dashed(name string) string {
	if len(name) == 1 {
		return "-" + name
	}
	return "--" + name
}

// typedFlags is which flags this invocation actually named, WITH EVERY HIDDEN
// ALIAS RESOLVED TO THE PRINTED SPELLING.
//
// It exists for one law: a flag that was typed always wins over an environment
// variable that stands in for it. Reading flag.Visit directly would report
// `budget` where a person typed the old spelling and `token-budget` where they
// typed the new one, so AFORGE_EXEC_BUDGET would silently overrule half the
// callers — which is exactly the failure the environment fallbacks are guarded
// against in the first place.
func typedFlags(flags *flag.FlagSet) map[string]bool {
	typed := map[string]bool{}
	flags.Visit(func(f *flag.Flag) {
		typed[f.Name] = true
		if now, _, hidden := hiddenFlag(f); hidden {
			typed[now] = true
		}
	})
	return typed
}

// invertedFlag is an old boolean whose sense is the opposite of the printed
// one's — `--contracts` (default on) against `--no-method` (default off).
//
// It cannot be an ordinary [aliasValue], because `--contracts=false` and
// `--no-method` are the same instruction spelled with opposite words, and an
// alias that passed the text through would turn one into its own negation.
type invertedFlag struct{ off *bool }

func (f *invertedFlag) String() string {
	if f == nil || f.off == nil {
		return "true"
	}
	return strconv.FormatBool(!*f.off)
}

func (f *invertedFlag) Set(text string) error {
	on, err := strconv.ParseBool(strings.TrimSpace(text))
	if err != nil {
		return fmt.Errorf("true or false")
	}
	*f.off = !on
	return nil
}

func (f *invertedFlag) IsBoolFlag() bool { return true }
