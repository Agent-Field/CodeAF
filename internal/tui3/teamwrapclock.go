package tui3

import "time"

// ── A TEAM THAT IS WRAPPING UP SAYS HOW LONG IT HAS ─────────────────────────
//
// `Wrap up first` gives the manager a bounded time to bring its closing report
// (internal/session's team_wrapup.go), and the clock is kept ON THE TEAM in
// teams.json (teams' Wrap: when it began and how long it was given), so it
// survives a restart. Nothing drew it: a person who asked a team to wrap up
// could not see whether it had twelve minutes left or none. The teams page's
// header and the manager's side column now say it.
//
// IT IS READ FROM THE TEAM THIS WINDOW HOLDS, never from the store on the paint
// clock: the frame law holds here as everywhere. The time left is the start
// plus the bound less now, so a window opened after a restart says the same as
// the one that was closed. Both places are cached by the words, so they move
// on the clock alone.

// teamWrapWords is what a team's wrap-up says at now: `wrapping up · 12m
// left`, `wrapping up · under a minute left`, `wrapping up · out of time`, and
// `wrapping up` alone for a clock from a file written before the bound was
// kept. "" for a team with no wrap-up, or a closed one: the emptiness law.
func (a *app) teamWrapWords(t team, now time.Time) string {
	if t.Wrap == nil || t.Closed() || t.Wrap.Started.IsZero() {
		return ""
	}
	words := "wrapping up"
	if t.Wrap.Bound <= 0 {
		return words
	}
	sep := " " + a.teamsDot() + " "
	left := t.Wrap.Started.Add(t.Wrap.Bound).Sub(now)
	switch {
	case left <= 0:
		return words + sep + "out of time"
	case left < time.Minute:
		return words + sep + "under a minute left"
	}
	minutes := int((left + time.Minute - 1) / time.Minute)
	return words + sep + itoa(minutes) + "m left"
}
