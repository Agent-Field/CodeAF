package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// THE STANDING GATE ON "WHO IS THIS ADDRESSED TO".
//
// The failure this whole road exists to close is not a bug in a function; it is
// a SENTENCE ADDRESSED TO SOMEBODY WHO IS NOT THERE. Nothing in a compiler
// notices that, and nothing in a review reliably notices it either — the
// sentence reads perfectly, and it reads perfectly on the one kind of session
// where it is a dead end. So the two facts a reviewer would have to hold in
// their head are held here instead, and a build fails when either moves.
//
// The two tests below are deliberately dumb: one counts the doors, the other
// counts the sentences. Neither tries to decide whether a road is CORRECT, and
// neither could; what they enforce is that a road cannot appear, or a
// person-addressed sentence be written, without somebody having said in this
// file where the principal comes into it.

// wakeRoads are every function in this package that may put a message on the
// steering queue the model OWES AN ANSWER FOR — the wake, which is the only
// mechanism by which this engine says anything to whoever is out there when no
// turn is running.
//
// EACH ENTRY SAYS HOW THAT ROAD REACHES THE PRINCIPAL. Adding one without an
// entry fails this test, and the entry is the place the author has to answer
// the question in words. "It does not need to" is a legitimate answer and two
// of these give it.
var wakeRoads = map[string]string{
	"wakeNote": "the constructor itself; it addresses nobody and carries no words of its own",
	"jobNote": "the registry's background-job ending; the model started this work and its tool " +
		"contract promised the ending would return instead of being polled",
	"enqueueWatchNote": "a watch FIRING, which is the last thing that watch will ever say. " +
		"It is addressed the way [jobNote] is: the model started the watch, the tool's contract " +
		"is that the news comes back instead of being polled, and a watch that has matched, gone " +
		"quiet or failed its way out has nothing left to poll. Its periodic ticks are not a wake " +
		"at all and ride the ambient lane",
	"steerNote": "the constructor itself; a line the PERSON typed into a running node, " +
		"which is the one wake that is already a person's own sentence",
	"enqueueSteering": "the generic lane. Its callers own the addressing: a landing goes through " +
		"[Agent.deliverTaskNote] below, a standing firing is the person's own armed words, " +
		"a fork's hand reports to the model that forked it",
	"spoken": "the person's own line, said into a node (task_room.go). The other speaker on " +
		"this road is another agent in this session, and it is addressed to the node's own " +
		"runner rather than to anybody out there ([relayNote])",
	"relayNote": "the model's `tasks … say` into a running node. It wakes THAT NODE'S RUNNER " +
		"and nobody else — a worker's turns are its runner's to start ([Agent.wakeLocked] " +
		"declines inside a task) — so it never reaches the wake a person would have to be " +
		"there for, and it is marked as the session's own words rather than as theirs",
	"postTaskMessage": "THE ONE THAT MATTERED, and the seam both roads into a landed node's " +
		"news take ([Agent.deliverTaskNote] announces the ending, " +
		"[Agent.bubbleUnverifiedChildren] re-addresses what is still owed). The note is built " +
		"by [taskNote] out of a [landingAddress], which is what the session's goal owner " +
		"answered about that landing ([Agent.addressLanding] → [Principal.Report])",
}

// TestEveryWakeRoadSaysWhoItIsAddressedTo fails when a new road into the wake
// appears without an entry above.
func TestEveryWakeRoadSaysWhoItIsAddressedTo(t *testing.T) {
	found := map[string]bool{}
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		var enclosing string
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:
				enclosing = typed.Name.Name
			case *ast.CallExpr:
				name, ok := typed.Fun.(*ast.Ident)
				if !ok || (name.Name != "wakeNote" && name.Name != "steerNote") {
					return true
				}
				if _, known := wakeRoads[enclosing]; !known {
					t.Errorf("%s:%d: %s puts a wake on the queue and principal_structure_test.go's "+
						"wakeRoads does not say who it is addressed to.\n"+
						"A wake is what this engine says when no turn is running, and on an unattended "+
						"session there is nobody to say it to. Route it through the session's principal "+
						"(principal.go) and then add the entry saying how.",
						filepath.Base(path), fset.Position(name.Pos()).Line, enclosing)
					return true
				}
				found[enclosing] = true
			}
			return true
		})
	})
	// AND AN ENTRY FOR A ROAD THAT NO LONGER EXISTS IS A LIE THAT ROTS. The two
	// constructors are found by their own declarations; every other entry has to
	// be reached by a call.
	for road := range wakeRoads {
		if road == "wakeNote" || road == "steerNote" {
			continue
		}
		if !found[road] {
			t.Errorf("wakeRoads names %q and nothing in this package puts a wake on the queue there any more — remove the entry", road)
		}
	}
}

// personAddressedSentences are the sentences this package writes that only make
// sense to a human being who is reading them.
//
// EACH ONE NAMES THE FILE IT MAY LIVE IN, and that file must be one that knows
// about the principal. The point is not the file: it is that a sentence like
// "offer them a follow-up in their own words" cannot be written anywhere in this
// package without landing next to the machinery that decides whether there is a
// them.
//
// The exact strings are here rather than referenced as constants deliberately.
// A test that read the constant would pass whatever the constant said; this one
// fails when the sentence is rewritten, which is the moment to ask again who is
// reading it.
var personAddressedSentences = map[string]string{
	"offer them a follow-up in their own words before anything else is spent on it": "principal_wire.go",
	"the person can also answer this on the card in front of them":                  "task_run.go",
	"nobody is here to say yes — this can only be set up in a conversation":         "tools_standing.go",
}

// TestEveryPersonAddressedSentenceSitsBesideThePrincipal fails when one of them
// is written somewhere that has never heard of a principal, and when one of them
// stops existing at all.
func TestEveryPersonAddressedSentenceSitsBesideThePrincipal(t *testing.T) {
	seen := map[string]bool{}
	forEachPackageFile(t, func(path string, _ *ast.File, _ *token.FileSet) {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		text := string(content)
		base := filepath.Base(path)
		for sentence, home := range personAddressedSentences {
			if !strings.Contains(text, strconv.Quote(sentence)) && !strings.Contains(text, sentence) {
				continue
			}
			seen[sentence] = true
			if base != home {
				t.Errorf("%s writes a sentence addressed to a person that belongs in %s:\n  %q\n"+
					"Whether there is a person to address is [Principal]'s question (principal.go); "+
					"move the sentence to where that is decided, or say so in personAddressedSentences.",
					base, home, sentence)
			}
			if !mentionsPrincipal(text) {
				t.Errorf("%s writes a sentence addressed to a person and never consults the session's "+
					"principal:\n  %q", base, sentence)
			}
		}
	})
	var missing []string
	for sentence := range personAddressedSentences {
		if !seen[sentence] {
			missing = append(missing, sentence)
		}
	}
	sort.Strings(missing)
	for _, sentence := range missing {
		t.Errorf("personAddressedSentences names a sentence this package no longer writes — "+
			"check who the new wording is addressed to, then update the table:\n  %q", sentence)
	}
}

// mentionsPrincipal is the weakest honest reading of "this file knows there is a
// question about who is being addressed": it names the interface, one of its two
// implementations, or the seam a landing note is addressed through.
func mentionsPrincipal(text string) bool {
	for _, word := range []string{"Principal", "Steward", "landingAddress", "a.who()", "a.steward()"} {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

// forEachPackageFile walks this package's own non-test sources. The floor
// guards against a walk that quietly stops finding anything — a moved package,
// a bad root — which would turn both tests above into two that always pass.
func forEachPackageFile(t *testing.T, visit func(path string, file *ast.File, fset *token.FileSet)) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	fset := token.NewFileSet()
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		scanned++
		visit(name, parsed, fset)
	}
	if scanned < 100 {
		t.Fatalf("only %d files were scanned; this package holds far more, so the walk is broken rather than clean", scanned)
	}
}
