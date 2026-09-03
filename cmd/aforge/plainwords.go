package main

import (
	"fmt"
	"strings"
)

// NO GO ERROR CHAIN REACHES A PERSON.
//
// An error inside this binary is wrapped on every floor it passes, which is
// right for a stack trace and wrong for a sentence somebody reads. The worked
// example is the one that cost this file: a run against a model id that does
// not exist printed
//
//	I couldn't apply that request: splice failed: compile request: compile
//	intent: API error (400): nosuch/model-xyz is not a valid model ID
//
// as the deliverable — three internal package verbs in front of the one fact
// that matters, and nothing at all about what to do next. Every message a
// person reads must carry THE CAUSE and WHAT TO DO, and may not leak a Go type,
// a wrapped chain, a package name or a path from inside the binary.

// machineryVerbs are the wrapping verbs the packages inside this binary put in
// front of a cause.
//
// They are NAMED ONE BY ONE and matched WHOLE, deliberately not guessed at by
// shape: a rule that dropped every short lower-case segment would also drop
// `open /nope/graph.json` and `splice parent "task-1" is closed`, which are the
// only parts of those chains a person can act on.
var machineryVerbs = []string{
	"splice failed",
	"materialize splice",
	"compile request",
	"compile intent",
}

// plainWords is the whole rule, applied to one sentence: drop the machinery
// verbs wherever they stand in the chain, keep everything a person could act
// on, and add the remedy when the fact is one aforge recognises.
func plainWords(sentence string) string {
	// A DISK FAULT IS ANSWERED FIRST AND WHOLE, because it is the one kind of
	// chain where the tail already carries every fact and the whole front is
	// the binary narrating what it was doing (see [filesystemFault]).
	if fault, ok := filesystemFault(sentence); ok {
		return fault.words()
	}
	fact := terminalCause(sentence)
	if remedy := remedyFor(fact); remedy != "" {
		return fact + "\n" + remedy
	}
	return fact
}

// ── THE OPERATING SYSTEM'S WORDS ARE NOT AFORGE'S ────────────────────────────
//
// A filesystem failure arrives as an [os.PathError] wrapped on every floor it
// passed on the way up:
//
//	open notebook: stat /home/x/.aforge/graph.db: no such file or directory
//	create chat workspace: mkdir /nope: permission denied
//
// `stat`, `mkdir` and `open` are the names of system calls — a library's words
// written for another library — the path is the only fact in the whole line,
// and nothing anywhere says what to do about it.
//
// THE RULE IS STRUCTURAL AND NOT A LIST OF SITES. A chain that ENDS in a
// syscall fault is a chain whose front is the binary saying what it was doing
// when the disk said no; the tail names the path itself, so the front carries
// nothing a person can act on and is dropped whole. That is why this needs no
// per-door decision and no per-door edit: every command in this binary reports
// through one line (main.go), so the doors that spelled `open notebook`, `open
// receipts`, `open store` and `create chat workspace` are all answered here
// without any of them being touched.
//
// What is deliberately NOT claimed is which flag named the path. Six doors take
// a path under four different flags, and a sentence that guessed `--dir` at a
// door whose flag is `--db` would send somebody to the wrong knob with
// confidence — the same reason [remedies] is short.

// syscallVerbs are the operating-system operations Go puts in front of a path.
// They are named one by one, like [machineryVerbs] and for the same reason: a
// rule that took any lower-case word before a path would swallow `splice parent
// "task-1"` and half the sentences a person can actually act on.
var syscallVerbs = []string{
	"open", "openat", "stat", "lstat", "fstat", "mkdir", "mkdirat",
	"read", "readdir", "readlink", "write", "remove", "removeall",
	"rename", "chdir", "chmod", "chown", "unlink", "symlink", "link", "truncate",
}

// creatingVerbs are the ones that were trying to BRING SOMETHING INTO BEING.
// The distinction earns its keep twice: a missing path under `mkdir` means the
// folder ABOVE it is missing, which is a different remedy, and "cannot be
// created" is a truer sentence than "was refused" when a folder is what was
// wanted.
var creatingVerbs = map[string]bool{
	"mkdir": true, "mkdirat": true, "create": true, "write": true,
	"rename": true, "symlink": true, "link": true, "truncate": true,
}

// fsFault is one filesystem failure taken apart: what was being done, to what,
// and what the operating system said about it.
type fsFault struct {
	verb   string
	path   string
	reason string
}

// filesystemFault reads the tail of a wrapped chain and says whether it is a
// disk fault. It looks at the LAST TWO segments only — `<verb> <path>` and the
// reason — because that pair is exactly what [os.PathError] formats itself as,
// and anything in front of it is this binary's own narration.
func filesystemFault(sentence string) (fsFault, bool) {
	segments := strings.Split(strings.TrimSpace(sentence), ": ")
	if len(segments) < 2 {
		return fsFault{}, false
	}
	reason := strings.ToLower(strings.TrimSpace(segments[len(segments)-1]))
	if _, known := filesystemSentences[reason]; !known {
		return fsFault{}, false
	}
	verb, path, cut := strings.Cut(strings.TrimSpace(segments[len(segments)-2]), " ")
	if !cut || strings.TrimSpace(path) == "" {
		return fsFault{}, false
	}
	verb = strings.ToLower(verb)
	for _, known := range syscallVerbs {
		if verb == known {
			return fsFault{verb: verb, path: strings.TrimSpace(path), reason: reason}, true
		}
	}
	return fsFault{}, false
}

// filesystemSentences is what aforge says about each thing the operating system
// can say, and what to do about it. The `created` half is the sentence when the
// verb was trying to make something ([creatingVerbs]); `found` is the sentence
// when it was only trying to look.
//
// EVERY ONE OF THEM IS A CAUSE AND A REMEDY, which is the whole law this file
// exists for: somebody reading a failure wants to know what to type next.
var filesystemSentences = map[string]struct{ found, created, remedy string }{
	"no such file or directory": {
		found:   "there is nothing at %s.",
		created: "%s could not be created — the folder above it does not exist.",
		remedy:  "check the path, and make the folder above it first if it is meant to be new.",
	},
	"permission denied": {
		found:   "%s cannot be opened by this account.",
		created: "%s could not be created — this account is not allowed to write there.",
		remedy:  "choose a path you own, such as one under your home directory.",
	},
	"not a directory": {
		found:   "%s is a file, and a folder was needed there.",
		created: "%s could not be created — something on the way to it is a file, not a folder.",
		remedy:  "point it at a folder instead.",
	},
	"is a directory": {
		found:   "%s is a folder, and a file was needed there.",
		created: "%s could not be written — it is a folder, not a file.",
		remedy:  "point it at a file instead.",
	},
	"file exists": {
		found:   "%s is already there.",
		created: "%s is already there.",
		remedy:  "remove it, or choose another path.",
	},
	"no space left on device": {
		found:   "the disk holding %s is full.",
		created: "the disk holding %s is full.",
		remedy:  "free some space and run it again.",
	},
	"read-only file system": {
		found:   "%s is on a disk that cannot be written to.",
		created: "%s could not be created — it is on a disk that cannot be written to.",
		remedy:  "choose a path you own, such as one under your home directory.",
	},
	"too many levels of symbolic links": {
		found:   "%s points at itself through a chain of links.",
		created: "%s points at itself through a chain of links.",
		remedy:  "point it at a real file or folder.",
	},
}

// words is the fault as a person reads it: the cause on one line, what to do on
// the next, and not one syscall name or wrapping verb between them.
func (f fsFault) words() string {
	said := filesystemSentences[f.reason]
	shape := said.found
	if creatingVerbs[f.verb] {
		shape = said.created
	}
	return fmt.Sprintf(shape, f.path) + "\n" + said.remedy
}

// terminalCause is the same sentence with the machinery taken out of it.
//
// It removes verbs from ANYWHERE in the chain rather than only from the front,
// because the front is usually the one segment written for a person — "I
// couldn't apply that request" — and the machinery hides between that and the
// fact.
func terminalCause(sentence string) string {
	trimmed := strings.TrimSpace(sentence)
	segments := strings.Split(trimmed, ": ")
	kept := make([]string, 0, len(segments))
	for _, segment := range segments {
		if isMachinery(segment) {
			continue
		}
		kept = append(kept, segment)
	}
	// A chain that is machinery all the way down is still better said than
	// swallowed: nothing at all is the one answer a person cannot work with.
	if len(kept) == 0 {
		return trimmed
	}
	return strings.Join(kept, ": ")
}

func isMachinery(segment string) bool {
	lowered := strings.ToLower(strings.TrimSpace(segment))
	for _, verb := range machineryVerbs {
		if lowered == verb {
			return true
		}
	}
	// The provider's transport verb is the one that carries a number with it —
	// `API error (400)`. Everything after it is the provider's own sentence,
	// which is the fact; the status code is not something a person acts on.
	return strings.HasPrefix(lowered, "api error")
}

// remedies are the failures aforge can name in a person's own words and say
// what to do about. The match is on the fact the provider or the store left
// behind, lower-cased, and the remedy is one line: the gesture that fixes it.
//
// The list is short on purpose. A remedy that is a guess is worse than none —
// it sends somebody to the wrong place with confidence — so a cause that is not
// here is printed as it is and nothing is invented under it.
var remedies = []struct {
	fact   string
	remedy string
}{
	{
		fact:   "is not a valid model id",
		remedy: "run `aforge models` to see the ids this key can reach, or pass --model with one of them.",
	},
	{
		fact:   "no auth credentials found",
		remedy: "export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.",
	},
	{
		// The most common first-run failure there is ([config.ErrNoAPIKey]).
		fact:   "openrouter_api_key (or openai_api_key) is required",
		remedy: "export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.",
	},
	{
		fact:   "insufficient credits",
		remedy: "that key is out of credit at the provider — top it up, or point AFORGE_MODEL at a model it can still reach.",
	},
}

func remedyFor(fact string) string {
	lowered := strings.ToLower(fact)
	for _, known := range remedies {
		if strings.Contains(lowered, known.fact) {
			return known.remedy
		}
	}
	return ""
}
