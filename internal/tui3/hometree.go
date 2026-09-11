package tui3

// ── A CONVERSATION'S WORK IS A TREE, AND HOME DREW IT AS A ROW OF STRANGERS ──
//
// A conversation hands work out, and the work it hands out hands more out. The
// index records exactly that — [session.TaskIndexEntry.Parent] is the row that
// asked for this one — and every reader that walks [session.TaskRollup.Rows] as
// a flat slice throws the relationship away. Home threw it away everywhere: the
// card's `work` band, the phone sheet's, the fold at the foot of both. Three
// pieces of ONE run stood side by side as three peers, in whatever order their
// end stamps happened to fall, and the fold — which counts things, not families
// — could cut a child away from the row that is the only thing on the screen
// explaining what it was for.
//
// SO HOME GROWS THE TREE ONCE, HERE, AND EVERY HOME SURFACE DRAWS THE SAME ONE.
// The CONVERSATION is the parent of all of it: it is the row in the list, its
// name is the card's title, and opening it opens the chat. What hangs under that
// is the work — each piece under the piece that asked for it, to whatever depth
// the index recorded — and opening one of THOSE opens that task's own record.
// One heading, one tree, two doors that cannot be confused for each other.
//
// FOUR LAWS COME OUT OF IT, AND THEY ARE WHAT THE TESTS PIN:
//
//   - A CHILD NEVER PRECEDES ITS VISIBLE ANCESTORS. The phone band folds whole
//     families. The desktop preview fits three task names in depth-first order,
//     so a large family stays compact while every visible child has context.
//   - A FAMILY STANDS WHERE ITS MOST URGENT MEMBER PUTS IT: what will not move
//     without a person, then what is still going, then what is over. A CHILD
//     THAT NEEDS SOMEBODY CARRIES ITS WHOLE FAMILY FORWARD, because the row that
//     explains that child is its root, and a surface that promoted the child
//     alone would be showing the answer with the question folded away.
//   - TIES KEEP THE RECORD'S OWN ORDER, which is newest first with live work
//     counted as now (internal/session's [sortTaskIndex]). Nothing here re-dates
//     a row, filters one out or invents one — every entry in the rollup comes
//     out of the walk exactly once, which [homeWorkFamilies] guarantees even for
//     a parent seam that points at nothing or points in a circle.
//   - THE CONNECTORS ARE THE ROSTER'S OWN (task.go's [treeBranch] and its
//     neighbours) and are not re-spelled here. The roster hangs work off work
//     with the plain elbow; home hanging the same work off the same parent with
//     a different mark would be two vocabularies for one fact.

import (
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// homeWorkNameFloor is the cells a task's NAME will not be squeezed below to buy
// room for the connectors in front of it.
//
// A tree drawn so deep that the name beside it is an ellipsis has spent the
// row's whole meaning on saying where the row hangs. Under this floor the
// connectors give way instead ([homeWorkLeadOf]) — first the ancestors, then the
// elbow itself — because "which piece of work is this" survives a lost elbow and
// nothing survives a lost name.
const homeWorkNameFloor = 16

// ── the record, as a tree ───────────────────────────────────────────────────

// homeTaskKey names one row of the index across a conversation's whole rollup:
// the session that ran it and the id it took inside that session. The PAIR is
// the identity — ids restart with every conversation, which is
// [session.TaskIndexEntry.ID]'s own doc — and it is the alphabet the parent seam
// is spoken in.
func homeTaskKey(entry session.TaskIndexEntry) string {
	id := strings.TrimSpace(entry.ID)
	if id == "" {
		return ""
	}
	return strings.TrimSpace(entry.SessionID) + "\x00" + id
}

// homeTaskUpKey is that same name for the row this one was handed out BY, and
// "" for a row nobody handed out. A row written before families entered the
// index decodes with no parent, which is exactly what it was.
func homeTaskUpKey(entry session.TaskIndexEntry) string {
	up := strings.TrimSpace(entry.Parent)
	if up == "" {
		return ""
	}
	return strings.TrimSpace(entry.SessionID) + "\x00" + up
}

// The three ranks a piece of work can hold, and they are the order home puts
// work in everywhere: what is asking for a person first, then what has not
// finished, then what is over.
const (
	homeWorkAsking = iota
	homeWorkMoving
	homeWorkOver
)

// homeWorkRank is where one row stands in that order.
//
// THE JUDGEMENT IS [taskEntryStatus]'S AND NOT A SECOND READING OF THE RECORD.
// Home asks its own liveness question — is the conversation that wrote this row
// still holding the node out ([session.SessionRow.Runs]) — and everything after
// that is the one status vocabulary every task surface answers through.
func homeWorkRank(entry session.TaskIndexEntry, row session.SessionRow) int {
	status := taskEntryStatus(entry, row.Runs(entry))
	switch {
	case status.Attention:
		return homeWorkAsking
	case !status.Settled():
		return homeWorkMoving
	}
	return homeWorkOver
}

// homeWorkTwig is one piece of work and the pieces it handed out.
//
// It is a shape rather than a list of (row, depth) pairs for [railTwig]'s
// reason: every question home asks of it is about a SUBTREE — how urgent is
// anything in here, how many rows is this family standing for — and a
// depth-tagged list answers those by scanning forward for the next row at the
// same depth, which is a tree with its structure taken out and then guessed back.
type homeWorkTwig struct {
	entry session.TaskIndexEntry
	kids  []*homeWorkTwig
	rank  int
}

// homeWorkNode is one row of the walk: the piece of work, where it hangs, and
// whether anything hangs under it.
type homeWorkNode struct {
	entry session.TaskIndexEntry
	// stems is one flag per level above this row, deepest last, each saying
	// whether the node at that level has a sibling still to come — which is the
	// difference between a stem running down past this row and blank air. It is
	// [app.railPrefix]'s alphabet exactly, so the two surfaces draw one tree.
	// Empty means a root.
	stems []bool
	// kids says work hangs UNDER this row, which is what turns the air below its
	// name into a stem: the sentence a task leaves sits between that task's name
	// and its first child, and blank there would put a gap in the one line the
	// eye is following down the family.
	kids bool
}

// depth is how many pieces of work stand above this one.
func (n homeWorkNode) depth() int { return len(n.stems) }

// homeWorkForest is a conversation's work as families: every root, each grown
// whole, in the order home draws them.
//
// EVERY ROW COMES OUT EXACTLY ONCE, whatever the parent seam says. A row whose
// parent is not in this rollup is a root — the parent is in another
// conversation, or has not been written yet, and a row held back for a heading
// that will never arrive is work that simply vanished off the screen. A row in a
// CYCLE is grown where the walk first reaches it, and the guard is not defensive
// tidiness: the seam is a string written by a producer this file does not own,
// and a loop in it would be a frame that never returns rather than one that
// looks wrong (task.go's [railRootOf] states the same law).
func homeWorkForest(row session.SessionRow) []*homeWorkTwig {
	rows := row.Tasks.Rows
	if len(rows) == 0 {
		return nil
	}
	// at is where each row stands in the record. A key claimed twice keeps its
	// FIRST row, which is the newest one: the index is append-only and the record
	// is newest first, so a row rewritten in place is read as the row it became.
	at := make(map[string]int, len(rows))
	for i := range rows {
		key := homeTaskKey(rows[i])
		if key == "" {
			continue
		}
		if _, taken := at[key]; !taken {
			at[key] = i
		}
	}
	kids := make(map[int][]int)
	roots := make([]int, 0, len(rows))
	for i := range rows {
		up, held := at[homeTaskUpKey(rows[i])]
		if !held || up == i {
			roots = append(roots, i)
			continue
		}
		kids[up] = append(kids[up], i)
	}
	grown := make([]bool, len(rows))
	var grow func(int) *homeWorkTwig
	grow = func(i int) *homeWorkTwig {
		grown[i] = true
		twig := &homeWorkTwig{entry: rows[i], rank: homeWorkRank(rows[i], row)}
		for _, kid := range kids[i] {
			if grown[kid] {
				continue
			}
			twig.kids = append(twig.kids, grow(kid))
		}
		// A nested decision brings its whole path ahead of settled siblings.
		for _, kid := range twig.kids {
			twig.rank = min(twig.rank, kid.rank)
		}
		sort.SliceStable(twig.kids, func(a, b int) bool { return twig.kids[a].rank < twig.kids[b].rank })
		return twig
	}
	forest := make([]*homeWorkTwig, 0, len(roots))
	for _, i := range roots {
		forest = append(forest, grow(i))
	}
	// Anything a cycle kept out of every family heads one of its own, in the
	// record's order, so the count on the fold line stays the truth.
	for i := range rows {
		if !grown[i] {
			forest = append(forest, grow(i))
		}
	}
	sort.SliceStable(forest, func(x, y int) bool {
		return homeWorkTwigRank(forest[x], row) < homeWorkTwigRank(forest[y], row)
	})
	return forest
}

// homeWorkTwigRank is the most urgent thing anywhere in a family — its root
// included — which is where the whole family stands in the column.
func homeWorkTwigRank(twig *homeWorkTwig, row session.SessionRow) int {
	return twig.rank
}

// homeWorkFamilies is what every home surface actually draws from: each family
// as a depth-first run of rows, root first, ready to be handed to a fold whole.
func homeWorkFamilies(row session.SessionRow) [][]homeWorkNode {
	forest := homeWorkForest(row)
	if len(forest) == 0 {
		return nil
	}
	families := make([][]homeWorkNode, 0, len(forest))
	for _, twig := range forest {
		families = append(families, homeWorkWalk(twig, nil, nil))
	}
	return families
}

// homeWorkWalk lays one family out, depth first.
func homeWorkWalk(twig *homeWorkTwig, stems []bool, out []homeWorkNode) []homeWorkNode {
	out = append(out, homeWorkNode{
		entry: twig.entry,
		stems: append([]bool(nil), stems...),
		kids:  len(twig.kids) > 0,
	})
	for at, kid := range twig.kids {
		// A FRESH SLICE PER CHILD. Sharing one growing array would let a deep
		// branch overwrite the level a later sibling is about to write, which is
		// the kind of bug that draws a correct tree until the day somebody hands
		// out three pieces from inside a fourth.
		under := make([]bool, len(stems), len(stems)+1)
		copy(under, stems)
		out = homeWorkWalk(kid, append(under, at < len(twig.kids)-1), out)
	}
	return out
}

// homeWorkHidden is how many PIECES OF WORK are behind a fold that shows the
// first `show` families, given what each family is standing for.
//
// THE LINE SAYS TASKS BECAUSE THE PERSON IS DECIDING ABOUT TASKS. `▸ 3 more
// tasks` under a band that folded by family has to count the same things it
// names — a fold that hid two runs of four and said `2 more` would be honest
// about its own machinery and wrong about the screen. Every home surface that
// folds this band asks this one function, so the band and the card cannot
// disagree about a number they both draw.
func homeWorkHidden(sizes []int, show int) int {
	hidden := 0
	for at := show; at < len(sizes); at++ {
		hidden += sizes[at]
	}
	return hidden
}

// ── the tree, as cells ──────────────────────────────────────────────────────

// homeWorkLead is the connectors one row is drawn behind: what goes in front of
// its NAME, what goes in front of that name's own wrapped rows, what goes in
// front of the lines hanging UNDER the name, and the cells each of those costs.
//
// THE THREE ARE NOT ONE STRING AND THE DIFFERENCE IS THE WHOLE POINT. The name's
// last connector is an elbow pointing AT it; everything below it carries a stem
// where the family continues past that row and blank air where it does not, so
// the vertical line the eye follows down a family is unbroken by the sentence
// each task leaves behind (task.go's [app.railUnderStem] is the same reading).
//
// AND `stem` COSTS EXACTLY WHAT `name` COSTS. A row's name is fitted into what
// is left after [homeWorkLead.nameCols], so a wrapped second row of that same
// block has to be given back the same cells or the block is drawn wider than the
// card it is in — which is how a right margin ends up one cell past the edge on
// exactly the rows that have children.
type homeWorkLead struct {
	name      string
	stem      string
	under     string
	nameCols  int
	underCols int
}

// homeWorkLeadOf is the connectors for one node at one width.
//
// A CARD TOO NARROW FOR THE WHOLE TREE STILL SAYS THIS IS UNDER SOMETHING. The
// full prefix costs three cells a level, and home's card is drawn at whatever
// the frame can spare; past four or five levels there would be nothing left of
// the name. So the prefix gives way in two steps — the ancestors first, leaving
// the one elbow that says this row hangs off the row above it, and then the
// elbow too — and the name keeps [homeWorkNameFloor] cells throughout. The row
// is never dropped and the walk is never cut: what a narrow frame loses is the
// DRAWING of the depth, never a piece of work.
func homeWorkLeadOf(node homeWorkNode, width int, pal palette) homeWorkLead {
	if len(node.stems) == 0 && !node.kids {
		return homeWorkLead{}
	}
	if full := homeWorkLeadFull(node, pal); homeWorkLeadFits(full, width) {
		return full
	}
	short := homeWorkLead{}
	if levels := len(node.stems); levels > 0 {
		short.name = homeWorkElbow(node.stems[levels-1], pal)
		short.stem = treeVoid
		short.under = treeVoid
		if node.kids {
			short.under = homeWorkStem(pal)
		}
		short.nameCols = ansi.StringWidth(short.name)
		short.underCols = ansi.StringWidth(short.under)
	}
	if homeWorkLeadFits(short, width) {
		return short
	}
	return homeWorkLead{}
}

// homeWorkLeadFits reports whether a name still has room to say anything behind
// this prefix. Both halves are measured, because the two are drawn on rows of
// one block and the wider of them is what the block costs.
func homeWorkLeadFits(lead homeWorkLead, width int) bool {
	return width-max(lead.nameCols, lead.underCols) >= homeWorkNameFloor
}

// homeWorkLeadFull is the prefix with every level of the tree in it.
func homeWorkLeadFull(node homeWorkNode, pal palette) homeWorkLead {
	var name, stem strings.Builder
	for at, more := range node.stems {
		down := treeVoid
		if more {
			down = homeWorkStem(pal)
		}
		if at == len(node.stems)-1 {
			name.WriteString(homeWorkElbow(more, pal))
		} else {
			name.WriteString(down)
		}
		stem.WriteString(down)
	}
	under := stem.String()
	if node.kids {
		under += homeWorkStem(pal)
	}
	lead := homeWorkLead{name: name.String(), stem: stem.String(), under: under}
	// THE WIDTH IS MEASURED AND NOT COUNTED. Both spellings of a connector are
	// three cells today, and a surface that assumed so would draw a broken column
	// the day one of them is respelled — or on the terminal where a box-drawing
	// glyph is reported wide (rowfit.go keeps the same law for every other
	// margin on this screen).
	lead.nameCols = ansi.StringWidth(lead.name)
	lead.underCols = ansi.StringWidth(lead.under)
	return lead
}

// homeWorkStem and homeWorkElbow are the roster's connectors with the ASCII
// floor applied — the same [palette.ascii] question home's own glyphs are asked
// (homeband_work.go's [homeWorkGlyph]).
func homeWorkStem(pal palette) string {
	if pal.ascii {
		return treeStemASCII
	}
	return treeStem
}

func homeWorkElbow(more bool, pal palette) string {
	if more {
		if pal.ascii {
			return treeBranchASCII
		}
		return treeBranch
	}
	if pal.ascii {
		return treeLastASCII
	}
	return treeLast
}
