package tui3

// planrail.go draws A RUN'S TASKS THE WAY EVERY OTHER TASK IS DRAWN.
//
// A run's parts are rows of its plan store and not nodes of this window's graph,
// and for a while they had a renderer of their own: a still half-circle where
// every other working row has the spinner, no handle, no clock and price line,
// and the finished parts of a family folded into one `✓ N done` count. Two
// shapes for one kind of thing is a column a person has to learn twice, and the
// owner's ruling on it was plain — every task looks the same.
//
// SO THERE IS ONE RENDERER AND THIS FILE IS ITS ADAPTER. A store row is lent a
// [taskNode] ([planRailNode]) carrying only what the store knows — its title,
// its state, when it started, what it has cost, and the step it is running —
// and that node is drawn by [app.railEntryRow], the function every node row on
// the side column is drawn by: one line, its glyph, its name and its time. A figure the store does not keep, such as tokens or
// the model, is left unset, and the renderer's emptiness law draws nothing for
// it rather than a zero.

import (
	"encoding/json"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// planRailNodeBit marks a node id as LENT to a store row. Every id the engine
// hands out is a small counter, so an id with the top bit set can never be one
// of them, and the maps this surface keys on node ids — the folds, the hover,
// the rungs — cannot mistake a lent node for a real one.
const planRailNodeBit = uint64(1) << 63

// planRailNodeID is the lent node's id: stable for the row's whole life, so a
// row drawn on two frames is one row, and never an engine id.
func planRailNodeID(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return planRailNodeBit | h.Sum64()>>1
}

// planRailHandle is the `#id` a store row wears: the store's own name for the
// task without the `t-` every stored id is spelled with on this side
// (session's planStoreID), so a run the person was answered with as task 6
// reads `#6`, exactly as the node row for it did.
func planRailHandle(id string) string {
	return strings.TrimPrefix(strings.TrimSpace(id), "t-")
}

// planNodeState is the node state a store row is drawn with. It decides which
// under-block the renderer builds ([app.railUnder]) and which group the row is
// counted in, and it is the same reading [planStatus] gives the mark.
func planNodeState(row session.PlanTaskRow) session.TaskState {
	if row.Stopped {
		return session.TaskFailed
	}
	if row.Interrupted {
		return session.TaskInterrupted
	}
	if row.Hold != "" && (row.Status == "ready" || row.Status == "running") {
		return session.TaskQueued
	}
	switch strings.TrimSpace(row.Status) {
	case "pending":
		return session.TaskQueued
	case "ready", "claimed", "running":
		return session.TaskRunning
	case "done":
		return session.TaskDone
	case "failed", "cancelled":
		return session.TaskFailed
	case "paused":
		return session.TaskUnverified
	}
	return ""
}

// planNodeStatus is [planStatus] with the node state beside it, which is what
// [app.taskStatus] answers for a lent node.
func planNodeStatus(row session.PlanTaskRow) session.TaskStatus {
	status := planStatus(row)
	status.State = planNodeState(row)
	return status
}

// planRailNode lends one store row a node, holding exactly what the store
// knows about it and nothing else.
//
// THE LIVE STEP IS THE NODE'S CURRENT CALL. The store publishes the command a
// worker is running and when it started ([session.PlanTaskRow.Live]), which is
// the fact the pilot lane gives a node row, so it goes where that fact goes and
// the renderer draws it as it draws any call in flight: `bash go test ./…` with
// its clock. A row between steps has no call, and draws none.
func planRailNode(row session.PlanTaskRow) *taskNode {
	held := row
	node := &taskNode{
		id:       planRailNodeID(row.ID),
		label:    strings.TrimSpace(row.Title),
		planTask: strings.TrimSpace(row.ID),
		planRow:  &held,
		handle:   planRailHandle(row.ID),
		state:    planNodeState(row),
		waiting:  strings.TrimSpace(row.Hold),
		stopped:  row.Stopped,
		began:    row.Started,
		started:  row.Started,
		ended:    row.Ended,
		cost:     row.USD,
		// AND WHICH PROGRAM HAS THE WORK, so the row wears its badge
		// (programbadge.go) like the node row it stands in for.
		program: strings.TrimSpace(row.Program),
	}
	node.title = taskTitleOf(node.label, "", node.id)
	if row.Live.Step > 0 {
		if command := strings.TrimSpace(planDisplayCommand(row.Live.Command, row.LiveParts)); command != "" {
			args, _ := json.Marshal(map[string]string{"command": command})
			node.tool, node.toolBegan = taskCallWord("bash", string(args), ""), row.Live.Since
		}
	}
	return node
}

// planTwig is one store row and the rows under it, in the order the rail
// draws them.
type planTwig struct {
	row  session.PlanTaskRow
	kids []*planTwig
}

// planRailForest is this reading's store rows as trees, in the rail's order:
// running work first, then the newest, and store order inside a family with the
// running part floated to the top ([tasksReading.railTree]).
//
// A row whose parent this reading does not hold is a tree of its own. That is
// the ordinary shape of a run somebody's node row carries: the reading leaves
// the run's own row to that node ([planRowShown]), and its parts arrive here
// without the row they hang from, to be hung under the node by the column.
//
// `store` is the rows in the store's own order, which is the order the parts
// were created in; with none the reading's own order stands in for it.
func (r tasksReading) planRailForest(store []session.PlanTaskRow) []*planTwig {
	items := make([]tasksItem, 0, len(r.items))
	for _, item := range r.items {
		if item.plan != nil {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil
	}
	plan := r
	plan.items, plan.held, plan.whole = items, len(items), len(items)
	plan.chats, plan.shape = nil, nil
	tree := plan.railTree()
	// A FAMILY KEEPS ITS CREATION ORDER, which is the node tree's own law
	// ([app.railForest]): a part that starts running does not jump over the
	// siblings it was created after, so a family's shape holds still while it
	// is read. The families themselves stand running first ([tasksReading.railTree]).
	position := make(map[string]int, len(items))
	for i, row := range store {
		position[strings.TrimSpace(row.ID)] = i
	}
	if len(position) == 0 {
		for i, item := range items {
			position[strings.TrimSpace(item.plan.ID)] = i
		}
	}
	seen := make(map[string]bool, len(items))
	var grow func(item tasksItem) *planTwig
	grow = func(item tasksItem) *planTwig {
		twig := &planTwig{row: *item.plan}
		seen[strings.TrimSpace(item.plan.ID)] = true
		kids := append([]tasksItem(nil), tree.kids[tasksKeyOf(item.entry)]...)
		sort.SliceStable(kids, func(i, j int) bool {
			return planKidPosition(kids[i], position) < planKidPosition(kids[j], position)
		})
		for _, kid := range kids {
			if kid.plan == nil || seen[strings.TrimSpace(kid.plan.ID)] {
				continue
			}
			twig.kids = append(twig.kids, grow(kid))
		}
		return twig
	}
	var out []*planTwig
	for _, group := range tree.groups {
		for _, root := range group.roots {
			if root.plan == nil || seen[strings.TrimSpace(root.plan.ID)] {
				continue
			}
			out = append(out, grow(root))
		}
	}
	return out
}

// planKidPosition is where a part stands in the store's own order, and last
// for a row the store did not hand over.
func planKidPosition(item tasksItem, position map[string]int) int {
	if item.plan == nil {
		return len(position)
	}
	if at, ok := position[strings.TrimSpace(item.plan.ID)]; ok {
		return at
	}
	return len(position)
}

// planTwigsOf is the page's children as trees, from the flat list the store
// read hands over (each row's Parent names the row above it, and a row whose
// parent is not in the list hangs from the page's own task).
func planTwigsOf(rows []session.PlanTaskRow) []*planTwig {
	held := make(map[string]*planTwig, len(rows))
	for _, row := range rows {
		held[strings.TrimSpace(row.ID)] = &planTwig{row: row}
	}
	var out []*planTwig
	for _, row := range rows {
		twig := held[strings.TrimSpace(row.ID)]
		if up := held[strings.TrimSpace(row.Parent)]; up != nil && up != twig {
			up.kids = append(up.kids, twig)
			continue
		}
		out = append(out, twig)
	}
	return out
}

// railLeadFloor is the least room a family row may leave for its title after
// the lead: two cells, one glyph and the space that follows it. A row with less
// than that has stopped being a row that names anything, and drawing only the
// bare connector would hand the reader a `├ ` that says "here is a row" over an
// empty name.
const railLeadFloor = 2

// railLevels is how many levels of family a column this wide may hang before a
// deeper path stops eating the cells the name needs. Two cells to a level, so
// the budget is the room a title keeps after a level draws itself; a path past
// it is NAMED AT THE BOUNDARY and not drawn ([app.railRowPlace]: one ellipsis
// row where the levels run out, never a row hung flat at the cap's depth).
func railLevels(width int) int {
	levels := (width - railTitleFloor) / 2
	if levels < 1 {
		levels = 1
	}
	return levels
}

// railLead is the lead cells a family row hangs by: two a level, drawn as the
// tree the manual documents — `├ ` off the parent while a sibling follows the
// row, `└ ` where the row closes its family, `│ ` down every level above it
// whose row had a sibling still to come, and two spaces where a branch has
// ended. `last` is those levels' own answers, shallowest first, the last one
// the row's own level, and a row with none draws nothing.
//
// The glyphs come through the vocabulary's one door ([palette.glyph]) like the
// tasks place draws its own kin with, so one tree shape is spelled one way on
// this surface. THE WIDTH IS MEASURED AND NOT COUNTED, the same bargain
// hometree.go strikes: a level is two cells today, and a row that assumed so
// would draw a broken column the day one of them is respelled — or on the
// terminal where a box-drawing glyph is reported wide.
//
// AN UNDER-ROW DRAWS NO ELBOW. The block a part says under its title stands
// between that row and whatever follows it — the sibling next to it, or its
// own parts — so the same flags with `elbow` false keep the trunk running
// where rows follow and leave air where the branch closed, and the elbow is
// the row's, drawn once.
func (a *app) railLead(last []bool, elbow bool) string {
	var out strings.Builder
	for i, closed := range last {
		if elbow && i == len(last)-1 {
			if closed {
				out.WriteString(a.pal.glyph(tokens.GTreeLast))
			} else {
				out.WriteString(a.pal.glyph(tokens.GTreeBranch))
			}
			out.WriteString(" ")
			continue
		}
		// An ancestor level carries the trunk while its row had a sibling
		// still to come, and the air the branch leaves when it was the last.
		if closed {
			out.WriteString("  ")
			continue
		}
		out.WriteString(a.pal.glyph(tokens.GTreeVert))
		out.WriteString(" ")
	}
	return out.String()
}

// railRowPlace is one family row's place in the tree — the one mechanism both
// renderers hang their rows by: the flags of the levels above the row with its
// own last-child flag, the lead those flags draw, and the room the lead leaves
// a title. A row deeper than the column's levels, or one whose lead leaves
// less than [railLeadFloor] (the day a level draws wider than its two cells,
// [app.railLead]'s own warning), is PAST the column: the caller names the
// branch at the boundary with [app.railBoundaryRow] and draws nothing of it,
// so a deep family is never flattened to the cap's depth — a child hung beside
// its parent — and never hidden without a row that says it is there.
func (a *app) railRowPlace(last []bool, i, n, width, levels int) (hang []bool, lead string, room int, past bool) {
	at := make([]bool, len(last), len(last)+1)
	copy(at, last)
	at = append(at, i == n-1)
	hang = at
	if len(hang) > levels {
		hang = hang[len(hang)-levels:]
	}
	lead = a.railLead(hang, true)
	room = max(width-ansi.StringWidth(lead), 0)
	return hang, lead, room, len(at) > levels || room < railLeadFloor
}

// railBoundaryRow is the one row that names a branch the column will not draw:
// the branch's own connector and the vocabulary's one ellipsis. It is the door
// onto the task it names, as every drawn line is ([app.openRailPlan]).
func (a *app) railBoundaryRow(lead, id string) railLine {
	return railLine{text: lead + a.pal.dim(a.pal.glyph(tokens.GEllipsis)), entry: -1, plan: id, head: true}
}

// planRailLines draws a run's parts under a row, each one THROUGH THE NODE
// RENDERER and one line each, as every task on the side column is (sidecol.go):
// a level of family costs two cells, drawn as the tree's own connectors
// ([app.railLead]) — `last` is the levels above the parts, each saying whether
// that ancestor was the last child of its own, and the parts' own levels hang
// off the row above them, `├ ` while a sibling follows and `└ ` where one
// closes the family.
//
// Every line a part draws carries its store id, which is what makes it a door
// onto that task's page ([app.openRailPlan]).
func (a *app) planRailLines(kids []*planTwig, last []bool, width, levels int) []railLine {
	var out []railLine
	for i, kid := range kids {
		hang, lead, room, past := a.railRowPlace(last, i, len(kids), width, levels)
		if past {
			out = append(out, a.railBoundaryRow(lead, kid.row.ID))
			continue
		}
		text := a.railEntryRow(railEntry{node: planRailNode(kid.row)}, room)
		out = append(out, railLine{text: lead + text, entry: -1, plan: kid.row.ID, head: true})
		out = append(out, a.planRailLines(kid.kids, hang, width, levels)...)
	}
	return out
}

// planPageLines is [app.planRailLines] for a task's page, which has the room
// the side column gave up: under each part's one line stand the lines the
// column moved to its hint ([app.railUnder]), what the part is doing and what
// it is costing, so the page still names a call in flight the way the rail
// once did beside the row. The page's own task stands in its head, so its
// parts hang a level in here, the same connectors the rail draws and the same
// trunk running through the under-block ([app.railLead]).
func (a *app) planPageLines(kids []*planTwig, last []bool, width, levels int) []railLine {
	var out []railLine
	for i, kid := range kids {
		hang, lead, room, past := a.railRowPlace(last, i, len(kids), width, levels)
		if past {
			out = append(out, a.railBoundaryRow(lead, kid.row.ID))
			continue
		}
		node := planRailNode(kid.row)
		out = append(out, railLine{text: lead + a.railEntryRow(railEntry{node: node}, room), entry: -1, plan: kid.row.ID, head: true})
		for _, under := range a.railUnder(node, max(room-4, 0)) {
			out = append(out, railLine{text: a.railLead(hang, false) + "    " + under, entry: -1, plan: kid.row.ID})
		}
		out = append(out, a.planPageLines(kid.kids, hang, width, levels)...)
	}
	return out
}

// planRailRoot draws one run whose own row no node on the column carries: its
// row, then its parts, every one of them through the node renderer.
func (a *app) planRailRoot(twig *planTwig, width int) []railLine {
	text := a.railEntryRow(railEntry{node: planRailNode(twig.row)}, width)
	out := []railLine{{text: text, entry: -1, plan: twig.row.ID, head: true}}
	return append(out, a.planRailLines(twig.kids, nil, width, railLevels(width))...)
}
