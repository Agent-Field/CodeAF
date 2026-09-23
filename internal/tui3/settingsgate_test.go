package tui3

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

// ── THE GATE ROWS, AND THE BADGE THAT CLAIMS WHAT THEY SAY ──────────────────
//
// `ask before running` and the two exception rows under it are the only rows in
// the settings panel whose effect is a SAFETY CLAIM: the status line's badge is
// drawn over an open gate and absent over a closed one (render.go's
// NEGATIVE-SPACE SAFETY), so the badge and the gate are one statement made in
// two places and they may never disagree.
//
// THEY DID DISAGREE, IN BOTH DIRECTIONS. The panel wrote the row to the profile
// and nothing told the gate this conversation was already running behind, while
// the badge re-read that same profile live ([app.approvalPosture]) — so turning
// the asking OFF lit the badge over a gate that went on asking, and turning it
// back ON put the badge out over a gate that was still wide open. The second one
// is exactly the false claim #322 and #325 were about, arrived at from the other
// side.

// gateSheet is the settings panel over a profile whose gate already stands where
// this test wants it, with the cursor on the `ask before running` row.
//
// The row is written through the registry — the writer the launch itself reads
// back — and [app.approval] is then taken the way boot takes it (app.go), so
// what the test starts from is a session whose badge and whose gate agree.
func gateSheet(t *testing.T, mode string) (*app, string) {
	t.Helper()
	a, dir := sheetApp(t)
	row, ok := a.registry().Row(config.KeyToolApprovalMode)
	if !ok {
		t.Fatal("the registry has no gate row to write")
	}
	if err := row.Apply(mode); err != nil {
		t.Fatalf("writing the gate row as %q: %v", mode, err)
	}
	a.approval = a.approvalPosture()
	a.openSettings()
	cursorTo(t, a, config.KeyToolApprovalMode)
	return a, dir
}

// THE ROW REACHES THE RUNNING GATE AND THE BADGE ON THE VERY SAME KEYSTROKE.
// Not at the next turn end, which is where [app.settle] re-reads the posture,
// and not at the next launch, which is what the row's hint used to promise.
func TestTheGateRowReachesTheRunningGateAndTheBadgeOnTheSameKeystroke(t *testing.T) {
	a, dir := gateSheet(t, "allow")
	if a.approval != "allow" || a.approvalSegment() != "YOLO" {
		t.Fatalf("a session launched with the asking off reads %q and draws %q",
			a.approval, a.approvalSegment())
	}
	told := 0
	a.applyApprovals = func() error {
		told++
		return nil
	}

	// allow → deny → prompt, which is the registry's own order and two presses
	// of the one key that answers a cycle row.
	drive(t, a, key("enter"), key("enter"))

	if got := config.ToolApprovalModeAt(dir); got != "prompt" {
		t.Fatalf("the profile row reads %q after two cycles, want prompt", got)
	}
	if told != 2 {
		t.Fatalf("the running gate was rebuilt %d times over two changes, want 2", told)
	}
	// THE BADGE IS OUT NOW AND NOT ONE TURN FROM NOW. Nothing has settled, no
	// turn has ended, and the person is still looking at the panel.
	if a.approval != "prompt" {
		t.Fatalf("the badge still reads %q while the gate asks first", a.approval)
	}
	if got := a.approvalSegment(); got != "" {
		t.Fatalf("the status line drew %q over a gate that asks before it runs anything", got)
	}
	if a.sheet.msg != "" {
		t.Fatalf("a change that landed still said something: %q", a.sheet.msg)
	}

	// AND THE OTHER DIRECTION, which is the dangerous one: the asking goes off
	// and the badge has to appear on the same keystroke.
	drive(t, a, key("enter"))
	if got := config.ToolApprovalModeAt(dir); got != "allow" {
		t.Fatalf("the profile row reads %q, want allow", got)
	}
	if told != 3 {
		t.Fatalf("the running gate was rebuilt %d times, want 3", told)
	}
	if a.approval != "allow" || a.approvalSegment() != "YOLO" {
		t.Fatalf("the gate is open and the line reads %q · drew %q", a.approval, a.approvalSegment())
	}
}

// THE TWO EXCEPTION ROWS TAKE THE SAME ROAD. They are edited on the same tab and
// the gate is built from all three together (cmd/codeaf's v3Policy), so a rule
// written here that the running conversation could not see would be the same
// defect one row down.
func TestTheToolAndShellRowsReachTheRunningGateToo(t *testing.T) {
	for _, row := range []struct {
		key  string
		text string
		read func(string) string
	}{
		{config.KeyToolApprovals, "read:deny", config.ToolApprovalsAt},
		{config.KeyBashApprovals, "deny rm -rf *", config.BashApprovalsAt},
	} {
		a, dir := sheetApp(t)
		told := 0
		a.applyApprovals = func() error {
			told++
			return nil
		}
		a.openSettings()
		cursorTo(t, a, row.key)
		drive(t, a, key("enter"))
		if a.sheet.edit == nil {
			t.Fatalf("%s did not open its box", row.key)
		}
		a.sheet.edit.box.setText(row.text)
		drive(t, a, key("enter"))

		if got := row.read(dir); got != row.text {
			t.Fatalf("%s reads %q on disk, want %q", row.key, got, row.text)
		}
		if told != 1 {
			t.Fatalf("%s told the running gate %d times, want once", row.key, told)
		}
		if a.sheet.msg != "" {
			t.Fatalf("%s landed and still said %q", row.key, a.sheet.msg)
		}
	}
}

// A REBUILD THAT COULD NOT BE DONE IS SAID OUT LOUD, and the badge stays with
// the gate that is still standing.
//
// The row is on the disk either way, so the next session is right — and the one
// thing this panel may not do is let somebody walk away believing the
// conversation in front of them has already changed. The words are /permissions'
// own ([nextSessionWord]), because it is the same seam answering the same
// question.
func TestAGateRowThatCouldNotBeRebuiltNamesTheNextSession(t *testing.T) {
	for _, seam := range []struct {
		name string
		push func() error
	}{
		{"a seam that refused", func() error { return os.ErrPermission }},
		{"no seam at all", nil},
	} {
		t.Run(seam.name, func(t *testing.T) {
			a, dir := gateSheet(t, "allow")
			a.applyApprovals = seam.push

			drive(t, a, key("enter"), key("enter"))

			if got := config.ToolApprovalModeAt(dir); got != "prompt" {
				t.Fatalf("the write did not stand: the row reads %q", got)
			}
			if a.sheet.msg != gateNextSessionWord {
				t.Fatalf("the panel said %q, want %q", a.sheet.msg, gateNextSessionWord)
			}
			if !strings.Contains(a.sheet.msg, nextSessionWord) {
				t.Fatalf("the panel did not name the next session: %q", a.sheet.msg)
			}
			// AND THE BADGE FOLLOWS THE GATE, NOT THE FILE. The gate this
			// conversation is behind is still the open one, so the claim on the
			// line has to stay the open one too.
			if a.approval != "allow" || a.approvalSegment() != "YOLO" {
				t.Fatalf("the badge read %q · drew %q over a gate that is still open",
					a.approval, a.approvalSegment())
			}
		})
	}
}

// ── /status SAYS WHICH GATE IT IS BEHIND ────────────────────────────────────

// noteFact is one labelled line's value out of a /status note, found BY ITS
// LABEL and never by the width of the gap in front of it.
//
// That gap is measured from the widest label the session happens to carry
// ([deckLabelWidth]), so a page that grows a fact grows the column — and three
// crew assertions written as "crew" plus exactly five spaces were assertions
// about every other row on the page. They broke the day `approvals` landed,
// which is nine characters wide and had nothing whatever to do with the crew.
func noteFact(text, label string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if fields := strings.Fields(trimmed); len(fields) > 0 && fields[0] == label {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, label))
		}
	}
	return ""
}

// statusObject is the last note read back as /status --json's own object.
func statusObject(t *testing.T, a *app) map[string]string {
	t.Helper()
	text := lastNote(t, a)
	var object map[string]string
	if err := json.Unmarshal([]byte(text), &object); err != nil {
		t.Fatalf("the note is not one JSON object of strings: %v\n%s", err, text)
	}
	return object
}

// /status IS "EVERYTHING THE STATUS LINE KNOWS" AND THE GATE WAS MISSING FROM
// IT. The line's own spelling of the posture is a badge over an open gate and
// nothing at all otherwise, which is right for a row read at a glance and leaves
// a page with no answer to "am I being asked before things run" — so the page
// states the posture in the `ask before running` row's own words, whichever of
// the three it is, under the label the phone's sheet already gives the segment.
func TestStatusNamesTheGateThisConversationIsBehind(t *testing.T) {
	// THE PAGE SAYS THE LADDER'S WORD — `ask`, `allow`, `deny` — which is what
	// the engine reports, rather than the row's `prompt` (approvalchip.go's
	// [app.approvalPostureWord]); the row's word is what this surface was handed.
	for row, posture := range map[string]string{"prompt": "ask", "allow": "allow", "deny": "deny"} {
		a := newTestApp(&fakeAgent{model: "m"})
		a.approval = row

		a.slash("/status")
		text := lastNote(t, a)
		line := ""
		for _, row := range strings.Split(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(row), "approvals") {
				if line != "" {
					t.Fatalf("the note names the gate twice:\n%s", text)
				}
				line = strings.TrimSpace(row)
			}
		}
		if line == "" {
			t.Fatalf("the note says nothing about the gate:\n%s", text)
		}
		if !strings.HasSuffix(line, posture) {
			t.Fatalf("the approvals line reads %q, want the posture %q", line, posture)
		}
		// AND IT IS THE BADGE'S WORD AND NOT THE BADGE. `YOLO` is a mark on a
		// row; a page says which of the three answers is in force.
		if strings.Contains(text, "YOLO") {
			t.Fatalf("the note printed the badge instead of the posture:\n%s", text)
		}
		// The phone's sheet is the other half of this one surface, and it carries
		// the fact under the same label (statusdeck.go).
		if got := deckValue(a.deckItems(), deckSegWords[segYolo]); got != posture {
			t.Fatalf("the phone's sheet says %q where the note says %q", got, posture)
		}

		// AND THE OBJECT CARRIES IT TOO, because both forms are one list.
		a.slash("/status --json")
		object := statusObject(t, a)
		if object["approvals"] != posture {
			t.Fatalf("the object says %q under approvals, want %q:\n%v", object["approvals"], posture, object)
		}
	}
}

// A POSTURE NOBODY CAN ANSWER FOR IS NOT A LINE. Over --host the gate belongs to
// the engine's machine and the answer travels once on the welcome; an engine that
// carried none leaves this page with nothing to say, which is the emptiness law
// and the same silence the badge keeps there.
func TestStatusSaysNothingAboutAGateItWasNeverToldAbout(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.approval = ""

	a.slash("/status")
	text := lastNote(t, a)
	for _, row := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(row), "approvals") {
			t.Fatalf("a session with no posture to report grew a row: %q", row)
		}
	}
	a.slash("/status --json")
	if _, ok := statusObject(t, a)["approvals"]; ok {
		t.Fatalf("the object grew an approvals key over an unknown posture:\n%s", lastNote(t, a))
	}
}
