package session

import "strings"

// ── THE SENTENCE A TEAM WAKE OPENS WITH, WRITTEN ONCE ───────────────────────
//
// A turn woken by team traffic starts on a note the session writes
// (team_wakewatch.go): a sentence telling the model that nobody typed this,
// then what arrived. The sentence is for the model. The live conversation
// never draws it (tui3's followup.go), and a surface that draws a transcript
// asks [TeamWakeNote] to leave it out the same way, so the wall's tiles and a
// reopened conversation show what the live one shows. The composers use these
// constants, so the reader can never drift from what was written.
const (
	// teamWakeMemberLead opens the note that wakes a member on its manager's
	// directive or answer.
	teamWakeMemberLead = "Your manager started this turn (a directive, or the answer to your question); the person did not speak."
	// teamWakeManagerLead opens the note that wakes a manager on its team's
	// replies.
	teamWakeManagerLead = "Your team's replies started this turn; the person did not speak. Act on them: hand out what comes next, or tell the person where the work stands."
	// teamWakeEarlierLead is how the member's note opened in builds of the
	// team delegation work before it landed, and journals written by them
	// still hold it.
	teamWakeEarlierLead = "Your manager's directive started this turn;"
)

// TeamWakeNote reports whether text is a note the session wrote to wake a
// turn on team traffic.
func TeamWakeNote(text string) bool {
	text = strings.TrimSpace(text)
	for _, lead := range []string{teamWakeMemberLead, teamWakeManagerLead, teamWakeEarlierLead} {
		if strings.HasPrefix(text, lead) {
			return true
		}
	}
	return false
}
