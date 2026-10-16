package session

// THE PROJECTION: ONE CONVERSATION'S PERSISTED TASKS AS GENERIC FACTS.
//
// Everything in this file is a pure function of what the conversation put on
// disk — its tasks.json (the nodes it admitted, the rows of its adaptive runs)
// and the plan stores beside it (the sub-tasks of a run, which are real tasks
// the app lists). Nothing is read from a title, a body or the order tasks were
// made in; a link is a stored command or it is not a link. A fact the record
// does not carry is left out: no instant is dated with the clock that read it,
// no price is looked up, no link is guessed.
//
// WHAT THE APP LISTS IS WHAT IS HERE. [Agent.AppTasks] answers three persisted
// roads — the graph's nodes (a quick node is the one exception: it is listed
// elsewhere and kept here only so a link to it resolves), the rows of the
// conversation's adaptive runs, and the rows of its plan stores — and one
// transient road, proposals that have not been approved yet. Those have no
// record and no stable id, so they are the one thing a persisted graph cannot
// hold; [WorkExcluded] says so.

import (
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/workgraph"
)

// WorkUnavailable names the facts the persisted record cannot give. A task's
// STEPS live in the running node's event ring (memory, gone with the process)
// and in its journal (a per-event trace this source never parses: reading it
// per request is the cost the graph exists to avoid), and the person's open
// ASKS live in memory too, where [Blocking.Tasks] names the work by title and
// not by reference. None is in tasks.json, so none is a fact of this graph.
var WorkUnavailable = []string{"steps", "asks"}

// wgElapsed is the frozen age a record kept for work that has ended.
func wgElapsed(ms int64, state TaskState) *int64 {
	if ms <= 0 || (state != TaskDone && state != TaskFailed) {
		return nil
	}
	return &ms
}

// WorkExcluded names the kinds of work the persisted graph does not hold, so
// their absence is a statement and not a silence.
var WorkExcluded = []string{"proposals"}

// The words a record's merge outcome is said in on the graph.
func wgLanding(merge string, ended time.Time) *workgraph.Landing {
	switch merge {
	case mergeMerged, mergeInPlace, mergeKept, mergeConflicted, mergeAborted:
		return &workgraph.Landing{Outcome: merge, At: workgraph.Stamp(ended)}
	}
	return nil
}

// textOf cuts and scrubs words that left the machine's own files: the bound
// first (so a huge field costs a bounded amount), then the redactor over what
// is kept.
func wgText(source, s string, limit int) *workgraph.GraphText {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	cut, was := workgraph.Cut(s, limit)
	return &workgraph.GraphText{Source: source, Text: redact.Secrets(cut), Cut: was}
}

func wgTitle(s string) string {
	cut, _ := workgraph.Cut(strings.TrimSpace(s), workgraph.MaxTitleRunes)
	return redact.Secrets(cut)
}

// filesOf is the paths a task changed (or, for one still running, wrote),
// workspace-relative and slash-spelled. An absolute path or one that leaves
// the workspace is withheld and counted, never shown: a graph is read by more
// than the person whose machine it is.
func wgFiles(changed, wrote []string) workgraph.FileSet {
	source, list := "changed", changed
	if len(list) == 0 {
		source, list = "wrote", wrote
	}
	var out workgraph.FileSet
	seen := make(map[string]bool, len(list))
	for _, raw := range list {
		clean, ok := wgRelPath(raw)
		switch {
		case !ok:
			out.Omitted++
		case seen[clean]:
		default:
			seen[clean] = true
			out.Total++
			if len(out.Paths) < workgraph.MaxFilesPerNode {
				out.Paths = append(out.Paths, redact.Secrets(clean))
			}
		}
	}
	if out.Total+out.Omitted > 0 {
		out.Source = source
	}
	return out
}

// relativePath is a recorded path as a clean workspace-relative one.
func wgRelPath(raw string) (string, bool) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" || strings.HasPrefix(raw, "/") || strings.ContainsRune(raw, 0) || (len(raw) > 1 && raw[1] == ':') {
		return "", false
	}
	clean := path.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

// spendOf is a recorded bill. Dollars recorded make it priced; tokens without
// dollars make it unpriced (the dollars are UNKNOWN, not zero); nothing at all
// makes no bill.
func wgSpend(usd float64, tokens int) *workgraph.TaskSpend {
	switch {
	case usd > 0:
		return &workgraph.TaskSpend{USD: usd, Tokens: tokens, Basis: "priced"}
	case tokens > 0:
		return &workgraph.TaskSpend{Tokens: tokens, Basis: "unpriced"}
	}
	return nil
}

// checksOf is the recorded runs of a task's declared checks, from a verdict
// basis that rested on running them. A basis that read the work, or carries no
// run, records no check.
func wgChecks(basis *plandb.VerdictBasis) *workgraph.TaskChecks {
	if basis == nil || basis.Kind != "run" || len(basis.Runs) == 0 {
		return nil
	}
	out := &workgraph.TaskChecks{}
	for _, run := range basis.Runs {
		if run.ExitCode == 0 {
			out.Passed++
		} else {
			out.Failed++
		}
	}
	return out
}

func wgTokens(r taskRecord) int { return r.Input + r.Output + r.CacheRead + r.CacheWrite }

func wgRef(owner string, id uint64) workgraph.TaskRef {
	return workgraph.TaskRef{Owner: owner, Task: strconv.FormatUint(id, 10)}
}

func wgID(owner string, id uint64) string { return wgRef(owner, id).ID() }

// instructionsOf is the words a task was given, with where they were read: the
// person's edit, then what it was admitted with, then the person's own message.
func wgInstructions(r taskRecord) *workgraph.GraphText {
	if t := wgText("task_brief", r.App.Brief, workgraph.MaxTextRunes); t != nil {
		return t
	}
	if t := wgText("admitted_brief", r.Brief, workgraph.MaxTextRunes); t != nil {
		t.Anchor = wgAnchor(r.OriginJournal, r.OriginLine)
		return t
	}
	t := wgText("request", r.Request, workgraph.MaxTextRunes)
	if t != nil {
		t.Anchor = wgAnchor(r.OriginJournal, r.OriginLine)
	}
	return t
}

// wgAnchor is the pointer a record keeps to the person's original words: the
// conversation whose journal it names and the line. A pointer with no line, or
// to a journal that is not some conversation's own, is not an anchor.
func wgAnchor(journal string, line int) *workgraph.TextAnchor {
	if line <= 0 || filepath.Base(journal) != placeTranscript {
		return nil
	}
	agent := filepath.Base(filepath.Dir(journal))
	if !workgraph.ValidOwner(agent) {
		return nil
	}
	return &workgraph.TextAnchor{Agent: agent, Line: line}
}

// wgRoute is the crew a run row was accepted onto, role by role.
// wgPrice is the accepted route's published tariff as the record stored it; nil
// stays nil, which the graph says as unknown.
func wgPrice(p *crewroute.RoutePrice) *workgraph.RoutePrice {
	if p == nil {
		return nil
	}
	return &workgraph.RoutePrice{Prompt: p.Prompt, Completion: p.Completion, CacheRead: p.CacheRead}
}

func wgRoute(rec *TaskCrewRecord) []workgraph.RouteSeat {
	if rec == nil || !rec.Routed {
		return nil
	}
	var out []workgraph.RouteSeat
	for _, pick := range rec.Decision.Crew {
		if pick.Model == "" {
			continue
		}
		out = append(out, workgraph.RouteSeat{Role: string(pick.Seat), Model: wgTitle(pick.Model), Pinned: pick.Pinned || rec.TaskPins[pick.Seat], Price: wgPrice(pick.Price)})
	}
	return out
}

// wgSource is a recorded source as the graph's fact: bounded again and scrubbed,
// because a graph is read by more than the person whose machine it is. A record
// with no valid source has none, and nothing stands in for it.
func wgSource(s *TaskSource) *workgraph.TaskSource {
	if !s.valid() {
		return nil
	}
	text, cut := boundSourceText(s.Text)
	return &workgraph.TaskSource{
		ID: s.ID, Kind: s.Kind, Text: redact.Secrets(text), Cut: cut || s.Cut,
		At: workgraph.Stamp(s.At),
	}
}

// kindOf is the engine's kind word for a record.
func wgKind(r taskRecord) string {
	if r.Kind == TaskKindQuick || r.Quick != nil {
		return workgraph.KindQuick
	}
	if r.Kind == "" {
		return "task"
	}
	return string(r.Kind)
}

// nodeFact is one admitted node as a generic fact. Its links are RECORDED: the
// node carries its depends_on and its external waits, so none is a true "waits
// on nothing".
func nodeFact(owner string, r taskRecord) workgraph.Fact {
	n := workgraph.GraphNode{
		ID: wgID(owner, r.ID), Ref: wgRef(owner, r.ID), Seq: r.ID,
		Kind: wgKind(r), Title: wgTitle(r.Title), State: string(r.State),
		Paused: r.App.Paused && r.State != TaskDone && r.State != TaskFailed, Declined: r.App.Declined,
		Ending: string(r.Ending), Depth: r.Depth,
		Clock: workgraph.Clock{
			AdmittedAt: workgraph.Stamp(r.AdmittedAt), StartedAt: workgraph.Stamp(r.StartedAt),
			StateAt: workgraph.Stamp(r.StateAt), EndedAt: workgraph.Stamp(r.EndedAt),
		},
		Instructions: wgInstructions(r), Model: wgTitle(r.Model),
		Landing: wgLanding(r.Merge, r.EndedAt), Files: wgFiles(r.Changed, r.Wrote),
		Spend: wgSpend(r.CostUSD, wgTokens(r)), Links: "recorded",
		ElapsedMS: wgElapsed(r.ElapsedMS, r.State), Attempt: r.Attempt,
		Source: wgSource(r.Source),
	}
	if r.Parent != 0 {
		n.Parent = wgID(owner, r.Parent)
	}
	return workgraph.Fact{Node: n, Waits: nodeWaits(owner, r)}
}

func nodeWaits(owner string, r taskRecord) []workgraph.Wait {
	waits := make([]workgraph.Wait, 0, len(r.DependsOn)+len(r.WaitsExternal))
	for _, id := range r.DependsOn {
		waits = append(waits, workgraph.Wait{On: wgRef(owner, id), Via: workgraph.ViaRecord})
	}
	for _, w := range waitRefs(r.WaitsExternal) {
		waits = append(waits, workgraph.Wait{On: workgraph.TaskRef{Owner: w.Session, Task: strconv.FormatUint(w.ID, 10)}, Via: workgraph.ViaExternal})
	}
	// A WAIT NAMES A SAFE REFERENCE OR IT IS NOT KEPT: an id read off a file is
	// never carried further than the law that checks it.
	waits = validWaits(waits)
	return waits
}

// runFamily is the id and the title of each adaptive run's own row, keyed by
// the row that opens it, so a worker row names the run it hangs under.
func wgRunTitles(runs []runRecord) map[uint64]string {
	titles := make(map[uint64]string)
	for _, r := range runs {
		if r.Parent == 0 || r.Run == "" {
			titles[r.ID] = r.Title
		}
	}
	return titles
}

// runFact is one row an adaptive run published. Its record carries no
// depends_on, so its links are UNRECORDED: zero edges here is unknown, not none.
func runFact(owner string, r runRecord, titles map[uint64]string) workgraph.Fact {
	kind := "run"
	if r.Kind != "" {
		kind = string(r.Kind)
	}
	n := workgraph.GraphNode{
		ID: wgID(owner, r.ID), Ref: wgRef(owner, r.ID), Seq: r.ID,
		Kind: kind, Title: wgTitle(r.Title), State: string(r.State), Stopped: r.Stopped,
		Ending: string(r.Ending), Model: wgTitle(r.Model),
		Clock:   workgraph.Clock{StartedAt: workgraph.Stamp(r.StartedAt), EndedAt: workgraph.Stamp(r.EndedAt)},
		Landing: wgLanding(r.Merge, r.EndedAt), Files: wgFiles(r.Changed, nil),
		Spend: wgSpend(r.CostUSD, 0), Links: "unrecorded", Route: wgRoute(r.CrewState),
		ElapsedMS: wgElapsed(r.ElapsedMS, r.State), Source: wgSource(r.Source),
	}
	if r.State == TaskInterrupted {
		n.State = string(TaskRunning)
	}
	if r.Parent != 0 {
		n.Parent = wgID(owner, r.Parent)
	}
	if r.Run != "" {
		root := r.Parent
		if root == 0 {
			root = r.ID
		}
		n.Group = &workgraph.GroupRef{ID: wgID(owner, root), Title: wgTitle(titles[root])}
	}
	return workgraph.Fact{Node: n}
}

// planToken is a plan-store task's wire spelling.
func wgPlanToken(id string) string { return planStoreID(id) }

// planStateWord is a plan task's status in the engine's state words. A row ready
// to run has not started, so it is queued; a cancelled one is a stopped or
// failed ending.
func planStateWord(t *plandb.Task) string {
	switch t.Status {
	case plandb.StatusDone:
		return string(TaskDone)
	case plandb.StatusFailed, plandb.StatusCancelled:
		return string(TaskFailed)
	case plandb.StatusClaimed, plandb.StatusRunning:
		return string(TaskRunning)
	}
	return string(TaskQueued)
}

// planFact is one task of a plan store as a generic fact: a real sub-task the
// app lists, identified by the store's own "t-<id>". Its links are the store's
// hard dependencies — the ones it gates the task on; a `suggests` is advice
// the store does not enforce and is not a link.
func planFact(owner string, t *plandb.Task, group *workgraph.GroupRef, spend map[string]float64, usage map[string]planTaskUsage) workgraph.Fact {
	tok := wgPlanToken(t.ID)
	n := workgraph.GraphNode{
		ID: owner + ":" + tok, Ref: workgraph.TaskRef{Owner: owner, Task: tok}, Kind: "plan",
		Title: wgTitle(t.Title), State: planStateWord(t), StateDetail: string(t.Status), Paused: t.Paused,
		Stopped:      t.Status == plandb.StatusCancelled && planStopReason(t.Error),
		Clock:        workgraph.Clock{AdmittedAt: workgraph.Stamp(t.CreatedAt), EndedAt: workgraph.Stamp(t.CompletedAt)},
		Instructions: wgText("admitted_brief", t.Description, workgraph.MaxTextRunes),
		Checks:       wgChecks(&t.VerdictBasis), Links: "recorded", Group: group,
		Spend: wgSpend(spend[t.ID], usage[t.ID].tokens), Model: wgTitle(usage[t.ID].model),
	}
	if t.ParentID != "" {
		n.Parent = owner + ":" + wgPlanToken(t.ParentID)
	}
	var waits []workgraph.Wait
	for _, dep := range t.Dependencies {
		if dep.Kind == plandb.DepSuggests {
			continue
		}
		waits = append(waits, workgraph.Wait{On: workgraph.TaskRef{Owner: owner, Task: wgPlanToken(dep.TaskID)}, Via: workgraph.ViaRecord})
	}
	return workgraph.Fact{Node: n, Waits: validWaits(waits)}
}

// validWaits keeps the waits whose target is a safe reference.
func validWaits(in []workgraph.Wait) []workgraph.Wait {
	out := in[:0]
	for _, w := range in {
		if w.On.Valid() {
			out = append(out, w)
		}
	}
	return out
}
