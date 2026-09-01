package session

// THE GROUND MOVING UNDER A RUN.
//
// A task reads a file, thinks for twenty minutes, and writes over the top of a
// change that landed while it was thinking. The work is correct against a world
// that stopped being true, and nothing anywhere notices: the run finished, the
// check passed, the branch merged, and the person finds out days later.
//
// Nothing tracks what a task READ, and nothing ever will cheaply — reads are
// numerous and mostly uninteresting (docs/coordination-design.md says so out
// loud). So the earlier warning, the one a preflight could give before the money
// is spent, cannot see this case at all. LAND TIME IS WHERE IT IS CAUGHT, and
// this file is the catching: at the instant a node would merge, the paths it
// wrote are held up against the two places other work leaves marks —
//
//   - the project's index, for work that LANDED in those paths while this node
//     was running ([LandedTouching] with the node's own start as the moment), and
//   - every other window's live claims, for work that is IN those paths right
//     now ([Elsewhere.Touching]).
//
// ── WHAT IT DOES ABOUT IT ──
//
// It does not resolve anything, it does not rebase anything, and it does not
// invent a state. The node lands in the one that already exists for "finished,
// but somebody should look" — [TaskUnverified] — with its branch kept and a
// reason in the person's own words at the top of its report. From there every
// surface, every note and every bubbling path is the one an unverified landing
// already takes, which is the whole reason this reuses the state rather than
// growing a sibling for it.
//
// ── THREE LAWS ──
//
//   - A CLAIM OF OVERLAP REQUIRES ACTUAL OVERLAP. Both doors this asks answer
//     with two lists, what overlaps and what could not be asked, and only the
//     first is read here. Work that named no files is work nobody can say
//     anything about, and a flag raised on a silence would be a flag that fires
//     on every old row in the file. The cautious wording belongs to the
//     preflight, which is spending nothing when it guesses wrong; at land time a
//     false alarm teaches a person to ignore the one that is real.
//
//   - THE NODE'S OWN FAMILY IS NOT SOMEBODY ELSE. A sub-task branches off its
//     parent's worktree and merges back into it, so a child landing in a file
//     its parent also wrote is the design working, not the ground moving.
//     Descendants are dropped before the question is asked.
//
//   - NOTHING FOUND CHANGES NOTHING. No overlap and the landing is exactly the
//     landing it was: merged, done, and silent. That is the emptiness law where
//     it costs the most to break — a warning nobody needed, standing between a
//     person and work that was fine.

import (
	"strconv"
	"strings"
	"time"
)

// groundListNamed is how many things a list names before it starts counting.
// Two, because these lines are read at a glance on a card and the third name is
// already past the point where a person is reading rather than scanning.
const groundListNamed = 2

// groundShift is the reason this node should not settle quietly, and the empty
// string when it should.
//
// wrote is the node's own leavings — the paths it changed, repo-relative and
// slash-spelled, exactly as [changedPath] made them. A node that wrote nothing
// cannot have written over anybody, and it is answered without a single read.
//
// IT READS THE FILE AND NOT [Agent.TaskIndex]. The merged view replaces a landed
// row with one rebuilt out of the live graph, and a rebuilt row's EndedAt is
// NOW — so every task this session has ever finished would look like it landed
// during this run. The durable rows carry the stamps that were true when they
// were written, which is the only reading a window question can be asked of.
func (a *Agent) groundShift(node *TaskNode, wrote []string) string {
	if node == nil || len(wrote) == 0 {
		return ""
	}
	// THE QUESTION IS ABOUT WHAT WOULD SHIP, and for a node that handed work out
	// that is the family's ledger rather than its own worker's slice of it
	// (task_ledger.go). It is folded HERE rather than by the caller because this
	// is the caller's own question: every road that asks it hands over the list
	// it was holding, and the landing that follows folds again for itself.
	wrote = absorbedLedger(node, wrote)
	after := node.runStart()
	if after.IsZero() {
		// NO WINDOW, NO QUESTION. A node whose start nothing knows cannot say
		// which side of it anything landed on, and a check that guessed the
		// moment would be flagging work on the strength of an invented clock.
		return ""
	}
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	rows := groundOthers(ReadTaskIndex(a.config.taskIndexFile()), session, node.family())
	return groundShiftReason(rows, a.Elsewhere(), wrote, after)
}

// groundOthers drops this node's own family out of the project's rows, on the
// pair both files spell the same way — (SessionID, ID). See this file's second
// law for why a descendant's landing is not the ground moving.
func groundOthers(rows []TaskIndexEntry, session string, own map[string]bool) []TaskIndexEntry {
	if len(rows) == 0 || len(own) == 0 {
		return rows
	}
	out := rows[:0:0]
	for _, row := range rows {
		if row.SessionID == session && own[row.ID] {
			continue
		}
		out = append(out, row)
	}
	return out
}

// groundShiftReason is the whole of the judgement, with the reading handed in.
//
// It is pure — rows in, claims in, one sentence or two out — so that the wording
// a person reads can be held to its own tests without a graph, a worktree or a
// clock behind it.
//
// THE TWO SOURCES GET TWO SENTENCES, because they are two different facts and
// one sentence over both would have to lie about one of them. A landed row is
// work that FINISHED in these files inside this node's window: "changed … while
// this ran" is exactly true of it. A live claim is a window that has already
// written those paths and is still going, and nothing says WHEN it wrote them —
// so it gets the present tense it has earned and no more.
func groundShiftReason(rows []TaskIndexEntry, elsewhere Elsewhere, files []string, after time.Time) string {
	if len(files) == 0 || after.IsZero() {
		return ""
	}
	var lines []string
	// The unknown half of both answers is deliberately dropped on the floor; see
	// this file's first law.
	if landed, _ := LandedTouching(rows, files, after); len(landed) > 0 {
		var who, shared []string
		for _, row := range landed {
			who = append(who, groundName(row.Title, "other work"))
			shared = append(shared, SharedFiles(files, row.Files)...)
		}
		lines = append(lines, groundList(who)+" changed "+groundList(groundPaths(shared))+" while this ran")
	}
	if touching, _ := elsewhere.Touching(files); len(touching) > 0 {
		var who, shared []string
		for _, at := range touching {
			who = append(who, groundName(at.Task.Title, groundName(at.Session, "another window")))
			shared = append(shared, SharedFiles(files, at.Task.Files)...)
		}
		lines = append(lines, groundList(who)+" is also working in "+groundList(groundPaths(shared)))
	}
	return strings.Join(lines, "\n")
}

// groundName is what to CALL one piece of work: its own title, in quotes,
// falling back to the plain phrase for work nothing ever named. The fallback is
// not quoted, because quotation marks around "another window" would read as the
// name of a window somebody called that.
func groundName(title, fallback string) string {
	if title = strings.TrimSpace(title); title != "" {
		return `"` + title + `"`
	}
	return fallback
}

// groundPaths is the paths from every source, in THIS node's own order, with
// repeats dropped — two other tasks in one file is one file.
func groundPaths(shared []string) []string {
	var out []string
	seen := make(map[string]bool, len(shared))
	for _, path := range shared {
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

// groundList spells a set of things as a person would say it, and STOPS
// COUNTING OUT LOUD after two. Repeats collapse first, so two unnamed windows
// read as one phrase rather than as "another window and another window".
//
// It is one function for the paths and for the names on purpose: a list that was
// spelled twice would be two sets of commas to keep in step.
func groundList(items []string) string {
	var out []string
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	switch len(out) {
	case 0:
		return ""
	case 1:
		return out[0]
	case 2:
		return out[0] + " and " + out[1]
	}
	return strings.Join(out[:groundListNamed], ", ") + " and " +
		strconv.Itoa(len(out)-groundListNamed) + " more"
}

// runStart is when this node's run began, and the ZERO TIME when nothing knows.
//
// A node that started in this process has the instant it was marked running. A
// node rehydrated from a checkpoint has no such instant — it has only the age it
// had when the checkpoint was written — so its window is measured back from now,
// which lands LATER than the truth and can only ever miss an overlap rather than
// invent one. That direction is chosen deliberately: this file's first law is
// that a false alarm costs more than a quiet miss.
func (n *TaskNode) runStart() time.Time {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if !n.started.IsZero() {
		return n.started
	}
	if n.elapsed > 0 {
		return time.Now().Add(-n.elapsed)
	}
	return time.Time{}
}

// family is this node's id and every id underneath it, spelled the way the index
// spells one, so a row can be matched without parsing anything.
func (n *TaskNode) family() map[string]bool {
	out := map[string]bool{strconv.FormatUint(n.id, 10): true}
	queue := []uint64{n.id}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		// children takes the graph's lock itself and gives it back, so this walk
		// holds nothing between rungs.
		for _, kid := range n.graph.children(id) {
			key := strconv.FormatUint(kid.id, 10)
			if out[key] {
				continue
			}
			out[key] = true
			queue = append(queue, kid.id)
		}
	}
	return out
}
