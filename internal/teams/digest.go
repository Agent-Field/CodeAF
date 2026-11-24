package teams

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The digest is what a team's manager is told about its team at the top of a
// turn: who is in it, what each member is doing, and the last few lines of
// Traffic. It is plain text cut to a character budget, and it is built only
// from what the caller hands in, so the same inputs always give the same text.

// StoppedByPerson is the text of a member's turn-ending event when the person,
// and not its manager, stopped the turn. The manager is woken on it: work it
// handed out ended without a report, and only this line says so.
const StoppedByPerson = "stopped by the person"

// Member states a caller reports in [MemberState].
const (
	StateRunning  = "running"
	StateIdle     = "idle"
	StateAsking   = "asking"
	StateFinished = "finished"
	StateFailed   = "failed"
)

// MemberState is what the caller knows about one member right now. Every
// field may be left empty.
type MemberState struct {
	// State is one of the State constants.
	State string
	// SinceActive is how long ago the member last did anything; 0 is not
	// known.
	SinceActive time.Duration
	// Question is the question the member is waiting on a person for, if any.
	Question string
	// Files are the files the member has touched.
	Files []string
	// ReportsTo is the name of the team whose manager this member reports to
	// when that is not this team (home.go): a shared member, which this
	// team's manager may read and send a note to, and not direct. A shared
	// member that is running is drawn busy for that team.
	ReportsTo string
	// Paused says the person paused this conversation in another of its teams
	// ([File.PausedAnywhere]); the member's own record says it for this one.
	Paused bool
}

// Digest limits.
const (
	digestTraffic  = 20  // the most Traffic lines shown
	digestFiles    = 5   // the most files named per member
	digestQuestion = 160 // characters of a waiting question
	digestText     = 200 // characters of one Traffic line's text
	digestRole     = 60  // characters of a member's role name
)

// Digest is the text block for team's manager. states is keyed by conversation
// key; recent is Traffic, oldest first, of which the newest lines that fit are
// shown. The members always come before any Traffic line; Traffic is cut from
// the oldest end first, and a digest whose members alone do not fit is cut at
// the budget with a closing "…". A budget of 0 or less is no limit.
func Digest(team Team, states map[string]MemberState, recent []Entry, budget int) string {
	var head strings.Builder
	manager := "no manager"
	if m, ok := team.Member(team.Manager); ok {
		manager = "manager " + address(m)
	}
	fmt.Fprintf(&head, "Team %q: %d members, %s.\n", team.Name, len(team.Members), manager)
	for _, m := range team.Members {
		head.WriteString(memberLine(m, states[m.Key], m.Key == team.Manager, lastWord(m, recent)))
		head.WriteByte('\n')
	}
	text := head.String()
	if budget > 0 && utf8.RuneCountInString(text) > budget {
		return cutRunes(text, budget)
	}

	if len(recent) > digestTraffic {
		recent = recent[len(recent)-digestTraffic:]
	}
	const title = "Recent traffic:\n"
	left := budget - utf8.RuneCountInString(text) - utf8.RuneCountInString(title)
	var lines []string
	for i := len(recent) - 1; i >= 0; i-- {
		line := trafficLine(recent[i]) + "\n"
		n := utf8.RuneCountInString(line)
		if budget > 0 && n > left {
			break
		}
		left -= n
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(text)
	b.WriteString(title)
	for i := len(lines) - 1; i >= 0; i-- {
		b.WriteString(lines[i])
	}
	return b.String()
}

// address is how a member is named in the digest: @handle, or its key while
// it has no handle.
func address(m Member) string {
	if m.Handle != "" {
		return "@" + m.Handle
	}
	return "[" + m.Key + "]"
}

// Words the digest says about a member beyond its state. They are the
// manager's only account of why handed-out work is not moving, so each names
// who caused it and what ends it.
const (
	// DigestPaused is a member the person paused ([Member.Paused]).
	DigestPaused = "paused by the person; it starts nothing until they resume it"
	// DigestStandby is a member made without work that has not been handed its
	// start line yet ([Member.Standby]).
	DigestStandby = "on standby; no work handed out yet"
)

// lastWord is what the recent traffic says about how a member's last handed-out
// work went, "" when it says nothing worth a manager's attention: a directive
// the member has not started on, or a turn the person stopped. It reads only
// the lines the digest was given, newest first, and stops at the member's own
// newest line.
func lastWord(m Member, recent []Entry) string {
	if m.Handle == "" {
		return ""
	}
	for i := len(recent) - 1; i >= 0; i-- {
		e := recent[i]
		switch {
		case e.From == m.Handle && e.Member != "" && e.Member != m.Key:
			// A line under this member's name about somebody else: the wake
			// its reply gave the manager ("woke ◆").
		case e.From == m.Handle:
			if e.Kind == KindEvent && e.State == StateIdle && e.Text == StoppedByPerson {
				return "its last turn was " + StoppedByPerson
			}
			return ""
		case e.Kind == KindEvent && e.From == FromManager && e.Member == m.Key && e.State == StateRunning:
			// The wake line: the member started on what it was sent.
			return ""
		case e.Kind == KindDirective && e.From == FromManager && e.Addressed(m.Handle):
			return digestNotStarted + ThreadNumber(e.ID)
		}
	}
	return ""
}

// digestNotStarted opens the digest's word for a directive nobody began.
const digestNotStarted = "has not started on "

// lastNotStarted is last when it is that word, "" otherwise.
func lastNotStarted(last string) string {
	if strings.HasPrefix(last, digestNotStarted) {
		return last
	}
	return ""
}

func memberLine(m Member, s MemberState, manager bool, last string) string {
	var b strings.Builder
	b.WriteString("- " + address(m))
	if manager {
		b.WriteString(" (manager)")
	}
	if role := oneLine(m.Role); role != "" && !manager {
		b.WriteString(", " + cutRunes(role, digestRole))
	}
	if title := oneLine(m.Word); title != "" {
		fmt.Fprintf(&b, " %q", title)
	}
	state := s.State
	if state == "" {
		state = "unknown"
	}
	if s.ReportsTo != "" && state == StateRunning {
		state = "busy for " + s.ReportsTo
	}
	resting := state != StateRunning && state != StateAsking && !strings.HasPrefix(state, "busy for ")
	quiet := state == StateIdle || state == "unknown"
	switch {
	case manager:
	case (m.Paused || s.Paused) && quiet:
		state = DigestPaused
	case m.Paused || s.Paused:
		state += ", " + DigestPaused
	case m.Standby && quiet:
		state = DigestStandby
	}
	b.WriteString(": " + state)
	// A directive a member in a turn has not answered yet is not one it has
	// not started on: it reads it at its next step.
	if last != "" && !manager && (resting || last != lastNotStarted(last)) {
		b.WriteString(", " + last)
	}
	if m.Independent {
		b.WriteString(", independent; notes only")
	} else if s.ReportsTo != "" {
		b.WriteString(", reports to " + s.ReportsTo)
	}
	if s.SinceActive > 0 {
		b.WriteString(", active " + ago(s.SinceActive))
	}
	if q := oneLine(s.Question); q != "" {
		fmt.Fprintf(&b, ". Asking: %q", cutRunes(q, digestQuestion))
	}
	if len(s.Files) > 0 {
		shown := s.Files
		if len(shown) > digestFiles {
			shown = shown[:digestFiles]
		}
		b.WriteString(". Files: " + strings.Join(shown, ", "))
		if more := len(s.Files) - len(shown); more > 0 {
			b.WriteString(" +" + strconv.Itoa(more) + " more")
		}
	}
	return b.String()
}

func trafficLine(e Entry) string {
	to := e.To
	if e.To == ToSeveral {
		to = strings.Join(e.Handles, ",")
	}
	line := fmt.Sprintf("- %s %s %s -> %s: %s", e.At.Format("15:04"), e.Kind, e.From, to,
		cutRunes(oneLine(e.Text), digestText))
	if len(e.Files) > 0 {
		line += " [" + strings.Join(e.Files, ", ") + "]"
	}
	return line
}

// ago is a duration as a COARSE age: "within the hour", "2h ago", "3d ago".
//
// IT MOVES AT MOST ONCE AN HOUR, and that is the point. The digest rides a
// manager's turn as a note that lands again only when its text moves, and a
// note that lands again is a message the manager's transcript carries for the
// rest of its life (session's landNoteLocked: an append, never a rewrite, for
// the prompt cache). An age counted in minutes moved the text every minute,
// so a manager asked twice in five minutes paid for its whole team twice with
// nothing changed. Nobody running a team needs to know "3m" from "7m".
func ago(d time.Duration) string {
	switch {
	case d < time.Hour:
		return "within the hour"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h ago"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d ago"
}

// oneLine is s with its whitespace runs, newlines included, made single
// spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// cutRunes is s cut to n characters, the last of them "…" when it was cut.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
