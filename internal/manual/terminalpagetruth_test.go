package manual

import (
	"strings"
	"testing"
)

// ── THE TERMINAL PAGE MAY NOT CLAIM A MEASUREMENT NOBODY TOOK ──────────────
//
// `running-from-the-terminal` described the `steps` key of the `--json` envelope
// as one a saved program "reports 0" for. `reports 0` tells a reader a
// measurement was taken and came back zero; a saved program does not count its
// steps at all, so the figure is an absence wearing a number's clothes, and a
// harness author reading that line writes `if steps == 0: nothing ran`.
//
// THE KEY ITSELF STAYS, AND THAT IS NOT THE EMPTINESS LAW LOSING. That law
// governs what a PERSON reads on a screen. `--json` is read by a program, and
// the envelope's written guarantee is that within a release a field is never
// removed and never changes meaning — a key that vanished when its number was
// unknown would break every caller that reaches for it. So what changes is the
// sentence, not the contract.
func TestTheTerminalPageDoesNotSayASavedProgramReportsZeroSteps(t *testing.T) {
	page := terminalPage(t)

	if strings.Contains(page, "does not count them and reports 0") {
		t.Fatalf("the terminal page still says a saved program `reports 0` for `steps`, " +
			"which tells a reader a measurement was taken")
	}
	// AND IT SAYS WHICH IT IS. A sentence that merely dropped the claim would
	// leave a reader with a `0` and no account of it, so the page has to name
	// the absence.
	for _, want := range []string{"does not measure it", "measurement nobody took"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the terminal page does not say that a saved program's `steps` is unmeasured; "+
				"looked for %q in its `--json` table", want)
		}
	}

	// AND EVERY KEY OF THE CONTRACT IS STILL DOCUMENTED. This is the ruling in
	// test form: the sentence moved and the shape did not.
	for _, key := range []string{
		"`ok`", "`stop`", "`answer`", "`files`", "`error`",
		"`spend_usd`", "`tokens`", "`seconds`", "`model`", "`steps`",
	} {
		if !strings.Contains(page, key) {
			t.Fatalf("the terminal page stopped documenting the %s key of the --json envelope; "+
				"a caller reaching for a key the manual dropped has nothing to read", key)
		}
	}
}

// ── EXIT 1 MEANS NOTHING RAN, AND THE PAGE SAYS SO ─────────────────────────
//
// `aforge exec` put a mid-run provider failure on exit 1 — the rung whose whole
// meaning is that nothing was attempted — so a script retried a run that had
// already spent money as though it had never begun. The code now leaves that
// rung to refusals before any work starts, and a person writing a wrapper finds
// out here or not at all.
func TestTheTerminalPageSaysARunThatStartedNeverLeavesOnExitOne(t *testing.T) {
	page := terminalPage(t)
	for _, want := range []string{
		// The law, in the words the page uses for it.
		"Exit 1 means nothing ran",
		// And the case it is actually about, so somebody who met the old
		// behaviour recognises their own situation on the page.
		"A run that started and then failed leaves with 2, not 1",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the terminal page does not carry the exit-1 law; looked for %q", want)
		}
	}
}

// ── THE TERMINAL READER'S OWN EXAMPLES ARE WHAT IT PRINTS ──────────────────
//
// The page shows a worked `aforge why` record and a worked `aforge rebuild`
// receipt, and both quoted the machinery vocabulary those commands used to
// print: a note signed `the harness`, and a count of `nodes`. A page that shows
// somebody an output line they will never see is worse than one that shows none.
func TestTheTerminalPageQuotesTheWordsWhyAndRebuildActuallyPrint(t *testing.T) {
	page := terminalPage(t)
	for _, stale := range []string{"turn 4 · the harness", "rebuilt 128 nodes"} {
		if strings.Contains(page, stale) {
			t.Fatalf("the terminal page shows %q, which is not what the command prints any more", stale)
		}
	}
	for _, want := range []string{"turn 4 · aforge", "rebuilt 128 steps"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the terminal page's worked example does not show %q, which is what the command prints", want)
		}
	}
}

// terminalPage is `running-from-the-terminal` as one line, so a sentence that
// wrapped across a line break is still found whole.
func terminalPage(t *testing.T) string {
	t.Helper()
	text, ok := Chat().Page("running-from-the-terminal")
	if !ok {
		t.Fatal("the chat corpus has no running-from-the-terminal page")
	}
	return flatten(text)
}
