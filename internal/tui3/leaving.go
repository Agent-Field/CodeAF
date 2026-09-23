package tui3

import (
	"strings"
)

// THE DOOR IS ONE CTRL+C, AND WHAT USED TO BE BOUGHT WITH THE SECOND PRESS IS
// BOUGHT ON THE WAY OUT INSTEAD.
//
// ctrl+c is read above every modal on this surface — leaving is never modal —
// and at rest it ends the session on the press that lands. For a while it took
// two: the first press armed the door and put a sentence in the hint slot
// naming what leaving would stop, and the second one inside a second and a half
// went through it. That was a question asked of somebody who had already
// answered it, in the one key every terminal on earth has taught them means
// leave, and it is gone.
//
// WHAT THE ARM WAS PROTECTING IS STILL PROTECTED, and by the exit path rather
// than by a keystroke:
//
//   - THE DRAFT AND EVERY PARKED MESSAGE ARE WRITTEN DOWN. [app.leavingDraft]
//     below is what the draft file gets on the way out, and it is folded in by
//     [app.quit] — so it is owed to a real SIGINT and a closing window too,
//     neither of which was ever going to press a key twice ([Run] in tui3.go is
//     where the signal half is caught).
//   - MID-TURN NOTHING CHANGES. ctrl+c is still the interrupt while a turn is
//     streaming, exactly as it always was: the press aimed at the model hits
//     the model and never reaches the door (input.go).
//   - WORK A HOST IS RUNNING OUTLIVES THE WINDOW ANYWAY (keeper.go's
//     [workOutlivesExit]), which is most of what the warning sentence existed
//     to say.
//
// What is genuinely spent is the WARNING: a person who leaves with tasks running
// in this process is no longer told so first. That is the trade, and it is the
// one the owner asked for — the key means leave.

// ── WHAT LEAVES WITH YOU ────────────────────────────────────────────────────

// leavingDraft is what the draft file gets on the way out: the sentence in the
// box, and then every message still parked above it, each on its own line.
//
// A PARKED MESSAGE IS SOMETHING THE PERSON TYPED AND PRESSED ENTER ON (park.go).
// It waits for the answer that is streaming and goes as a turn of its own when
// that answer lands — so a session closed before the turn ended used to drop it
// on the floor without a word. Folding it back into the draft is the only place
// on this surface that can still hold it: the next launch restores that file
// into the box (draft.go), and the words come back instead of vanishing.
//
// The draft leads because it is the newest thing typed and the thing the cursor
// is in; the parked messages follow in the order they were parked, which is the
// order they would have been sent in.
//
// AND THE DRAFT IT MEANS IS MAIN'S (recipient.go). Both readers of this — the
// file written on the way out of the program, and the sentence that walks with
// the person through /new, /resume and a takeover — are about the CONVERSATION,
// and the parked messages under it are the conversation's too. Read off the
// screen, a window quit while standing in a task's page would keep that page's
// steering line as the conversation's unsent sentence and give it back at the
// next launch in a box pointed at the model.
func (a *app) leavingDraft() string {
	return foldedParkedDraft(a.mainDraftText(), a.parks)
}

// foldedParkedDraft is quitting's text-only insurance assembled from either
// the conversation in front or a held conversation's sidecar.
//
// STRUCTURED PARKS NEVER GO ON DISK. Pictures, paste bodies and standing marks
// remain process memory while a conversation is held; the plain draft file is
// the last-resort record that can promise only that nobody's typed words vanish.
func foldedParkedDraft(text string, parks []parked) string {
	if len(parks) == 0 {
		return text
	}
	lines := make([]string, 0, len(parks)+1)
	if strings.TrimSpace(text) != "" {
		lines = append(lines, text)
	}
	for _, p := range parks {
		if strings.TrimSpace(p.text) != "" {
			lines = append(lines, p.text)
		}
	}
	return strings.Join(lines, "\n")
}
