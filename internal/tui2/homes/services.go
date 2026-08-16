package homes

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The services room: a log tail, three verbs, and no composer (5.24).
//
// The composer is the point. A log tail looks like a transcript — same column,
// same scroll, same monospace — and a prompt underneath it would be an
// invitation to talk to a process that cannot hear. 5.15's one rule is that the
// affordance never lies, so a service row binds [rail.ComposerNone] and the
// chat surface draws the reason in place of the prompt.

func (v *View) services(state State, rowID string, width, height int) {
	if rowID == "" {
		v.servicesBrief(state, width, height)
		return
	}
	id := strings.TrimPrefix(rowID, ServiceRowPrefix)
	for i := range state.Services.Services {
		if state.Services.Services[i].ID == id {
			v.service(state.Services.Services[i], state, width, height)
			return
		}
	}
	v.gone(width, height)
}

func (v *View) servicesBrief(state State, width, height int) {
	if !v.sectionWord("services", width, height) {
		return
	}
	if !v.prose(HomeServices.Blurb(), tokens.TextTertiary, width, height) {
		return
	}
	if !v.blank(height) {
		return
	}
	list := state.Services.Services
	if len(list) == 0 {
		v.prose(RouteServices.Empty(), tokens.TextSecondary, width, height)
		return
	}
	for i := range list {
		sv := list[i]
		glyph, tok := v.stateGlyph(sv.Life, false)
		if !v.row(glyph, tok, sv.Name, sv.word(), tokens.TextPrimary, false, width, height) {
			return
		}
		if !v.indented(sv.line(), width, height) {
			return
		}
	}
}

// service is one process, opened: what it is, then what it has been saying.
func (v *View) service(s Service, state State, width, height int) {
	glyph, tok := v.stateGlyph(s.Life, false)
	if !v.heading(glyph, tok, s.Name+" "+tokens.GlyphSeparator+" "+s.word(), width, height) {
		return
	}
	if !v.blank(height) {
		return
	}
	if s.Command != "" && !v.pair("command", s.Command, width, height) {
		return
	}
	if s.Health != "" && !v.pair("health", s.Health, width, height) {
		return
	}
	if a := age(s.Since, state.Now); a != "" {
		if !v.pair("up", a, width, height) {
			return
		}
	}
	if s.Restarts > 0 && !v.pair("restarts", count(s.Restarts, false), width, height) {
		return
	}
	if !v.pair("auto-restart", yesNo(s.AutoRestart), width, height) {
		return
	}
	if s.LogPath != "" && !v.pairPath("log", s.LogPath, width, height) {
		return
	}
	if !v.blank(height) {
		return
	}
	if !v.logTail(s, width, height) {
		return
	}
	if len(s.Verbs) == 0 {
		return
	}
	if !v.blank(height) {
		return
	}
	v.verbs(s.Verbs, width, height)
}

// logTail draws what the service has been saying, oldest first.
//
// When the tail dropped lines it says so, and it says so with
// [tokens.GlyphCut] rather than an ellipsis: 12.5.2's mark means "this stopped
// and should not have", an ellipsis means "there is more, ask for it", and a
// log window is the second. The count is the information — `╌ 4,812 earlier
// lines` — and the rule under it is the visible seam between what is on screen
// and what is not.
//
// A log line is bytes a foreign process wrote. It goes through the same
// sanitiser everything else does, so a service that prints an escape sequence
// cannot repaint the frame around it.
func (v *View) logTail(s Service, width, height int) bool {
	if len(s.Log) == 0 {
		return v.text("(no output)", tokens.TextTertiary, width, height)
	}
	if s.Dropped > 0 {
		l := &v.line
		l.reset(width)
		l.add(v.glyph(tokens.GCut), tokens.TextTertiary)
		l.add(" ", tokens.TextTertiary)
		l.add(earlier(s.Dropped), tokens.TextTertiary)
		if !v.push(l.emit(&v.buf, v.profile, v.focus, width, false, tokens.Ground), height) {
			return false
		}
	}
	for _, row := range s.Log {
		if !v.text(row, tokens.TextSecondary, width, height) {
			return false
		}
	}
	return len(v.lines) < height
}

func earlier(n int) string {
	return plural(n, "earlier line", "earlier lines")
}

// yesNo is the two-word reading of a boolean rail. "on"/"off" rather than
// "true"/"false": the row is a setting a person changes, not a field.
func yesNo(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
