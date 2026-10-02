package preflight

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/inventory"
	"github.com/Agent-Field/codeaf/internal/rebuild"
)

// A message about a move is short and bounded, because it is spent once and read
// by a model that will act on every line of it.
const (
	maxLines = 12
	maxChars = 1200
)

// The two forms of the same facts. The brief opens a setup turn the person said
// yes to; the news arrives at the next step when the person did not, or where no
// card was shown. Both are built from one Resume, and the card the person read
// is built from it too, so the person is shown exactly what the agent is told.

const (
	briefHead = "This chat moved here%s. This machine has none of what it left out or left running. " +
		"Bring back only what is listed, then stop; do not explore the machine."
	newsHead = "This chat moved here%s."
	newsRule = "This is news and asks for nothing: do any of it only if the work needs it, and never assume the server is up."
)

// entry is one fact in the two forms it is told in.
type entry struct{ brief, news string }

// group is one list of a message: a label in each form and the entries under it.
type group struct {
	briefLabel, newsLabel string
	entries               []entry
}

// groups are the lists of a resume, in the order they are told. An empty list is
// left out of the message, since a line about nothing is noise.
func (r Resume) groups() []group {
	all := []group{
		{"Not brought along (rebuild from the file named):", "Not brought along, so absent:", r.withOmitted(inventory.ListWithheld, r.missing())},
		{"Was running there, not running here:", "Not running here:", r.withOmitted(inventory.ListRunning, r.stopped())},
		{"Started there outside this chat:", "Started there outside this chat:", r.withOmitted(inventory.ListDetached, r.detached())},
		{"Also needed here:", "Also needed here:", r.lacks()},
	}
	var out []group
	for _, g := range all {
		if len(g.entries) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// withOmitted ends a list with the count the record itself cut, so the message
// says `and N more` for entries it never had.
func (r Resume) withOmitted(list string, entries []entry) []entry {
	if n := r.Omitted[list]; n > 0 && len(entries) > 0 {
		entries = append(entries, entry{more(n), more(n)})
	}
	return entries
}

func (r Resume) missing() []entry {
	out := make([]entry, len(r.Missing))
	for i, w := range r.Missing {
		out[i] = entry{missingBrief(w), missingNews(w)}
	}
	return out
}

// missingBrief names what rebuilds the folder and how it was made, or how it
// usually is when nobody saw it made.
func missingBrief(w inventory.Withheld) string {
	if setApart(w) {
		return w.Path + ": not brought along; " + w.Reason
	}
	how := "usually `" + rebuild.Hint(w.Lock) + "`"
	if w.MadeBy != "" {
		how = "it was made with `" + w.MadeBy + "`" + inDir(w.Cwd)
	}
	return fmt.Sprintf("%s: from %s; %s", w.Path, w.Lock, how)
}

func missingNews(w inventory.Withheld) string {
	if setApart(w) {
		return w.Path + " (" + w.Reason + ")"
	}
	return w.Path + " (" + rebuildCommand(w) + ")"
}

func (r Resume) stopped() []entry {
	out := make([]entry, len(r.Stopped))
	for i, run := range r.Stopped {
		out[i] = entry{
			brief: "`" + run.Command + "`" + inDir(run.Cwd) + ports(run.Ports, ", "),
			news:  "`" + run.Command + "`" + inDir(run.Cwd) + wasOn(run.Ports),
		}
	}
	return out
}

func (r Resume) detached() []entry {
	out := make([]entry, len(r.Detached))
	for i, d := range r.Detached {
		text := "`" + d.Command + "`" + inDir(d.Cwd)
		out[i] = entry{text, text}
	}
	return out
}

func (r Resume) lacks() []entry {
	out := make([]entry, len(r.Lacks))
	for i, it := range r.Lacks {
		out[i] = entry{it.Line(), it.Line()}
	}
	return out
}

// inDir spells a folder after a command, and nothing for the root.
func inDir(cwd string) string {
	if cwd == "" || cwd == "." {
		return ""
	}
	return " in " + strings.TrimSuffix(cwd, "/") + "/"
}

// ports spells the ports a command listened on after sep, and nothing when it
// listened on none.
func ports(list []int, sep string) string {
	switch len(list) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("%sport %d", sep, list[0])
	}
	names := make([]string, len(list))
	for i, p := range list {
		names[i] = fmt.Sprint(p)
	}
	return sep + "ports " + strings.Join(names, ", ")
}

func wasOn(list []int) string {
	if s := ports(list, ""); s != "" {
		return " (was on " + s + ")"
	}
	return ""
}

// fromClause is " from <device>", and nothing when the device is unknown.
func (r Resume) fromClause() string {
	if r.From == "" {
		return ""
	}
	return " from " + r.From
}

// movedClause is the one sentence about a folder that changed, said when a
// recorded command names the old one. The command itself is shown as recorded:
// rewriting it would be the harness deciding what the person meant.
func (r Resume) movedClause() string {
	if r.Was == "" || r.Was == r.Now || !r.namesOldFolder() {
		return ""
	}
	return fmt.Sprintf("The folder was %s; here it is %s.", r.Was, r.Now)
}

func (r Resume) namesOldFolder() bool {
	for _, g := range r.groups() {
		for _, e := range g.entries {
			if strings.Contains(e.brief, r.Was) {
				return true
			}
		}
	}
	return false
}

// Brief is the opening of a setup turn that follows a move: the facts, then what
// this machine lacks. It is empty for a resume with nothing to say.
func (r Resume) Brief() string {
	if r.Empty() {
		return ""
	}
	lines := []string{fmt.Sprintf(briefHead, r.fromClause())}
	if moved := r.movedClause(); moved != "" {
		lines = append(lines, moved)
	}
	groups, hidden := fit(r.groups(), func(e entry) string { return e.brief }, lengthOf(lines), len(lines))
	for _, g := range groups {
		lines = append(lines, g.briefLabel)
		for _, e := range g.entries {
			lines = append(lines, "- "+e.brief)
		}
	}
	if hidden > 0 {
		lines = append(lines, "- "+more(hidden))
	}
	return strings.Join(lines, "\n") + "\n"
}

// News is the same facts as one paragraph that asks for nothing: what the next
// step is told when the person did not say yes, or was never asked. It is empty
// for a resume with nothing to say.
func (r Resume) News() string {
	if r.Empty() {
		return ""
	}
	parts := []string{fmt.Sprintf(newsHead, r.fromClause())}
	if moved := r.movedClause(); moved != "" {
		parts = append(parts, moved)
	}
	groups, hidden := fit(r.groups(), func(e entry) string { return e.news }, lengthOf(parts)+len(newsRule), 1)
	for i, g := range groups {
		texts := make([]string, len(g.entries))
		for j, e := range g.entries {
			texts[j] = e.news
		}
		if hidden > 0 && i == len(groups)-1 {
			texts = append(texts, more(hidden))
		}
		parts = append(parts, g.newsLabel+" "+strings.Join(texts, ", ")+".")
	}
	return strings.Join(append(parts, newsRule), " ")
}

func more(n int) string { return fmt.Sprintf("and %d more", n) }

func lengthOf(parts []string) int {
	n := 0
	for _, p := range parts {
		n += len(p) + 1
	}
	return n
}

// fit keeps as many entries as the message's limits allow and counts the rest.
// spent is what the message already holds in characters and lines; each list
// costs a line for its label and each entry a line and its own length, and one
// line is kept back for the count. The first entry that does not fit ends the
// message: everything after it, in that list and in every later one, is counted,
// so the message says `and N more` once and at its end. The cut is by entries,
// never by characters, so no line is ever half a command.
func fit(all []group, text func(entry) string, spent, lines int) (kept []group, hidden int) {
	room := func(n int, chars int) bool { return lines+n < maxLines && spent+chars+len(more(999)) < maxChars }
	for _, g := range all {
		shown := g
		shown.entries = nil
		for i, e := range g.entries {
			label := 0
			if len(shown.entries) == 0 {
				label = 1
			}
			if hidden > 0 || !room(label+1, len(g.briefLabel)+len(text(e))+4) {
				hidden += len(g.entries) - i
				break
			}
			if label == 1 {
				lines, spent = lines+1, spent+len(g.briefLabel)+1
			}
			shown.entries = append(shown.entries, e)
			lines, spent = lines+1, spent+len(text(e))+3
		}
		if len(shown.entries) > 0 {
			kept = append(kept, shown)
		}
	}
	return kept, hidden
}

// The lines of the offer, in the words a person reads: the same entries the
// agent is told, without the marks a model needs.

// MissingNames are the folders the copy did not bring, each with the command
// that brings it back: saying `set up` consents to that command, so the person
// reads it first.
func (r Resume) MissingNames() []string {
	out := make([]string, len(r.Missing))
	for i, w := range r.Missing {
		out[i] = missingNews(w)
	}
	return out
}

// RunningLines are the commands that were running there.
func (r Resume) RunningLines() []string {
	out := make([]string, len(r.Stopped))
	for i, run := range r.Stopped {
		out[i] = run.Command + inDir(run.Cwd) + ports(run.Ports, " (") + closeIf(len(run.Ports) > 0)
	}
	return out
}

func closeIf(open bool) string {
	if open {
		return ")"
	}
	return ""
}

// NeededLines are what this machine also needs.
func (r Resume) NeededLines() []string {
	out := make([]string, len(r.Lacks))
	for i, it := range r.Lacks {
		out[i] = it.Line()
	}
	return out
}

// Grants are the commands the offer lists, each as the agent will run it: what
// rebuilds each folder that was left out (the command that made it, or the one
// that usually does), and every command that was running or was started outside
// the chat. Saying `set up` is consent for exactly these and nothing more.
func (r Resume) Grants() []string {
	var out []string
	for _, w := range r.Missing {
		if command := rebuildCommand(w); command != "" {
			out = append(out, command)
		}
	}
	for _, run := range r.Stopped {
		out = append(out, run.Command)
	}
	for _, d := range r.Detached {
		out = append(out, d.Command)
	}
	return out
}

// setApart reports whether the entry is a single path left out for a reason,
// which nothing rebuilds: it has no lockfile to name and no command to run.
func setApart(w inventory.Withheld) bool { return w.Lock == "" && w.Reason != "" }

// rebuildCommand is the command a folder is brought back with, and nothing for a
// path that is set apart, since no command brings it back.
func rebuildCommand(w inventory.Withheld) string {
	if setApart(w) {
		return ""
	}
	if w.MadeBy != "" {
		return w.MadeBy
	}
	return rebuild.Hint(w.Lock)
}
