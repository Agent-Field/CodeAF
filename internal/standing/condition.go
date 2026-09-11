package standing

// condition.go is what `when.hint` is allowed to mean, and what a say line may
// carry in place of `{{evidence}}`.
//
// A HINT IS A CONDITION, AND A CONDITION IS JUDGED WITH EVIDENCE OR NOT AT ALL
// (wave 5, ruling R4). A file watch carrying one used to ask the sentinel with
// nothing in front of it, so "any .md change inside a client subfolder" was
// answered "no changes" while the changes sat on disk, and the watch could never
// fire (the chat scoreboard's nested case). Now a file watch's condition is
// shown the reading's change list, the sizes on both sides, and how each changed
// file now ends; a probe's is shown the probe's output, as it always was. The
// kinds that gather nothing — a moment, a rhythm, a quiet machine, a rule — are
// refused one at setup, naming the field, because a judgment of words alone is
// a guess the person is billed for every time it is made.
//
// A SAY LINE IS WHAT A PERSON READS (ruling R8). `{{evidence}}` is its one
// placeholder, and it becomes ONE LINE — "2 files added: support/t1.md,
// support/t2.md" — never the reading it was measured from. Any other `{{…}}`
// is refused at setup, since nothing would ever fill it and the person would be
// sent the braces.
//
// Both refusals are asked through [Item.CheckWatch], which is the one check the
// chat's card, `aforge standing add` (by way of [Store.Create]) and an edit
// (by way of [Store.Revise]) all pass through before anything stands.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// EvidencePlaceholder is the only placeholder a say line may carry. A firing
// replaces it with one line naming what changed or what the check found.
const EvidencePlaceholder = "{{evidence}}"

// placeholderPattern finds anything spelled like a placeholder, so that one
// spelled wrongly is refused rather than delivered as braces.
var placeholderPattern = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// THE BOUNDS ON WHAT A CONDITION IS SHOWN (L6: an interactive read is bounded).
// The whole evidence is held to [ProbeClip], the same figure a probe's output
// is held to, because both are one cheap judgment's reading. Inside it the
// change list names at most [conditionChanges] files, and at most
// [conditionExcerpts] of them are opened, each read for its last
// [ConditionExcerpt] bytes only — a condition on a folder of ten thousand files
// reads four kilobytes of them, never the folder.
const (
	// ConditionExcerpt is how much of the end of one changed file a condition
	// is shown. The end is read because that is where a thread, a log or a
	// notes file grows.
	ConditionExcerpt  = 1 << 10
	conditionExcerpts = 4
	conditionChanges  = 40
)

// summaryPaths is how many paths a say line names before it counts the rest,
// and summaryClip is the most that line may hold.
const (
	summaryPaths = 3
	summaryClip  = 240
)

// conditionUnjudged is the check line of an item that already stands with a
// condition nothing can answer — made before the refusal below existed.
const conditionUnjudged = "it has a condition with nothing to judge it against, so it waits — set it up again without the condition"

// gathers answers whether a waking kind brings evidence a condition can be
// judged against: a file watch's reading, or a probe's output.
func (k WhenKind) gathers() bool { return k == WhenFile || k == WhenProbe }

// nothingGathered is the refusal's middle clause for a kind that gathers
// nothing, with the article its name takes.
func nothingGathered(kind WhenKind) string {
	article := "a "
	if kind != "" && strings.ContainsRune("aeiou", rune(kind[0])) {
		article = "an "
	}
	return article + string(kind) + " item gathers nothing"
}

// checkCondition refuses a hint on a kind with nothing to judge it against.
func (it Item) checkCondition() error {
	if strings.TrimSpace(it.When.Hint) == "" || it.When.Kind.gathers() {
		return nil
	}
	return fmt.Errorf("when.hint is a condition, judged against what a file watch saw change or what a probe found; %s to judge it against — leave when.hint out, or use kind file or probe", nothingGathered(it.When.Kind))
}

// checkSay refuses a say line whose placeholders nothing will fill.
func (it Item) checkSay() error {
	if it.Does.Kind != ActionSay {
		return nil
	}
	for _, found := range placeholderPattern.FindAllString(it.Does.Say, -1) {
		if found != EvidencePlaceholder {
			return fmt.Errorf("does.say uses %s, which nothing fills — the only placeholder is %s, and it becomes one line naming what changed or what the check found", found, EvidencePlaceholder)
		}
	}
	if strings.Contains(it.Does.Say, EvidencePlaceholder) && !it.When.Kind.gathers() {
		return fmt.Errorf("does.say uses %s, but %s to put there — leave it out", EvidencePlaceholder, nothingGathered(it.When.Kind))
	}
	return nil
}

// changesSummary is a file watch's change list as the one line a say carries:
// "1 file added: support/t1.md", "3 files changed: a.md (added), b.md
// (modified), c.md (removed)", then "and N more".
func changesSummary(changes []Change, unknown bool) string {
	if unknown || len(changes) == 0 {
		return "the files it watches changed"
	}
	kind := changes[0].Kind
	for _, change := range changes[1:] {
		if change.Kind != kind {
			kind = "changed"
			break
		}
	}
	noun := "files"
	if len(changes) == 1 {
		noun = "file"
	}
	names := make([]string, 0, summaryPaths)
	for _, change := range changes[:min(len(changes), summaryPaths)] {
		name := change.Path
		if kind == "changed" {
			name += " (" + change.Kind + ")"
		}
		names = append(names, name)
	}
	line := fmt.Sprintf("%d %s %s: %s", len(changes), noun, kind, strings.Join(names, ", "))
	if more := len(changes) - summaryPaths; more > 0 {
		line += fmt.Sprintf(" and %d more", more)
	}
	return shorten(line, summaryClip)
}

// probeSummary is a probe's finding as the one line a say carries: the
// judgment's own sentence, which is what the probe found said plainly, or the
// last line the probe printed when the judgment said nothing more than yes.
func probeSummary(line, output string) string {
	if said := oneLine(line); said != "" {
		return shorten(said, summaryClip)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	return shorten(lines[len(lines)-1], summaryClip)
}

// conditionEvidence is what a file watch's condition is judged against: the
// change list with sizes, then how each changed file now ends. A reading that
// cannot be compared is said to be one, and the listing stands in for it.
func conditionEvidence(workspace string, changes []Change, before, now map[string]fileEntry, unknown bool, listing string) string {
	var out strings.Builder
	if unknown {
		out.WriteString("WHAT CHANGED: not known — the previous reading is not available. EVERY MATCHING FILE NOW:\n" + listing)
		return clipHead(out.String(), ProbeClip)
	}
	fmt.Fprintf(&out, "WHAT CHANGED SINCE THE LAST READING (%d):\n", len(changes))
	for i, change := range changes {
		if i == conditionChanges {
			fmt.Fprintf(&out, "and %d more\n", len(changes)-i)
			break
		}
		out.WriteString(change.Kind + "  " + change.Path + "  " + sizeWords(change, before, now) + "\n")
	}
	shown := 0
	for _, change := range changes {
		if change.Kind == "removed" || shown == conditionExcerpts {
			continue
		}
		text, ok := excerpt(workspace, change.Path)
		if !ok {
			continue
		}
		if shown == 0 {
			fmt.Fprintf(&out, "\nHOW EACH CHANGED FILE ENDS NOW (its last %d bytes at most):\n", ConditionExcerpt)
		}
		out.WriteString("--- " + change.Path + "\n" + text + "\n")
		shown++
	}
	return clipHead(out.String(), ProbeClip)
}

// sizeWords is one change's sizes: what it is now, what it was, or both.
func sizeWords(change Change, before, now map[string]fileEntry) string {
	switch change.Kind {
	case "added":
		return fmt.Sprintf("%d bytes", now[change.Path].Size)
	case "removed":
		return fmt.Sprintf("was %d bytes", before[change.Path].Size)
	}
	return fmt.Sprintf("%d → %d bytes", before[change.Path].Size, now[change.Path].Size)
}

// excerpt is the last [ConditionExcerpt] bytes of one changed file. A folder
// says nothing; anything else that is not a file — a pipe, a socket, a device
// — is named as [notRegular] and never opened.
//
// A FILE OUTSIDE THE PROJECT IS NEVER SHOWN. The judgment is a model call, and
// a link inside a watched folder that leads to somebody's keys is a file the
// work itself could not read; the condition is held to the same border and is
// shown the file's name and size instead. The link is resolved ONCE, and the
// resolved target is what is looked at and opened — without following a link
// ([noFollow]), so a link put in the target's place after the check is refused
// rather than read. A folder ABOVE the target swapped for a link in that
// moment is not caught; that would take resolving beneath the project on every
// component, which this package has no call for.
func excerpt(workspace, name string) (string, bool) {
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, name)
	}
	target, inside := resolveInside(workspace, path)
	if !inside {
		return "", false
	}
	info, err := os.Stat(target)
	switch {
	case err != nil || info.IsDir():
		return "", false
	case !info.Mode().IsRegular():
		return notRegular, true
	}
	file, err := openRegular(target, noFollow)
	if errors.Is(err, errNotRegular) {
		return notRegular, true
	}
	if err != nil {
		return "", false
	}
	defer file.Close()
	offset := max(0, info.Size()-ConditionExcerpt)
	data := make([]byte, ConditionExcerpt)
	n, _ := file.ReadAt(data, offset)
	data = data[:n]
	if bytes.IndexByte(data, 0) >= 0 {
		return "(not text, not shown)", true
	}
	text := strings.ToValidUTF8(string(data), "")
	if offset > 0 {
		text = "…" + text
	}
	return text, true
}

// notRegular stands in the evidence for a changed entry that is not a file.
const notRegular = "(not a regular file, not shown)"

// resolveInside answers path with its links followed, and whether that is
// inside workspace.
func resolveInside(workspace, path string) (string, bool) {
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", false
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(root, target)
	inside := err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
	return target, inside
}

// clipHead keeps the start of a text, because a condition's evidence puts the
// change list first and the list is what matters most.
func clipHead(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return strings.ToValidUTF8(text[:limit], "") + "\n…"
}
