package main

import (
	"strings"
	"testing"
)

// NO GO ERROR CHAIN REACHES A PERSON.
//
// The worked example is the one this rule was written from: a run against a
// model id that does not exist printed, as its deliverable,
//
//	I couldn't apply that request: splice failed: compile request: compile
//	intent: API error (400): nosuch/model-xyz is not a valid model ID
//
// Three internal package verbs stand in front of the one fact that matters, and
// nothing at all says what to do about it.
func TestNoGoErrorChainReachesAPerson(t *testing.T) {
	chain := "I couldn't apply that request: splice failed: compile request: compile intent: " +
		"API error (400): nosuch/model-xyz is not a valid model ID"
	said := plainWords(chain)

	for _, machinery := range []string{"splice failed", "compile request", "compile intent", "API error"} {
		if strings.Contains(said, machinery) {
			t.Fatalf("the reader is still shown the internal verb %q:\n%s", machinery, said)
		}
	}
	if !strings.Contains(said, "nosuch/model-xyz is not a valid model ID") {
		t.Fatalf("the one fact a person can act on was lost:\n%s", said)
	}
	if !strings.Contains(said, "aforge models") || !strings.Contains(said, "--model") {
		t.Fatalf("the message says what went wrong and never says what to do about it:\n%s", said)
	}
	if !strings.Contains(said, "I couldn't apply that request") {
		t.Fatalf("the sentence written for a person was thrown away with the machinery:\n%s", said)
	}
}

// A cause aforge does not recognise is printed as it is. A remedy that is a
// guess is worse than none: it sends somebody to the wrong place with
// confidence.
func TestAnUnrecognisedCauseIsSaidPlainlyAndNothingIsInvented(t *testing.T) {
	said := plainWords("compile request: the planner is unreachable")
	if said != "the planner is unreachable" {
		t.Fatalf("plain words = %q, want %q", said, "the planner is unreachable")
	}
	if strings.Contains(said, "\n") {
		t.Fatalf("a remedy was invented for a cause nothing knows what to do about:\n%s", said)
	}
}

// The parts of a chain that name something a person can act on are KEPT. A rule
// that took only the last segment would answer "no such file or directory" and
// throw away the path that says which file.
func TestPlainWordsKeepsWhatAPersonCanActOn(t *testing.T) {
	said := plainWords("open /nope/graph.json: no such file or directory")
	if !strings.Contains(said, "/nope/graph.json") {
		t.Fatalf("the only actionable part of the chain was dropped:\n%s", said)
	}
}

// A chain that is machinery all the way down is still better said than
// swallowed: nothing at all is the one answer a person cannot work with.
func TestAChainOfNothingButVerbsIsStillSaid(t *testing.T) {
	if said := plainWords("splice failed: compile intent"); strings.TrimSpace(said) == "" {
		t.Fatal("a failure made of nothing but machinery was reported as silence")
	}
}

// ── C12: A DISK FAULT IS SAID IN AFORGE'S WORDS, WITH WHAT TO DO ─────────────
//
// The five doors in the audit row spelled four different wrapping verbs in
// front of the same operating-system sentence, and not one of them said what to
// type next:
//
//	open notebook: stat /nope/graph.db: no such file or directory
//	open receipts: stat /nope/graph.db: no such file or directory
//	open store: stat /nope/graph.db: no such file or directory
//	create chat workspace: mkdir /nope: permission denied
//	open /nope/graph.json: no such file or directory
//
// Every one of them arrives here, because every command in this binary reports
// its failure through the one line in main.go.
func TestADiskFaultIsSaidInAforgesWordsAndSaysWhatToDo(t *testing.T) {
	for _, probe := range []struct {
		name  string
		chain string
		path  string
		cause string
	}{
		{
			name:  "the notebook's own wrapping",
			chain: "open notebook: stat /nope/graph.db: no such file or directory",
			path:  "/nope/graph.db",
			cause: "there is nothing at /nope/graph.db.",
		},
		{
			name:  "the receipts reader's wrapping",
			chain: "open receipts: stat /nope/graph.db: no such file or directory",
			path:  "/nope/graph.db",
			cause: "there is nothing at /nope/graph.db.",
		},
		{
			name:  "a plan file nobody wrapped at all",
			chain: "open /nope/graph.json: no such file or directory",
			path:  "/nope/graph.json",
			cause: "there is nothing at /nope/graph.json.",
		},
		{
			name:  "a workspace that could not be made",
			chain: "create chat workspace: mkdir /nope: permission denied",
			path:  "/nope",
			cause: "/nope could not be created — this account is not allowed to write there.",
		},
		{
			name:  "the same fault with no wrapping in front of it",
			chain: "mkdir /nope: permission denied",
			path:  "/nope",
			cause: "/nope could not be created — this account is not allowed to write there.",
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			said := plainWords(probe.chain)
			// THE OPERATING SYSTEM'S WORDS ARE GONE, and so is every verb the
			// binary put in front of them.
			for _, machinery := range []string{
				"stat ", "mkdir ", "open notebook", "open receipts", "open store",
				"create chat workspace", "no such file or directory", "permission denied",
			} {
				if strings.Contains(said, machinery) {
					t.Fatalf("%q still reaches the reader.\n  chain: %s\n  said:  %q",
						machinery, probe.chain, said)
				}
			}
			cause, remedy, split := strings.Cut(said, "\n")
			if !split {
				t.Fatalf("the sentence is a cause with no remedy under it.\n  chain: %s\n  said:  %q\n"+
					"  want:  %q then a line saying what to do",
					probe.chain, said, probe.cause)
			}
			if cause != probe.cause {
				t.Fatalf("the cause is wrong.\n  chain: %s\n  said:  %q\n  want:  %q",
					probe.chain, cause, probe.cause)
			}
			if !strings.Contains(said, probe.path) {
				t.Fatalf("the one fact a person can act on — the path — was lost.\n"+
					"  chain: %s\n  said:  %q\n  want it to name %s",
					probe.chain, said, probe.path)
			}
			if strings.TrimSpace(remedy) == "" {
				t.Fatalf("the remedy line is empty, so the message says the cause and nothing to do.\n"+
					"  chain: %s\n  said:  %q", probe.chain, said)
			}
		})
	}
}

// A LOOKING VERB AND A MAKING VERB ARE DIFFERENT ANSWERS TO THE SAME REASON.
// `stat /nope/x: no such file or directory` means the thing is not there;
// `mkdir /nope/x: no such file or directory` means the folder ABOVE it is not
// there, and telling somebody to check the path they typed would be telling
// them the wrong thing.
func TestAPathThatCouldNotBeMadeIsNotToldItIsSimplyMissing(t *testing.T) {
	looked := plainWords("stat /nope/x: no such file or directory")
	made := plainWords("mkdir /nope/x: no such file or directory")
	if looked == made {
		t.Fatalf("looking and making got the same sentence, so the difference is not said:\n%s", looked)
	}
	if !strings.HasPrefix(looked, "there is nothing at /nope/x.") {
		t.Fatalf("a path that was only looked at = %q, want it said to be missing", looked)
	}
	if !strings.Contains(made, "the folder above it does not exist") {
		t.Fatalf("a path that could not be made = %q, want it to name the folder above it", made)
	}
}

// AND NOTHING THAT IS NOT A DISK FAULT IS DRESSED AS ONE. The rule reads the
// tail of a chain, so a sentence that merely ENDS in a path, or ends in a word
// the operating system never said, must fall through to the ordinary reading —
// otherwise every refusal in the binary would grow a remedy about folders.
func TestASentenceThatIsNotADiskFaultIsLeftAlone(t *testing.T) {
	for _, sentence := range []string{
		"I couldn't apply that request: splice parent \"task-1\" is closed",
		"could not open /home/x/.aforge/graph.db: permission denied",
		"the planner is unreachable",
		"model /nope/graph.json: was refused",
	} {
		said := plainWords(sentence)
		for _, invented := range []string{"there is nothing at", "could not be created", "check the path"} {
			if strings.Contains(said, invented) {
				t.Fatalf("a sentence that is not a disk fault was answered as one.\n"+
					"  given: %q\n  said:  %q\n  it invented %q", sentence, said, invented)
			}
		}
	}
}
