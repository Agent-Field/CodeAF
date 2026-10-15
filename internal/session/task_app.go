package session

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// AppTaskView is an engine read, independent of the app's JSON contract.
// Numeric graph ids and plan ids keep their existing bytes behind the HTTP codec.
type AppTaskView struct {
	ID, Parent, Title, Brief, State, Model, Origin, Result string
	Waits                                                  []string
	Deadline                                               time.Time
	Held                                                   bool
	Cap, Spent                                             float64
	Step                                                   int
	Files                                                  []string
	Notes                                                  []AppTaskNote
	WorkLog                                                []string
	Elapsed                                                int64
	Version                                                uint64
}
type AppTaskNote struct {
	Direction  uint64 `json:"direction,omitempty"`
	Text       string
	ReadAtStep *int
}
type AppQuickTaskView struct {
	// Grew is why the quick work asked to become a full task, while it waits
	// for that answer; State is then "grew".
	ID, Title, State, Grew string
	Files                  []AppQuickFile
	Added, Removed         int
	Version                uint64
}
type AppQuickFile struct {
	Path, BeforeHash, AfterHash string
	// Added and Removed are this file's own changed lines.
	Added, Removed int
}
type AppTaskIntent struct {
	ID, Action, Text, Model, Verb string
	State                         *string
	Version                       *uint64
}

// The metadata records a person's holds and note receipts beside the work itself.
// Wake channels belong to this process; a restored hold creates one on demand.
type appTaskRecord struct {
	Paused   bool          `json:"paused,omitempty"`
	Declined bool          `json:"declined,omitempty"`
	Version  uint64        `json:"version,omitempty"`
	Notes    []AppTaskNote `json:"notes,omitempty"`
	Brief    string        `json:"brief,omitempty"`
}

func cloneAppTaskRecord(in appTaskRecord) appTaskRecord {
	in.Notes = append([]AppTaskNote(nil), in.Notes...)
	for i := range in.Notes {
		if in.Notes[i].ReadAtStep != nil {
			step := *in.Notes[i].ReadAtStep
			in.Notes[i].ReadAtStep = &step
		}
	}
	return in
}

// writeAppControl uses the checkpoint's established store-before-graph lock order.
// A failed disk write restores the in-memory change before any reader can see it.
func (g *TaskGraph) writeAppControl(change func() (func(), error)) error {
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	if store != nil {
		store.mu.Lock()
		defer store.mu.Unlock()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	rollback, err := change()
	if err != nil {
		return err
	}
	if store != nil {
		if err := store.writeLocked(g.documentLocked()); err != nil {
			if rollback != nil {
				rollback()
			}
			return err
		}
	}
	return nil
}

// appBoundary holds the next action without changing whether a note was read.
func (n *TaskNode) appBoundary(ctx context.Context) error {
	for {
		n.graph.mu.Lock()
		paused := n.app.Paused
		if paused && n.appWake == nil {
			n.appWake = make(chan struct{})
		}
		wake := n.appWake
		n.graph.mu.Unlock()
		if !paused {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
		}
	}
	return ctx.Err()
}

func appNodeState(n *TaskNode) string {
	if n.app.Declined {
		return "declined"
	}
	if n.stopped {
		return "stopped"
	}
	if n.app.Paused && !n.state.settled() {
		return "paused"
	}
	switch n.state {
	case TaskQueued:
		return "waiting"
	case TaskUnverified:
		return "your_call"
	case TaskInterrupted:
		return "paused"
	default:
		return string(n.state)
	}
}

// AppTasks reads both engine task roads and pending proposals; no UI copy owns state.
func (a *Agent) AppTasks() ([]AppTaskView, error) {
	out := []AppTaskView{}
	cap, _ := config.CrewTaskSpendCap(a.config.ProfileDir)
	graphIDs := map[string]bool{}
	out = a.appGraphTasks(out, cap, graphIDs)
	// A HANDED-OVER TASK is a run's row, not a node (task_app_runs.go).
	for _, row := range a.appRunRows() {
		id := strconv.FormatUint(row.ID, 10)
		if graphIDs[id] {
			continue
		}
		graphIDs[id] = true
		out = append(out, a.appRunView(row, cap))
	}
	for _, row := range a.PlanTasks() {
		if graphIDs[row.ID] {
			continue
		}
		state := row.StateWord()
		if state == "queued" {
			state = "waiting"
		}
		if state == "your call" {
			state = "your_call"
		}
		if state == "incomplete" {
			state = "failed"
		}
		out = append(out, AppTaskView{ID: row.ID, Parent: row.Parent, Title: row.Title, State: state, Model: row.Model, Cap: cap, Spent: row.USD, Step: row.Steps, Waits: append([]string{}, row.Waits...), Origin: "asked"})
	}
	out = append(out, a.appProposalViews(cap)...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// appGraphTasks is the task graph's own nodes as the app lists them, each
// marked in ids so the rows after it are not listed twice.
func (a *Agent) appGraphTasks(out []AppTaskView, cap float64, ids map[string]bool) []AppTaskView {
	graph := a.tasker()
	if graph == nil {
		return out
	}
	graph.mu.Lock()
	nodes := make([]*TaskNode, 0, len(graph.order))
	for _, id := range graph.order {
		nodes = append(nodes, graph.nodes[id])
	}
	graph.mu.Unlock()
	for _, node := range nodes {
		if node == nil || node.spec.quick != nil {
			continue
		}
		notice := node.notice()
		_, live, _ := a.taskLiveRead(node.id, taskLiveMaxTail)
		graph.mu.Lock()
		view := AppTaskView{ID: strconv.FormatUint(node.id, 10), Title: notice.Title, Brief: notice.Brief, State: appNodeState(node), Model: notice.Model, Cap: cap, Spent: notice.CostUSD, Step: live.Steps, Result: notice.Report, Files: append([]string{}, notice.Changed...), Notes: node.appNotesLocked(), WorkLog: live.Lines, Elapsed: int64(notice.Elapsed.Seconds()), Version: node.app.Version + 1, Origin: "proposed"}
		if node.app.Brief != "" {
			view.Brief = node.app.Brief
		}
		if node.parent != 0 {
			view.Parent = strconv.FormatUint(node.parent, 10)
		}
		for _, id := range node.dependsOn {
			view.Waits = append(view.Waits, strconv.FormatUint(id, 10))
		}
		if len(view.Files) == 0 {
			view.Files = append([]string{}, node.wrote...)
		}
		journal := node.journal
		graph.mu.Unlock()
		view.WorkLog = appTaskWorkLog(view.WorkLog, journal)
		ids[view.ID] = true
		out = append(out, view)
	}
	return out
}

func (a *Agent) AppTask(id string) (AppTaskView, bool, error) {
	rows, err := a.AppTasks()
	if err != nil {
		return AppTaskView{}, false, err
	}
	for _, row := range rows {
		if row.ID == id {
			if page, ok := a.PlanTaskPage(id); ok {
				if row.Brief == "" {
					row.Brief = page.Description
				}
				if row.Result == "" {
					row.Result = page.Result
				}
				if len(row.Notes) == 0 {
					for _, note := range page.Notes {
						row.Notes = append(row.Notes, AppTaskNote{Text: note.Body})
					}
				}
				if len(row.WorkLog) == 0 {
					for _, step := range page.Steps {
						row.WorkLog = append(row.WorkLog, step.Command)
					}
				}
			}
			return row, true, nil
		}
	}
	return AppTaskView{}, false, nil
}

func appExpect(intent AppTaskIntent, state string, version uint64) error {
	if (intent.State != nil && *intent.State != state) || (intent.Version != nil && *intent.Version != version) {
		return errors.New("stale: that task changed; open it again")
	}
	return nil
}

// AppTaskAction keeps admission and landing separate: a proposal answers a wait;
// a finished task uses the existing continuation, check and acceptance doors.
func (a *Agent) AppTaskAction(intent AppTaskIntent) error {
	id, numeric := strconv.ParseUint(intent.ID, 10, 64)
	if numeric == nil {
		if handled, err := a.appProposalAction(id, intent); handled {
			return err
		}
	}
	row, found, err := a.AppTask(intent.ID)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("no such task")
	}
	if err := appExpect(intent, row.State, row.Version); err != nil {
		return err
	}
	node := a.taskNode(id)
	if numeric == nil && node == nil {
		if run, ok := a.appRunRow(id); ok {
			return a.appRunAction(run, intent)
		}
	}
	if numeric != nil || node == nil {
		return a.appPlanAction(intent)
	}
	return a.appGraphAction(node, intent, row)
}

// A proposal's clock and model are edited under the same lock as its answer.
func (a *Agent) appProposalAction(id uint64, intent AppTaskIntent) (bool, error) {
	var choice taskModelChoice
	if intent.Action == "model" {
		choice = a.resolveTaskModel(intent.Model)
	}
	a.mu.Lock()
	question := a.taskAnswers[id]
	if question == nil {
		a.mu.Unlock()
		return false, nil
	}
	state := "proposed"
	if question.held {
		state = "held"
	}
	if err := appExpect(intent, state, question.appVersion+1); err != nil {
		a.mu.Unlock()
		return true, err
	}
	switch intent.Action {
	case "hold", "typing":
		a.mu.Unlock()
		a.HoldTask(id)
		return true, nil
	case "model":
		if choice.problem != "" || choice.model == "" {
			a.mu.Unlock()
			return true, errors.New("choose one known model")
		}
		question.notice.Model = choice.model
		question.appVersion++
		notice, hub := question.notice, a.hub
		a.mu.Unlock()
		a.broadcastTaskProposal(hub, notice)
		return true, nil
	case "start", "decline":
		delete(a.taskAnswers, id)
		notice := question.notice
		a.mu.Unlock()
		if intent.Action == "decline" {
			a.keepAppDeclined(id, notice)
		}
		question.answer <- TaskAnswer{Approved: intent.Action == "start", Model: notice.Model}
		return true, nil
	default:
		a.mu.Unlock()
		return true, errors.New("that proposal must be started, held or declined")
	}
}

func (a *Agent) appPlanAction(intent AppTaskIntent) error {
	switch intent.Action {
	case "note":
		return a.PlanNote(intent.ID, intent.Text)
	case "brief":
		return a.PlanAmend(intent.ID, intent.Text)
	case "pause":
		return a.PlanPause(intent.ID)
	case "resume":
		return a.PlanResume(intent.ID)
	case "stop":
		return a.PlanCancel(intent.ID)
	}
	return errors.New("that action is unavailable for this plan task")
}
func (a *Agent) appGraphAction(node *TaskNode, intent AppTaskIntent, row AppTaskView) error {
	switch intent.Action {
	case "pause", "resume":
		return node.appPause(intent)
	case "stop":
		_, err := a.Cancel("task:" + intent.ID)
		return err
	case "model":
		_, err := a.RetargetTask(node.id, intent.Model)
		return err
	case "note", "brief":
		return a.appTaskNote(node, intent)
	case "decide":
		return a.appTaskDecide(node.id, intent, row.State)
	case "continue":
		// A task the app's quit left with nothing driving it goes back on the
		// frontier as itself ([Agent.ContinueTask]); any other is left alone.
		node.graph.mu.Lock()
		interrupted := node.state == TaskInterrupted
		node.graph.mu.Unlock()
		if !interrupted {
			return nil
		}
		return a.ContinueTask(node.id, "")
	}
	return errors.New("unknown task action")
}

// A queued hold keeps the frontier still; a running hold releases at a boundary.
func (node *TaskNode) appPause(intent AppTaskIntent) error {
	var wake chan struct{}
	err := node.graph.writeAppControl(func() (func(), error) {
		if err := appExpect(intent, appNodeState(node), node.app.Version+1); err != nil {
			return nil, err
		}
		if node.state.settled() {
			return nil, errors.New("that task has finished")
		}
		old := node.app
		node.app.Paused = intent.Action == "pause"
		node.app.Version++
		if intent.Action == "resume" {
			wake = node.appWake
		}
		return func() { node.app = old }, nil
	})
	if err != nil {
		return err
	}
	if wake != nil {
		node.graph.mu.Lock()
		if node.appWake == wake {
			close(wake)
			node.appWake = nil
		}
		node.graph.mu.Unlock()
	}
	node.graph.announce(node)
	if intent.Action == "resume" {
		node.graph.runFrontier()
	}
	return nil
}

func (a *Agent) appTaskNote(node *TaskNode, intent AppTaskIntent) error {
	if strings.TrimSpace(intent.Text) == "" {
		return errors.New("words required")
	}
	receipt, err := a.SteerTask(node.id, intent.Text)
	if err != nil {
		return err
	}
	err = node.graph.writeAppControl(func() (func(), error) {
		old := cloneAppTaskRecord(node.app)
		node.app.Notes = append(node.app.Notes, AppTaskNote{Text: intent.Text, Direction: receipt.Direction})
		if intent.Action == "brief" {
			node.app.Brief = intent.Text
		}
		node.app.Version++
		return func() { node.app = old }, nil
	})
	if err == nil {
		node.graph.announce(node)
	}
	return err
}
func (a *Agent) appTaskDecide(id uint64, intent AppTaskIntent, state string) error {
	if state != "your_call" {
		return errors.New("that task is not waiting for your call")
	}
	switch intent.Verb {
	case "accept":
		return a.ResolveUnverified(id, TaskAccept, intent.Text)
	case "recheck":
		return a.ResolveUnverified(id, TaskReaudit, intent.Text)
	case "not_right", "tell":
		return a.ContinueTask(id, intent.Text)
	case "let_it_decide":
		return a.HandUnverifiedToModel(id)
	}
	return errors.New("unknown your call verb")
}

// A declined proposal has a record but never enters the running frontier.
func (a *Agent) keepAppDeclined(id uint64, notice TaskNotice) {
	graph := a.graph()
	node := &TaskNode{graph: graph, id: id, kind: "", state: TaskFailed, done: make(chan struct{}), spec: taskSpec{title: notice.Title, named: true, brief: notice.Brief, acceptance: notice.Acceptance, model: notice.Model}, app: appTaskRecord{Declined: true}, report: "Declined by you"}
	close(node.done)
	graph.mu.Lock()
	if graph.nodes[id] == nil {
		graph.nodes[id] = node
		graph.order = append(graph.order, id)
	}
	graph.mu.Unlock()
	graph.checkpoint()
}

func (a *Agent) AppQuickTasks() ([]AppQuickTaskView, error) {
	out := []AppQuickTaskView{}
	graph := a.tasker()
	if graph == nil {
		return out, nil
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	for _, id := range graph.order {
		node := graph.nodes[id]
		if node == nil || node.spec.quick == nil {
			continue
		}
		quick := node.spec.quick
		state := "running"
		if node.state.settled() {
			state = "done"
		}
		if quick.promotion != 0 && !quick.promoted && quick.grew != "" {
			state = "grew"
		}
		if quick.promoted {
			state = "promoted"
		}
		if quick.undone {
			state = "undone"
		}
		view := AppQuickTaskView{ID: strconv.FormatUint(id, 10), Title: node.spec.title, State: state, Grew: quick.grew, Version: node.app.Version + 1, Files: []AppQuickFile{}}
		for _, c := range quick.captures {
			added, removed := quickLineChanges(c.Before, c.After)
			view.Files = append(view.Files, AppQuickFile{Path: c.Path, BeforeHash: quickHash(c.Before, c.BeforeExists), AfterHash: quickHash(c.After, c.AfterExists), Added: added, Removed: removed})
			view.Added += added
			view.Removed += removed
		}
		out = append(out, view)
	}
	return out, nil
}

func (a *Agent) AppQuickTaskAction(intent AppTaskIntent) ([]string, []string, error) {
	id, err := strconv.ParseUint(intent.ID, 10, 64)
	if err != nil {
		return nil, nil, errors.New("no such quick task")
	}
	node := a.taskNode(id)
	if node == nil || node.spec.quick == nil {
		return nil, nil, errors.New("no such quick task")
	}
	rows, _ := a.AppQuickTasks()
	for _, row := range rows {
		if row.ID == intent.ID {
			if err := appExpect(intent, row.State, row.Version); err != nil {
				return nil, nil, err
			}
		}
	}
	switch intent.Action {
	case "undo":
		return a.UndoQuickTask(id)
	case "cancel":
		_, err := a.Cancel("task:" + intent.ID)
		return nil, nil, err
	case "promote":
		return nil, nil, a.appPromoteQuick(node)
	}
	return nil, nil, errors.New("unknown quick task action")
}

// Note receipts come from the steering record that was carried into a request.
func (n *TaskNode) appNotesLocked() []AppTaskNote {
	notes := cloneAppTaskRecord(n.app).Notes
	known := map[uint64]bool{}
	for i := range notes {
		known[notes[i].Direction] = true
		if said, ok := n.assignment.direction(notes[i].Direction); ok {
			notes[i].ReadAtStep = said.readAtStep
		}
	}
	for _, said := range n.assignment.directions {
		if said.from == directionFromPerson && !known[said.id] {
			notes = append(notes, AppTaskNote{Direction: said.id, Text: said.words, ReadAtStep: said.readAtStep})
		}
	}
	return notes
}

// An explicit Promote press is permission to start the ordinary task road.
func (a *Agent) promoteQuickDirect(node *TaskNode) error {
	if !node.quickIO.TryLock() {
		return errors.New("stale: that quick task is changing; read it again")
	}
	defer node.quickIO.Unlock()
	node.graph.mu.Lock()
	quick := node.spec.quick
	if quick.promoted || quick.undone {
		node.graph.mu.Unlock()
		return errors.New("that quick task has ended")
	}
	brief := quick.line + "\n\nContinue from these in-place edits; do not discard them."
	for _, capture := range quick.captures {
		brief += "\n" + capture.Path
	}
	title, model, parent, owner := node.spec.title, node.spec.model, node.parent, node.owner
	ground := node.Ground
	depends := []uint64{}
	if !node.state.settled() {
		depends = append(depends, node.id)
	}
	cancel := node.cancel
	node.graph.mu.Unlock()
	id := node.graph.reserve()
	err := node.graph.writeAppControl(func() (func(), error) {
		quick.promoted = true
		old := node.app.Version
		node.app.Version++
		return func() { quick.promoted = false; node.app.Version = old }, nil
	})
	if err != nil {
		return err
	}
	spec := taskSpec{admission: a.admissionContext(), title: title, named: true, brief: brief, acceptance: taskPersonAcceptance, model: model, ground: ground, where: "in place", mode: TaskModeInPlace, parent: parent, owner: owner, dependsOn: depends}
	node.graph.admit(id, spec)
	if cancel != nil {
		cancel()
	}
	return nil
}

func (a *Agent) appProposalViews(cap float64) []AppTaskView {
	out := []AppTaskView{}
	a.mu.Lock()
	for id, question := range a.taskAnswers {
		notice := question.notice
		state := "proposed"
		if question.held {
			state = "held"
		}
		view := AppTaskView{ID: strconv.FormatUint(id, 10), Title: notice.Title, Brief: notice.Brief, State: state, Model: notice.Model, Deadline: notice.Deadline, Held: question.held, Cap: cap, Origin: "proposed", Version: question.appVersion + 1}
		for _, depends := range notice.DependsOn {
			view.Waits = append(view.Waits, strconv.FormatUint(depends, 10))
		}
		out = append(out, view)
	}
	a.mu.Unlock()
	return out
}

// A restored node reads its existing journal when no live work-log tail remains.
func appTaskWorkLog(live []string, journal string) []string {
	if len(live) > 0 || journal == "" {
		return live
	}
	out := []string{}
	for _, entry := range ReadTranscript(journal).Entries {
		if entry.Role == "tool" || entry.Role == "assistant" {
			out = append(out, entry.Hint+" "+entry.Text)
		}
	}
	return out
}

// A stale checkpoint may name a proposal from the previous engine lifetime.
func (a *Agent) appPromoteQuick(node *TaskNode) error {
	node.graph.mu.Lock()
	promotion, promoted, active := node.spec.quick.promotion, node.spec.quick.promoted, !node.state.settled()
	node.graph.mu.Unlock()
	if promoted {
		return errors.New("that quick task was already promoted")
	}
	if promotion != 0 {
		if owner := node.graph.quickProposalOwner(node); owner != nil {
			if handled, err := owner.appProposalAction(promotion, AppTaskIntent{ID: strconv.FormatUint(promotion, 10), Action: "start"}); handled {
				return err
			}
		}
		if active {
			return errors.New("stale: that promotion is still opening; read it again")
		}
	}
	return a.promoteQuickDirect(node)
}

// A quick worker's promotion belongs to the person, even inside a nested task.
func (g *TaskGraph) quickProposalOwner(node *TaskNode) *Agent {
	if g.home != nil {
		return g.home
	}
	return g.runner(node)
}
func (a *Agent) taskProposalOwner(spec taskSpec) *Agent {
	if !spec.irreversible || !a.config.InTask {
		return a
	}
	if graph := a.tasker(); graph != nil && graph.home != nil {
		return graph.home
	}
	return a
}
