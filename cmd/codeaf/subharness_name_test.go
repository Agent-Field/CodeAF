package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── C17: A BAD PROGRAM NAME WAS REPORTED AS AN EMPTY INPUT ───────────────────
//
// `aforge run nosuchharness --input -` answered
//
//	the input is empty — there is nothing here for the run to do
//
// and never mentioned the name. Two things were wrong and the message named the
// one the person had NOT got wrong, so they went away and fixed their input.
func TestABadProgramNameIsSaidBeforeTheInputIsBlamed(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	// An input that is ALSO wrong, so the two refusals are genuinely in a race
	// and the test is about which one answers. A file rather than a pipe,
	// because os.Stdin is not something the door takes from a caller.
	empty := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	err := runSubharnessCommand([]string{"nosuchharness", "--input", empty})
	if err == nil {
		t.Fatal("a program that does not exist ran anyway")
	}
	said := err.Error()
	if !strings.Contains(said, "nosuchharness") {
		t.Fatalf("the refusal never names what the person typed.\n  said: %q\n"+
			"  want: it to name the program `nosuchharness`", said)
	}
	if strings.Contains(said, "the input is empty") {
		t.Fatalf("the refusal blames the input, so the reader fixes the wrong thing.\n"+
			"  said: %q\n  want: a sentence about the program name", said)
	}
}

// A GOOD NAME IS NOT REFUSED, and this is the half that makes the check safe to
// have at all: the reading admits a program that is really there and hands the
// run on, so the input's own refusal — which is the right answer once the name
// is right — still arrives.
func TestAProgramThatIsReallyThereGetsPastTheNameCheck(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AFORGE_HOME", root)
	// One bundle, laid out the way substore lays them out: a named directory
	// holding a complete version. The listing is a directory read, so this is
	// the whole of what it takes to be a program a person can name.
	bundle := filepath.Join(root, "subharnesses", "realprogram")
	if err := os.MkdirAll(filepath.Join(bundle, "v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "v1.json"), []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := checkSubharnessName("realprogram", ""); err != nil {
		t.Fatalf("a program that is in the store was refused at the door: %v", err)
	}
	// And the name the deopt path resolves is admitted even though it is on no
	// list a person reads (registerSubharnessRunners).
	if err := checkSubharnessName("linear", ""); err != nil {
		t.Fatalf("the generalist was refused at the door: %v", err)
	}
	// While a neighbour of the real name is not.
	if err := checkSubharnessName("realprogramm", ""); err == nil {
		t.Fatal("a misspelling of a real program was admitted")
	}
}

// THE DOOR AND THE RUN SAY IT THE SAME WAY. There are two readings of one
// question — a cheap one at the door and the registry's own inside the run —
// and a person who hit them at different moments must not meet two sentences.
func TestBothReadingsRefuseAMissingProgramInTheSameWords(t *testing.T) {
	atTheDoor := noSuchSubharnessNamed("nosuch", []string{"alpha", "beta"})
	if !strings.Contains(atTheDoor.Error(), `there is no subharness called "nosuch"`) {
		t.Fatalf("the door's refusal changed shape: %q", atTheDoor.Error())
	}
	if !strings.Contains(atTheDoor.Error(), "alpha, beta") {
		t.Fatalf("the refusal does not say what this build DOES have: %q", atTheDoor.Error())
	}
	// A build with nothing to offer says that, rather than trailing an empty
	// list a person would read as a truncated message.
	bare := noSuchSubharnessNamed("nosuch", nil)
	if !strings.Contains(bare.Error(), "none to offer") {
		t.Fatalf("a build with no programs printed an empty offer: %q", bare.Error())
	}
}

// AND THE EARLY READING MUST KNOW ABOUT EVERY STORE THE RUN REGISTERS.
//
// The door lists the person's own bundles and the project's; the run registers
// exactly those two through [exec.Registry.UseBundles]. The seam comment in
// subharness_run.go anticipates a third — the packed trailer — and the day it
// lands, an early reading that had not heard of it would refuse a program that
// exists. This is structural rather than a grep so it cannot be satisfied by a
// comment quoting the call.
func TestTheEarlyNameCheckReadsEveryBundleStoreTheRunRegisters(t *testing.T) {
	const checked = 2

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "subharness_run.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	registered := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "UseBundles" {
			return true
		}
		registered++
		return true
	})
	if registered != checked {
		t.Fatalf("the run registers %d bundle sources and the door's early name check reads %d.\n"+
			"  A source the check has not heard of makes it refuse a program that exists.\n"+
			"  Teach checkSubharnessName the new store, then raise the count here.",
			registered, checked)
	}
}
