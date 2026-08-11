package chat

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
)

// The `@` grammar's wiring (5.18): what this room can address, and what a send
// that addressed something does.
//
// internal/tui2/composer owns the token, the filter and the two send chords. It
// knows the user addressed a target, whether that target was settled, and
// whether they asked to follow — and it deliberately knows nothing about what a
// dispatch DOES. That is here, and it is two doors that already exist: a live
// target takes the steer door every work row's composer takes, and a settled
// one takes the room's own post door with the task named as context.
//
// The one rule that shapes everything below is 5.18's blunt one. A settled
// target NEVER receives direct injection, because a dead thread has nobody to
// absorb it, and a composer that appeared to speak into one would be the
// affordance lying — the same lie 5.15's disabled settled composer prevents
// from the other direction.

// mentionTargets is [composer.Options.Targets]: a cheap snapshot of what may be
// addressed right now.
//
// Cheap is a requirement rather than an aspiration. It is called when a filter
// opens and thereafter on every edit while the draft holds an '@', so it reads
// the rail's already-built home scope — one slice walk, no store call — exactly
// as the palette's own catalog does. The scope source is rebuilt once per
// journal move and never per keystroke, so this is a projection of a projection
// and costs one allocation.
//
// A target that leaves the rail stops being addressable the moment it does,
// which is the property the composer's derived-mention design buys: the
// affordance cannot outlive the thing it addresses (5.20, 12.5).
func (a *App) mentionTargets() []composer.Target {
	if a.source == nil || !a.source.ready {
		return nil
	}
	rows := a.source.home.Rows
	out := make([]composer.Target, 0, len(rows))
	words := make(map[string]int, len(rows))
	for i := range rows {
		row := rows[i]
		if !strings.HasPrefix(row.ID, rowTaskPrefix) {
			continue
		}
		out = append(out, mentionTarget(row, words))
	}
	return out
}

// mentionTarget is one rail row as an addressable thing.
//
// Seed is the rail's own hue assignment and not a re-derivation: the rail paints
// a card from [blocks.Seed] of the job root's id, and a filter row that hashed
// the ROW id instead would give the same task two colours on one screen. 5.16's
// identity axis is only worth having if it is the same everywhere.
func mentionTarget(row rail.Row, words map[string]int) composer.Target {
	return composer.Target{
		ID:    row.ID,
		Word:  uniqueWord(taskWord(row.Name), words),
		Title: strings.TrimSpace(row.Name),
		Seed:  blocks.Seed(row.Seed),
		// The amber `?` is 5.16's one meaning: something is waiting on a human.
		// It is the same count the card and the footer read, so a badge cannot
		// appear in one place and not the other.
		Attention: row.Questions > 0,
		Settled:   row.Life.Terminal(),
	}
}

// maxWordSegments and maxWordCells bound the token a mention completes to. A
// word is a HANDLE — it is typed, matched and read back out of the draft — so it
// has to be short enough to type and stable enough to match, which a whole task
// title is neither.
const (
	maxWordSegments = 3
	maxWordCells    = 20
)

// taskWord derives the short word a task is addressed by.
//
// It is derived rather than stored because nothing in the graph carries one: a
// node has a title, a brief and an id, and the id is the one thing 5.14 forbids
// showing. Deriving it from the title means the word a reader types is the word
// they can see on the card, which is the only property a handle needs.
//
// The rule is deliberately dull: lowercase, runs of anything that is not a
// letter or a digit become one hyphen, and the first few segments survive. A
// title that reduces to nothing keeps the whole product honest by yielding no
// word at all — the composer skips a target whose Word is empty rather than
// offering a token that can never be matched back out of the draft.
func taskWord(title string) string {
	var b strings.Builder
	segments, pending := 0, false
	for _, r := range strings.TrimSpace(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pending {
				if segments >= maxWordSegments || b.Len() >= maxWordCells {
					break
				}
				b.WriteByte('-')
				pending = false
			}
			if b.Len() >= maxWordCells {
				break
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		if b.Len() > 0 && !pending {
			pending = true
			segments++
		}
	}
	return b.String()
}

// uniqueWord keeps two tasks from answering to one token.
//
// A mention is matched by exact word, so a duplicate would silently address
// whichever target came first and the reader would have no way to reach the
// other. The suffix is ugly and the alternative is a dispatch going somewhere
// the user did not point at, which is not a trade.
func uniqueWord(word string, seen map[string]int) string {
	if word == "" {
		return ""
	}
	seen[word]++
	if n := seen[word]; n > 1 {
		return word + "-" + itoa(n)
	}
	return word
}

// -- what a dispatch does ------------------------------------------------------

// dispatchCmd routes one addressed send (5.18).
//
// Three branches and no fourth. A LIVE target takes the steer door — the same
// journal verb the steer line uses, so a worker's steering mailbox stays one
// mailbox and not two. A SETTLED target takes the room's own post door with the
// task named as context, because the routing decision the `@` recorded is
// "about this", not "to this". A target that has since left the rail takes the
// post door unchanged: the words the reader typed are never dropped on the
// floor because the thing they named finished while they were typing.
func (a *App) dispatchCmd(dispatch composer.Dispatch) tea.Cmd {
	node := strings.TrimPrefix(dispatch.TargetID, rowTaskPrefix)
	if node == "" || node == dispatch.TargetID {
		return a.postCmd(dispatch.Text)
	}
	var cmd tea.Cmd
	if dispatch.Settled {
		cmd = a.postCmd(aboutText(a.mentionWord(dispatch.TargetID), dispatch.Text))
	} else {
		cmd = a.steerNode(node, dispatch.Text)
	}
	if !dispatch.Follow {
		return cmd
	}
	// The power chord (5.18): enter sends and stays, ctrl+enter sends and
	// follows. Following is the rail's own selection and nothing else, so a
	// followed send lands the reader exactly where walking there would have.
	return tea.Batch(cmd, a.jumpTo(dispatch.TargetID))
}

// mentionWord is the word the draft addressed, read back out of the same
// snapshot the filter offered. It is asked for rather than carried on the
// [composer.Dispatch] because the dispatch names an ID and IDs are what this
// side routes on; the word is for the sentence a human reads.
func (a *App) mentionWord(id string) string {
	for _, target := range a.mentionTargets() {
		if target.ID == id {
			return target.Word
		}
	}
	return ""
}

// aboutText is 5.18's referenced-context form: the message goes to the main
// head WITH the task named, rather than into a thread that has nobody left to
// absorb it.
//
// The leading mention token is folded into the sentence rather than left beside
// it, because "about perf-audit: @perf-audit what happened" says the same thing
// twice and the second one looks like an address that was not honoured. A
// mention further into the sentence is left exactly where the reader put it.
func aboutText(word, text string) string {
	text = strings.TrimSpace(text)
	if word == "" {
		return text
	}
	if rest, cut := strings.CutPrefix(text, "@"+word); cut {
		if trimmed := strings.TrimSpace(rest); trimmed != "" {
			text = trimmed
		}
	}
	return "about " + word + ": " + text
}
