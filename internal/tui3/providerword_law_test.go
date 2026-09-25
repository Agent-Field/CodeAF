package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// THE PERSON-FACING WORD FOR THE THING BEHIND A MODEL IS `provider`.
//
// One model id is served by many machines, and until 2026-09-14 this surface
// called one of them a **lane**: the settings row was labelled `lane`, the
// picker's hint said `→ lanes`, the fold's empty line said `no machine has been
// measured`, and the manual's page was titled Lanes. The wire, OpenRouter's own
// refusals (`provider.only`) and the owner all said **provider**, so a person
// met two words for one thing and only one of them was ever written down
// anywhere they could check. The owner ruled on the word (issue #1023): it is
// `provider`, everywhere a person reads.
//
// THE RULING IS ABOUT VOCABULARY AND NOT ABOUT ARCHITECTURE, which is why this
// law reads STRING LITERALS and nothing else. `internal/lane`, `LanePin`,
// `laneAutoSaid`, `lanes.go` and the `lane.talk` settings key are all still
// spelled the old way on purpose — a name a person never reads costs nothing to
// leave alone, and renaming a key on disk would move somebody's pin. What a
// person READS is what moved, and a literal is the only place this surface can
// say a word to them.
//
// It is a law rather than a review note for the reason the icon sweep is one: a
// word reintroduced in one new hint is invisible in a diff and permanent on the
// screen, and the two spellings are exactly what the ruling exists to end.

// laneWordLaw is the standalone word in either number. It is a word boundary on
// both sides so that `lane.talk`, `lanes.json` and `internal/lane` — the three
// machinery spellings that are deliberately unchanged — are not swept up by a
// substring match, and it is case-insensitive because a sentence that opens
// with the word says it just as loudly.
var laneWordLaw = regexp.MustCompile(`(?i)\blanes?\b`)

// laneWordAllowed is every string literal in this package that may still carry
// the old word, each one with the reason it is not a person's word.
//
// `lane` in statusdeck.go is the KEY `/status --json` prints, beside `model`
// and `served`. It is a machinery name like `lane.talk` — a script that reads it
// was promised a stable spelling — so it stays, and the manual's own account of
// `/status` says the row keeps the old word and why. Everything else here is an
// import path, which is a package name and not a sentence.
// laneWordAllowed keeps only the /status --json key; see retiredWordAllowed.
var laneWordAllowed = map[string]string{}

// THE SECOND AND THIRD LAWS: one word for the thing you connect and hold a key
// for (provider), one word for the machine that served one answer (host).
// Issue #1508 found the same thing called service, model service, connection,
// custom connection, active connection and the `models` group head — and the
// routing word `provider` sitting on the same tab. The service and connection
// words are banned outright here and every literal that may still carry them is
// named below with the OTHER meaning it has; the host law bans the routing
// phrases (`→ providers`, `tab providers`, `provider · `, `all providers slow`,
// the measured-nothing line) rather than the bare word, because the word
// `provider` in its own right is the law of the first paragraph.

// serviceWordLaw and connectionWordLaw are the standalone words in either
// number, word-bounded on both sides and case-insensitive.
var serviceWordLaw = regexp.MustCompile(`(?i)\bservices?\b`)
var connectionWordLaw = regexp.MustCompile(`(?i)\bconnections?\b`)

// providerHostLaw catches `provider` in the ROUTING meaning, the meaning issue
// #1508 moves to `host`: the hints, the row prefixes, the fold empty line and
// the slow-all line. A literal that only names the thing you connect does not
// match any of these shapes.
var providerHostLaw = regexp.MustCompile(`(?i)(→ ?providers?\b|tab providers?\b|providers? · |all providers slow|no providers? has been measured|served by providers?\b)`)

// retiredWordAllowed is every string literal that may still carry a retired
// word, each with the OTHER meaning that keeps it: an account (Slack, Google, a
// tool server), a long-running process (`codeaf services`), the ssh or session
// wire, a machinery identifier a script reads, or a demo not on this surface.
var retiredWordAllowed = []struct{ text, where string }{
	// machinery: ids and prefixes a person never reads as a sentence
	{"model-service:", "modelservices.go"},
	{"new-custom-connection", "modelservices.go"},
	{"switch-connection", "modelservices.go"},
	{"connections", "commands.go"},
	{"Connections", "connectcaps.go"},
	{"connection", "statusdeck.go"},
	{"lane", "statusdeck.go"},
	{"lost the connection", "taskending.go"},
	{"each of the five can be pinned on its own in /settings → Providers", "crew.go"},
	// the account connect flow: the ACT of connecting, not the thing
	{" connection didn't complete", "connect.go"},
	{" connection didn't complete", "connectcaps.go"},
	{"openrouter did not start a browser connection", "firstrun.go"},
	{"openrouter connection cancelled · enter tries again or paste a key", "firstrun.go"},
	// the session wire, not a provider
	{"this connection cannot carry a file · the words were not sent", "attach.go"},
	{"a connection holds one conversation at a time", "keeper.go"},
	{"this connection cannot replace a pending request", "questionconversation.go"},
	{"connections are unavailable here", "connectpanel.go"},
	// ssh, in Session settings
	{"seconds an ssh connection stays reusable after it closes, so a quick ", "settings.go"},
	{"how many unanswered heartbeats end a dead connection — three with the ", "settings.go"},
	// a long-running process and a demo document, not a model source
	{"Rows already carry a foreign key into it and the migration is one file.\n+ one place to back up\n- another service to run locally", "questiondemo.go"},
	{"the ledger and the rest of the project share one connection", "questiondemo.go"},
	// a team or host connection, not a model source (dev commits of Sep 2026)
	{". changing them is not available over this connection.", "host.go"},
	{"changing them is not available over this connection", "settings.go"},
	{" the teams inherit that machine's Settings · changing them is not available over this connection", "settings.go"},
	{"Wrap up first is not offered over this connection: Close now, or Cancel", "teamclose.go"},
	{"delete is not available over this connection", "teamclose.go"},
	{"the inbox and the spend are not available over this connection", "teamspage.go"},
	{"its closing report is kept where the team ran, and is not readable over this connection", "teamspagedraw.go"},
	// the crew provider list (daily caps, price ceilings), not the routing tab
	{" · providers · ", "crewpanel.go"},
	{"walk the models row · the providers · a model's routes", "crewpanel.go"},
}

// TestNoPersonFacingStringInThisSurfaceSaysLane walks every string literal this
// package declares and refuses the retired word.
//
// IT READS THE TREE AND NOT A RENDERED FRAME, which is what makes it a law: a
// hint that is only drawn at one width, a settings `about` nobody opened in a
// test, and a note posted from a branch no fixture reaches are all equally
// visible here, and all three are places the word lived.
func TestNoPersonFacingStringInThisSurfaceSaysLane(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("where am I: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(root, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		// THE IMPORT BLOCK IS NOT A SENTENCE. `internal/lane` is the package
		// this surface asks its questions of, and its path is a literal like any
		// other, so it is cut out by shape rather than by an exception nobody
		// would remember to keep true.
		imports := map[ast.Node]bool{}
		for _, spec := range file.Imports {
			imports[spec.Path] = true
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || imports[lit] {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, kept := range retiredWordAllowed {
				if kept.text == text && kept.where == name {
					return true
				}
			}
			switch {
			case laneWordLaw.MatchString(text):
				t.Errorf("%s:%d says %q — the person-facing word for the machine behind a model is `provider` (issue #1023)",
					name, fset.Position(lit.Pos()).Line, text)
			case serviceWordLaw.MatchString(text):
				t.Errorf("%s:%d says %q — the person-facing word for the thing you connect is `provider`, not `service` (issue #1508)",
					name, fset.Position(lit.Pos()).Line, text)
			case connectionWordLaw.MatchString(text):
				t.Errorf("%s:%d says %q — the person-facing word for the thing you connect is `provider`, not `connection` (issue #1508)",
					name, fset.Position(lit.Pos()).Line, text)
			case providerHostLaw.MatchString(text):
				t.Errorf("%s:%d says %q — the person-facing word for the machine that served one answer is `host` (issue #1508)",
					name, fset.Position(lit.Pos()).Line, text)
			}
			return true
		})
	}
}
