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
}

// Digest limits.
const (
	digestTraffic  = 20  // the most Traffic lines shown
	digestFiles    = 5   // the most files named per member
	digestQuestion = 160 // characters of a waiting question
	digestText     = 200 // characters of one Traffic line's text
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
		head.WriteString(memberLine(m, states[m.Key], m.Key == team.Manager))
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

func memberLine(m Member, s MemberState, manager bool) string {
	var b strings.Builder
	b.WriteString("- " + address(m))
	if manager {
		b.WriteString(" (manager)")
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
	b.WriteString(": " + state)
	if s.ReportsTo != "" {
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
	line := fmt.Sprintf("- %s %s %s -> %s: %s", e.At.Format("15:04"), e.Kind, e.From, e.To,
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
