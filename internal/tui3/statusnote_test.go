package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── 1. the two commands exist, once ─────────────────────────────────────────

// BOTH ARE ON THE LIST AND IN /help, which is one table read twice
// (commands.go), and neither of them brought a word that already meant
// something else.
func TestStatusAndCostAreOnTheCommandList(t *testing.T) {
	help := helpText("", chordSpelling{})
	for _, name := range []string{"status", "cost"} {
		found := false
		for _, c := range commands {
			found = found || c.name == name
		}
		if !found {
			t.Fatalf("/%s is not on the command list", name)
		}
		if !strings.Contains(help, "/"+name) {
			t.Fatalf("/%s is not in /help", name)
		}
	}
	if err := checkCommands(commands); err != nil {
		t.Fatalf("the table stopped being a table: %v", err)
	}
	// The other words for them reach them, and the list is what says so.
	for word, want := range map[string]string{
		"info": "status", "context": "status",
		"usage": "cost", "tokens": "cost",
	} {
		if got := canonicalCommand(word); got != want {
			t.Fatalf("/%s ran as /%s and not /%s", word, got, want)
		}
	}
	// AND `spend` IS NO LONGER ONE OF THEM. It was an alias of /cost, which
	// prints THIS CONVERSATION's bill — so the one word a person guesses for
	// "what has this cost me" landed on a different question from the
	// machine-wide place, and said nothing about it. It is a command of its own
	// now and opens that place (commands.go, app.go's dispatch).
	if got := canonicalCommand("spend"); got != "spend" {
		t.Fatalf("/spend ran as /%s — it must open the spend place, not print one conversation's bill", got)
	}
}

// ── 2. /cost ────────────────────────────────────────────────────────────────

// A SESSION THAT HAS SPENT NOTHING SAYS SO IN WORDS. It does not say "$0.00",
// and it does not go quiet: a command typed on purpose that answers with an
// empty note reads as a command that broke.
func TestCostOnAFreshSessionSaysNothingRatherThanZero(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})

	a.slash("/cost")
	text := lastNote(t, a)
	if !strings.Contains(text, "nothing spent") {
		t.Fatalf("a fresh session answered %q", text)
	}
	for _, banned := range []string{"$0.00", "0 in", "0 out", "0 read"} {
		if strings.Contains(text, banned) {
			t.Fatalf("the answer says %q:\n%s", banned, text)
		}
	}
}

// EVERY FIGURE THE SESSION HAS IS ON A LINE OF ITS OWN, and the one it does not
// have — nobody published a cache accounting — has no line at all.
func TestCostNamesTheFiguresItHasAndNoOthers(t *testing.T) {
	agent := &fakeAgent{model: "m", usage: session.Usage{
		Input:    48_100,
		Output:   3_200,
		CostUSD:  0.42,
		Turns:    9,
		Calls:    14,
		Duration: 3*time.Minute + 12*time.Second,
	}}
	a := newTestApp(agent)

	a.slash("/cost")
	text := lastNote(t, a)
	for _, want := range []string{"$0.42", "48.1k in", "3.2k out", "14", "3m12s"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the answer lost %q:\n%s", want, text)
		}
	}
	// The provider said nothing about a warm prefix, which is not the same fact
	// as a cache that missed — so there is no cache line to read either way.
	if strings.Contains(text, "cache") {
		t.Fatalf("an unpriced, uncached session grew a cache line:\n%s", text)
	}
	// A turn count is a person's word for a person's messages; these are the
	// requests that went to the provider.
	if strings.Contains(text, "turns") {
		t.Fatalf("the answer calls the model's calls turns:\n%s", text)
	}
}

// THE DENOMINATOR IS EVERY REQUEST, NOT EVERY TURN. A session's spend includes
// the calls nobody asked for by name — naming the session, a judge, a picture
// being looked at, every request a task's own agent made — so the count printed
// beside the money has to include them too. The session counts the two
// separately for exactly this reason (session.Usage: Turns keeps its own law,
// Calls is the honest denominator), and this line reads the second.
func TestCostCountsEveryModelCallAndNotJustTheTurns(t *testing.T) {
	agent := &fakeAgent{model: "m", usage: session.Usage{
		Input: 1_000, Output: 200, CostUSD: 0.05,
		// Two turns of the conversation's own, and three more requests behind
		// them that no turn asked for.
		Turns: 2, Calls: 5,
	}}
	a := newTestApp(agent)

	a.slash("/cost")
	text := lastNote(t, a)
	line := ""
	for _, row := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(row), "model calls") {
			line = strings.TrimSpace(row)
		}
	}
	if line == "" {
		t.Fatalf("the answer has no model calls line:\n%s", text)
	}
	if !strings.HasSuffix(line, " 5") {
		t.Fatalf("the model calls line reads %q, want the 5 requests that were made "+
			"and not the 2 turns that asked for some of them", line)
	}
}

func TestCostNamesTheReflexCallsThatSpentTheirCeilingOnNoAnswer(t *testing.T) {
	a := newTestApp(&fakeAgent{usage: session.Usage{
		Input: 2_000, Output: 400, Calls: 4, EmptyReflex: 2,
	}})

	a.slash("/cost")
	text := lastNote(t, a)
	line := ""
	for _, row := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(row), "empty reflex answers") {
			line = strings.TrimSpace(row)
		}
	}
	if line == "" || !strings.HasSuffix(line, " 2") {
		t.Fatalf("the empty reflex line reads %q in:\n%s", line, text)
	}
}

// THE CACHE LINE IS WHAT WAS READ, AND WHAT THAT WAS WORTH — and the money half
// appears only when a price was published while the reads were happening
// (app.go's [app.cacheSaved], which is not derivable afterwards).
func TestCostSaysWhatTheCacheGaveBackOnlyWhenItIsPriced(t *testing.T) {
	agent := &fakeAgent{model: "m", usage: session.Usage{
		Input: 50_000, Output: 1_000, CostUSD: 0.2, CacheRead: 31_200,
	}}
	a := newTestApp(agent)

	a.slash("/cost")
	unpriced := lastNote(t, a)
	if !strings.Contains(unpriced, "31.2k read") {
		t.Fatalf("the answer lost what the cache served:\n%s", unpriced)
	}
	if strings.Contains(unpriced, "saved") {
		t.Fatalf("an unpriced session claimed a saving:\n%s", unpriced)
	}

	a.cacheSaved = 0.018
	a.slash("/cost")
	priced := lastNote(t, a)
	if !strings.Contains(priced, "31.2k read · saved $0.0180") {
		t.Fatalf("the priced answer reads:\n%s", priced)
	}
}

// ── 3. /status ──────────────────────────────────────────────────────────────

// /status IS THE SHEET'S OWN LIST. Every fact the phone's status sheet carries
// is in the note, under the same label, because both are built from
// [app.deckItems] — a row on one and not the other would make "what does this
// session say about itself" a question with two answers (statusdeck.go).
func TestStatusPrintsTheSheetsOwnList(t *testing.T) {
	agent := &fakeAgent{
		model:  "openrouter/deepseek-v4-flash",
		window: 128_000,
		weight: 12_400,
		usage:  session.Usage{Input: 12_400, Output: 900, CostUSD: 0.31},
	}
	a := newTestApp(agent)
	a.model, a.ctxWindow, a.ctxTokens = agent.model, 128_000, 12_400
	a.branch, a.branchDirty = "cmd/cost-status", true

	a.slash("/status")
	text := lastNote(t, a)
	for _, item := range a.deckItems() {
		if !strings.Contains(text, item.label) || !strings.Contains(text, item.value) {
			t.Fatalf("the note lost %q · %q:\n%s", item.label, item.value, text)
		}
	}
	// The model is its FULL routing address here, the way the sheet records it —
	// the basename is what a forty-four column row has to settle for.
	if !strings.Contains(text, "openrouter/deepseek-v4-flash") {
		t.Fatalf("the note cut the model down to its basename:\n%s", text)
	}
	if !strings.Contains(text, "12.4k/128k · 10%") {
		t.Fatalf("the note lost the context meter:\n%s", text)
	}
	if !strings.Contains(text, "cmd/cost-status*") {
		t.Fatalf("the note lost the branch and its star:\n%s", text)
	}
	if !strings.Contains(text, "$0.31") {
		t.Fatalf("the note lost the bill:\n%s", text)
	}
}

// V2 and V8: /status asks the live seam each time and omits search entirely
// when the session has no web-search hand.
func TestStatusNamesTheNextSearchAndKeepsAnAbsentHandSilent(t *testing.T) {
	status := "firecrawl · keyless"
	a := newApp(t.Context(), Options{
		Agent:        &fakeAgent{model: "m"},
		SearchStatus: func() string { return status },
	})
	a.slash("/status")
	if text := lastNote(t, a); !strings.Contains(text, "search") || !strings.Contains(text, "firecrawl · keyless") {
		t.Fatalf("status did not name the next search:\n%s", text)
	}

	status = "exa · key not set — searches fail"
	a.slash("/status")
	if text := lastNote(t, a); !strings.Contains(text, "search") || !strings.Contains(text, "exa · key not set — searches fail") {
		t.Fatalf("status hid the broken pin:\n%s", text)
	}

	status = "exa · with your key"
	a.slash("/status")
	if text := lastNote(t, a); !strings.Contains(text, "search") || !strings.Contains(text, "exa · with your key") {
		t.Fatalf("status stayed on the launch-time search:\n%s", text)
	}

	without := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}})
	without.slash("/status")
	text := lastNote(t, without)
	for _, line := range strings.Split(text, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == "search" {
			t.Fatalf("a session with no search hand grew a row:\n%s", text)
		}
	}
}

// A BILL OF ZERO IS NOT A BILL, and the two commands have to agree about that:
// the status line draws "$0.00" because it is a live row whose segments must
// not jump sideways, and a note is written once (statusnote.go states the
// trade).
func TestStatusDropsASpendNobodyHasSpent(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})

	a.slash("/status")
	if text := lastNote(t, a); strings.Contains(text, "$0.00") {
		t.Fatalf("a session that has spent nothing was billed:\n%s", text)
	}
}

// A WINDOW NOBODY HAS NAMED IS NOT A METER. There is no context line and there
// is no percentage anywhere in the answer — design-law-v2 §16, which is the
// same silence [app.contextSegment] keeps on the status line.
func TestStatusSaysNothingAboutAContextItCannotMeasure(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.model = "m"

	a.slash("/status")
	text := lastNote(t, a)
	if strings.Contains(text, "%") {
		t.Fatalf("the note quoted a percentage of an unknown window:\n%s", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "context") {
			t.Fatalf("the note grew a context line: %q", line)
		}
	}
}

// The file is one identity line the sheet does not carry, and it is here because a
// path is a thing people copy into another program rather than a thing they
// read off a row (statusnote.go states the trade).
func TestStatusNamesTheFileWhenThereIsOne(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})

	a.slash("/status")
	if strings.Contains(lastNote(t, a), "file") {
		t.Fatal("a session with no file on disk grew a file line")
	}

	a.file = "/tmp/lab/.aforge/sessions/2026-08-17T09-15-02.json"
	a.slash("/status")
	if !strings.Contains(lastNote(t, a), a.file) {
		t.Fatalf("the note lost the session file:\n%s", lastNote(t, a))
	}
}

func TestStatusNamesTheBuildHoldingTheConversation(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.build = "1265feda (dirty) built 2026-08-27 13:28"

	a.slash("/status")
	if text := lastNote(t, a); !strings.Contains(text, "\nbuild") || !strings.Contains(text, a.build) {
		t.Fatalf("the note lost the build:\n%s", text)
	}
}
