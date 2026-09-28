package session

// A BELT WORKER READS THE BELT'S PAGES AND NOTHING ELSE.
//
// prompts/system.md is the CHAT's account of itself — the pane, the slash
// commands, ten tools the belt does not carry — and prompts/worker.md is the
// TASK worker's page. Neither is a belt page: a worker that read one would be
// handed promises about hands it does not have, and every step of every worker
// would pay for the sentences again. So this test renders a belt worker's
// system prompt the way its own door builds it ([NewBeltWorker]) and holds the
// three things a belt page owes — it is small, it carries none of the chat's
// own words, and it still carries the two facts the loop is finished by.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/effort"
)

// chatOnlySentences are three strings ONLY prompts/system.md carries, one from
// each class the belt's page must not leak: the chat's identity, a file tool
// this belt does not carry, and the chat's rule about how an answer ends. The
// brief named a slash command, the word "pane" and `generate_image` as the
// shapes to pick; this page has none of those three, so these are the same test
// read off the page that actually shipped — each is verified unique to
// prompts/system.md by construction (measured against every other fragment).
//
// AND EACH MUST STILL BE ON THE CHAT'S PAGE, which the test now asserts. The
// closing-offer sentinel was `Say the word and I'll` until #1209 reworded the
// rule without it, and from then on it was a string no page carried: absent
// from the belt's page by default, so it tested nothing at all.
var chatOnlySentences = []string{
	"a working colleague in a conversation", // the chat's first line
	"Regex search",                          // the `grep` hand this belt does not carry
	"no closing offer",                      // the chat's closing-offer rule
}

func TestABeltWorkerReadsTheBeltsPagesAndNothingElse(t *testing.T) {
	// The run road's own worker: [NewBeltWorker] sets InTask and the belt, so
	// this is exactly the page the brief is about (bashbelt_worker_test.go
	// builds it against a real store and a stubbed CLI).
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(planCLIBinEnv, filepath.Join(t.TempDir(), "stub-codeaf"))
	agent := newRunBeltWorker(t, effort.None)
	page := systemTextOf(agent)

	// UNDER THE SIZE BOUND. The belt's own pages are the whole prompt, so a
	// page that grew back toward the chat's shows here first.
	if n := len(page); n >= 16*1024 {
		t.Fatalf("a belt worker's system prompt is %d bytes, want under 16 KiB — the belt's own pages are the whole of it", n)
	}
	// AND NOT ONE SENTENCE OF THE CHAT'S PAGE. Each is prompts/system.md's word
	// for word, and prompts/system.md is not a page this belt reads.
	for _, foreign := range chatOnlySentences {
		if !strings.Contains(systemPrompt, foreign) {
			t.Errorf("the sentinel %q is no longer on prompts/system.md, so its absence here proves nothing: pick one the chat's page still says", foreign)
		}
		if strings.Contains(page, foreign) {
			t.Errorf("a belt worker's page carries %q, which only prompts/system.md has", foreign)
		}
	}
	// AND THE TWO FACTS IT ACTS ON SURVIVE: the finish verb and the plan CLI.
	for _, want := range []string{"plandb done", "plandb"} {
		if !strings.Contains(page, want) {
			t.Errorf("a belt worker's page lost %q, the fact its loop is finished by", want)
		}
	}
	// AND IT OPENS ON THE POLICY, not on the chat colleague the belt replaced.
	if !strings.HasPrefix(page, "You own one task within a shared objective.") {
		t.Errorf("a belt worker's page does not open on the belt's policy: %.120q", page)
	}
}
