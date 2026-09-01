package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// resumedJournal writes a transcript that has already spent money and hands
// back its path.
//
// The lines are shaped exactly as the engine writes them (session's
// sessionfile.go): a header, the exchange, one usage line for the turn itself
// and one flagged `aux` for an errand the session ran beside it. The two are
// deliberately different sizes so an assertion below can tell a sum from
// whichever half it was taken from.
func resumedJournal(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "session.jsonl")
	lines := strings.Join([]string{
		`{"type":"session","version":1,"id":"fix128aaaaaaaaaa","cwd":"` + dir + `","model":"vendor/m","timestamp":"2026-08-31T20:57:46.937847Z"}`,
		`{"type":"message","role":"user","content":"what did we spend?","timestamp":"2026-08-31T20:57:50Z"}`,
		`{"type":"message","role":"assistant","content":"A fair amount so far.","timestamp":"2026-08-31T20:57:55Z"}`,
		`{"type":"usage","usage":{"model":"vendor/m","input":48100,"output":3200,"cacheRead":14080,"costUsd":0.42,"calls":12,"durationMs":192000},"timestamp":"2026-08-31T20:57:55Z"}`,
		`{"type":"usage","usage":{"model":"vendor/errand","input":686,"output":120,"costUsd":0.02,"calls":2,"aux":true},"timestamp":"2026-08-31T20:58:05Z"}`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	return path
}

// resumedAgent opens a real session on a journal, which is the resume path a
// person takes with `aforge chat --session <path>`.
func resumedAgent(t *testing.T, dir, file string) *session.Agent {
	t.Helper()
	agent, err := session.New(session.Config{
		Workspace: dir,
		Model:     "vendor/m",
		APIKey:    "test",
		// A base URL is required to build a session and never dialled by these
		// tests: no turn is sent, and the whole question is what the file
		// already holds.
		BaseURL:     "http://127.0.0.1:1/never-dialled",
		System:      "SYSTEM",
		SessionFile: file,
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent
}

// A RESUMED CONVERSATION OPENS KNOWING WHAT IT HAS ALREADY SPENT, on its first
// frame and with no turn sent.
//
// The engine always restored the total from the journal's usage lines; nothing
// on this surface asked for it until the paint clock came round, and the paint
// clock only turns while something is animating — so an idle resumed session sat
// at the prompt reporting `$0.00` over a conversation that had spent real money
// (#128). The figures below are the file's two usage lines summed, the auxiliary
// one included: an errand the session ran was still this conversation's bill.
func TestAResumedConversationOpensWithTheSpendItsJournalRecords(t *testing.T) {
	dir := t.TempDir()
	agent := resumedAgent(t, dir, resumedJournal(t, dir))

	a := newApp(t.Context(), Options{Agent: agent, Workspace: dir, Resumed: true})

	if got := dollars(a.cost); got != "$0.44" {
		t.Fatalf("the resumed session's cost is %q, want the journal's two lines summed", got)
	}
	if a.inputTokens != 48786 || a.outputTokens != 3320 {
		t.Fatalf("restored tokens = %d in / %d out, want 48786/3320",
			a.inputTokens, a.outputTokens)
	}
	if a.tokens != 52106 {
		t.Fatalf("restored total tokens = %d, want both halves' 52106", a.tokens)
	}
	if a.cacheRead != 14080 {
		t.Fatalf("restored cache reads = %d, want the journal's 14080", a.cacheRead)
	}

	// THE STATUS LINE IS THE SURFACE THE BUG WAS SEEN ON, so it is asserted as a
	// person reads it rather than through the field behind it.
	if line := plain(a.status(200)); !strings.Contains(line, "$0.44") {
		t.Fatalf("the status line does not carry the conversation's spend:\n%q", line)
	}

	// And /cost's own figures, which read the same counters plus the report's
	// call count — the honest denominator for the money above it.
	cost := plain(a.costText())
	for _, want := range []string{"$0.44", "48.8k in · 3.3k out", "14.1k read", "model calls", "14"} {
		if !strings.Contains(cost, want) {
			t.Fatalf("/cost is missing %q:\n%s", want, cost)
		}
	}
}

// AND ONE THAT SPENT NOTHING STILL SAYS NOTHING. The restore folds a zero Usage
// in, which leaves every counter where it was, so the emptiness law reaches the
// resumed session exactly as it reaches a fresh one.
func TestAResumedConversationThatSpentNothingStillDrawsNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	lines := strings.Join([]string{
		`{"type":"session","version":1,"id":"fix128bbbbbbbbbb","cwd":"` + dir + `","model":"vendor/m","timestamp":"2026-08-31T20:57:46.937847Z"}`,
		`{"type":"message","role":"user","content":"hello","timestamp":"2026-08-31T20:57:50Z"}`,
		`{"type":"message","role":"assistant","content":"hello yourself","timestamp":"2026-08-31T20:57:55Z"}`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	a := newApp(t.Context(), Options{Agent: resumedAgent(t, dir, path), Workspace: dir, Resumed: true})

	if a.cost != 0 || a.tokens != 0 {
		t.Fatalf("a journal with no usage lines restored $%v / %d tokens", a.cost, a.tokens)
	}
	if got := plain(a.costText()); got != "nothing spent yet — this session has not sent a turn." {
		t.Fatalf("/cost on an unspent resumed session says:\n%q", got)
	}
}

// SWITCHING TO A CONVERSATION BRINGS ITS BILL WITH IT. The meters are zeroed on
// the way in because every figure on them is a fact about the conversation being
// left; this is the other half of that statement, and without it every door onto
// a second conversation — the welcome list, the keeper, a take-over — landed on
// `$0.00` over a session that had spent money.
func TestSwitchingToAResumedConversationTakesUpItsSpend(t *testing.T) {
	first := t.TempDir()
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}, Workspace: first})
	a.width, a.height = 200, 40
	a.cost = 9.99

	second := t.TempDir()
	agent := resumedAgent(t, second, resumedJournal(t, second))
	a.attachConversation(Conversation{Agent: agent, Workspace: second, Resumed: true}, nil)

	if got := dollars(a.cost); got != "$0.44" {
		t.Fatalf("after the switch the status line says %q, want the arriving conversation's own bill", got)
	}
	if a.inputTokens != 48786 || a.outputTokens != 3320 {
		t.Fatalf("after the switch tokens = %d in / %d out, want the arriving journal's 48786/3320",
			a.inputTokens, a.outputTokens)
	}
}
