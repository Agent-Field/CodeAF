package tui3

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE LIVE PREVIEW AT tierPhone (toolview.go).
//
// The preview is the one block a row hangs that nobody clicked for, and its
// whole justification is that it shows the change BEFORE it lands. That
// justification is a claim about proportion, and proportion is what changes
// with the frame: twelve rows is a third of a laptop's body and the WHOLE of a
// phone's. So the ceiling is the tier's ([previewPhoneWindow]) — and only for
// the block nobody asked for; a call somebody OPENED at tierPhone gets the
// whole frame (expand.go) and keeps the wide window.

// livePreviewApp is one write call still in flight, with a body of lines long
// enough that every ceiling below cuts it.
func livePreviewApp(t *testing.T, lines int) *app {
	t.Helper()
	body := make([]string, lines)
	for i := range body {
		body[i] = "line " + strconv.Itoa(i)
	}
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.ANSI256, false)
	a.state = stateWorking
	a.entries = []entry{{
		kind: entryTool, tool: "write", text: "write", status: toolRunning, began: time.Now(),
		detail: toolDetail{Args: `{"path":"notes.md","content":"` + strings.Join(body, `\n`) + `"}`},
	}}
	a.touch()
	return a
}

// previewRowsAt is the preview a call hangs on a frame of one width: the rows
// under the tool line, plain.
func previewRowsAt(t *testing.T, a *app, width int) []string {
	t.Helper()
	rows := a.toolRows(a.conversation(), 0, true, width)
	if len(rows) < 2 {
		t.Fatalf("the call hung nothing at width %d", width)
	}
	out := make([]string, 0, len(rows)-1)
	for _, r := range rows[1:] {
		out = append(out, plain(r.text))
	}
	return out
}

// TestThePhoneKeepsAShortPreviewAndTheWideTiersKeepTheirs is the cap itself. A
// preview is a header, its rows and the clickable foot; what the tier moves is
// how many rows sit between them.
func TestThePhoneKeepsAShortPreviewAndTheWideTiersKeepTheirs(t *testing.T) {
	const lines = 30
	a := livePreviewApp(t, lines)

	for _, c := range []struct {
		what  string
		width int
		want  int
	}{
		{"a phone", phoneWidth, previewPhoneWindow},
		{"a narrow split", 72, previewWindow},
		{"an everyday frame", 100, previewWindow},
		{"the full frame", 140, previewWindow},
	} {
		rows := previewRowsAt(t, a, c.width)
		if len(rows) == 0 || !strings.Contains(rows[0], previewApplying) {
			t.Fatalf("%s hung no preview header: %q", c.what, rows)
		}
		foot := rows[len(rows)-1]
		if !strings.Contains(foot, "more lines") {
			t.Fatalf("%s dropped rows without saying so: %q", c.what, foot)
		}
		if body := len(rows) - 2; body != c.want {
			t.Fatalf("%s previewed %d rows, want %d:\n%s", c.what, body, c.want, strings.Join(rows, "\n"))
		}
		// THE FOOT COUNTS WHAT IS ACTUALLY LEFT. A ceiling that cut harder than
		// the number under it says would be the surface hiding rows it claims to
		// be offering.
		if !strings.Contains(foot, strconv.Itoa(lines-c.want)+" more lines") {
			t.Fatalf("%s miscounted what it held back: %q", c.what, foot)
		}
	}
}

// TestLiftingTheCapAtPhoneStillShowsTheWholeChange: the tighter ceiling bounds
// what arrives UNASKED, and it is not a second answer to "where is the rest".
// The foot is the same one gesture at every tier.
func TestLiftingTheCapAtPhoneStillShowsTheWholeChange(t *testing.T) {
	const lines = 30
	a := livePreviewApp(t, lines)
	a.entries[0].full = true
	a.touch()

	rows := previewRowsAt(t, a, phoneWidth)
	if strings.Contains(rows[len(rows)-1], "more lines") {
		t.Fatalf("a lifted cap still held rows back:\n%s", strings.Join(rows, "\n"))
	}
	if body := len(rows) - 1; body != lines {
		t.Fatalf("a lifted cap showed %d of %d rows", body, lines)
	}
}

// TestTheSheetKeepsTheWideWindowAtPhone is the other half of the seam. At
// tierPhone an OPENED call goes over the whole frame, and the phone's ceiling
// exists to stop an unasked-for block from taking that frame — so it has
// nothing to protect once the person has asked.
func TestTheSheetKeepsTheWideWindowAtPhone(t *testing.T) {
	a := livePreviewApp(t, 30)

	body, more := a.detailBody(&a.entries[0], phoneWidth)
	if got := len(body) - 1; got != previewWindow {
		t.Fatalf("the sheet previewed %d rows, want the wide %d", got, previewWindow)
	}
	if more != 30-previewWindow {
		t.Fatalf("the sheet miscounted what it held back: %d", more)
	}
}

// TestThePhoneCeilingIsTheTiersAnswerAndNotAWidthOfItsOwn keeps the seam where
// [layoutTier] is: one function decides which frame this is, and the preview
// asks the caller's answer rather than comparing widths a second time.
func TestThePhoneCeilingIsTheTiersAnswerAndNotAWidthOfItsOwn(t *testing.T) {
	if previewCap(true) != previewPhoneWindow || previewCap(false) != previewWindow {
		t.Fatalf("the caps are %d/%d", previewCap(true), previewCap(false))
	}
	if previewPhoneWindow >= previewWindow {
		t.Fatal("the phone's ceiling is not tighter than the frame it exists to protect")
	}
	// The tier's own floor is what "phone" means here, and nothing in this file
	// restates it: a 59-column frame is a phone and a 60-column one is not.
	if layoutTier(phoneWidth) != tierPhone || layoutTier(60) == tierPhone {
		t.Fatal("this file has drifted from layoutTier's floor")
	}
}
