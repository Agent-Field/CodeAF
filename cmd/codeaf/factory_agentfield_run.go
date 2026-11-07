package main

// ── A JOB RUN BY AN AGENTFIELD REASONER, KEPT HERE AS AN ORDINARY JOB ───────
//
// A job filed with run_on (factory.Item.RunOn) is run by that reasoner on its
// node, and this file is the runner's Options.Remote: it dispatches the run,
// then keeps the job's own record of it, so the run reads exactly like a job
// this factory ran itself.
//
//   - ITS STEPS ARE THE RUN'S TOP-LEVEL CALLS. The dispatched reasoner's
//     children in AgentField's tree become Stream.Phases, named by reasoner
//     and in the order they began. Nested calls are not steps; the wire's
//     factory.agentfield_run folds them under the step that made them.
//   - A STEP THAT RAN THE CODEAF HARNESS IS AN ORDINARY CONVERSATION HERE. The
//     node relays every line of that run's own record VERBATIM; the first
//     "started" mints a conversation folder in the job's bucket, owned by the
//     job like a native step's, and every line after its header is appended to
//     its transcript.jsonl unchanged. So `agents.entries` reads that step with
//     the SAME code, into the SAME entries, as a step that ran on this machine,
//     and a held chat hears each batch through the same stage news a native
//     step's events tell (appfactory_stepstream.go).
//   - WORKAF IS THE SYSTEM OF RECORD. A batch is appended, then acked, then
//     the ack is recorded on the job (AgentFieldRun.Acked). The control plane
//     drops what was acked. Reading resumes after the recorded seq, so a
//     factory that was offline gets every line once; a batch appended but not
//     yet recorded as acked is skipped by seq on the next read, never doubled.
//   - THE JOB ENDS WITH THE RUN: done when the root succeeded, needs you with
//     the run's own words when it failed, and the person's Stop cancels the
//     whole tree.
//
// A control plane that does not answer is waited out, never a failure: the
// node keeps every line until one does.

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// afRemoteEvery is how often a running AgentField job is read.
// afStopDrain is how long a stopped run is read for after its cancel: long
// enough for the node to relay a step's last lines and the control plane to
// mark the tree, short enough that a stop never hangs on a node that is gone.
const afStopDrain = 15 * time.Second

var afRemoteEvery = 500 * time.Millisecond

// afClient is the part of the control plane client a remote run uses.
type afClient interface {
	Dispatch(ctx context.Context, target string, input any, owner string) (agentfield.Dispatched, error)
	Tree(ctx context.Context, run string) (agentfield.TreeNode, string, error)
	Relay(ctx context.Context, execution string, after int64) ([]agentfield.RelayEntry, error)
	RelayAck(ctx context.Context, execution string, through int64) error
	Steer(ctx context.Context, execution, message string) error
	CancelTree(ctx context.Context, run string) error
}

// afRemote runs RunOn jobs for one runner.
type afRemote struct {
	items     *store.Store // the runner's jobs
	floor     *store.Store // the factory a step's conversation is owned by
	workspace string
	// conn is the control plane now, and the node id this factory is; false
	// while the factory is not connected.
	conn func() (afClient, string, bool)
	// news tells the app a step's transcript grew.
	news func(file string)
	now  func() time.Time

	mu    sync.Mutex
	files map[string]*afMirror // by execution id
	drain map[string]bool      // executions with nothing more to read
}

// afDrainAfter is how long after an execution ended its relay is still read.
const afDrainAfter = time.Minute

// readToEnd reads the run, writing it on jobs, until its tree has ended and
// every harness step's record has closed ("ended" read and kept), or ctx is
// done: a step whose node died without closing is waited out to the deadline.
func (r *afRemote) readToEnd(ctx context.Context, client afClient, runID string, job int, jobs *store.Store) {
	for {
		if tree, _, err := client.Tree(ctx, runID); err == nil {
			if ended, _ := r.syncTo(ctx, client, jobs, job, tree); ended && r.recordsClosed() {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(afRemoteEvery):
		}
	}
}

// recordsClosed says every harness step this run made a conversation for has
// had its record closed.
func (r *afRemote) recordsClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for exec := range r.files {
		if !r.drain[exec] {
			return false
		}
	}
	return true
}

func (r *afRemote) drained(exec string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.drain[exec]
}

func (r *afRemote) markDrained(exec string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.drain == nil {
		r.drain = map[string]bool{}
	}
	r.drain[exec] = true
}

// afMirror is one harness execution's conversation on this machine.
type afMirror struct {
	file   string
	id     string
	header bool // the local header is written
	state  afRelayState
	// cpAcked is the last ack the control plane answered, in this process.
	cpAcked int64
}

func (r *afRemote) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// Run is Options.Remote.
func (r *afRemote) Run(ctx context.Context, job int) error {
	it, err := r.items.Get(job)
	if err != nil {
		return err
	}
	client, node, ok := r.conn()
	if !ok {
		return errors.New(factoryrun.NoRemoteWords)
	}
	run := it.AgentField
	if run == nil || run.ExecutionID == "" {
		input := map[string]any{"job": it.ID, "title": it.Title, "body": it.Body, "event": afJobEvent(it)}
		d, err := client.Dispatch(ctx, strings.TrimSpace(it.RunOn), input, node)
		if err != nil {
			return err
		}
		fresh := &factory.AgentFieldRun{Target: strings.TrimSpace(it.RunOn), ExecutionID: d.ExecutionID, RunID: firstNonEmpty(d.RunID, d.WorkflowID, d.ExecutionID), Began: r.clock(), Status: d.Status}
		// The run's page, for the job's source link, when the client knows
		// the control plane's address.
		if pages, ok := client.(interface{ RunURL(string) string }); ok {
			fresh.URL = pages.RunURL(fresh.RunID)
		}
		// The node's machine, for the job's "runs on" fact.
		if nodes, ok := client.(interface {
			NodeHost(context.Context, string) string
		}); ok {
			n, _, _ := strings.Cut(fresh.Target, ".")
			fresh.Host = nodes.NodeHost(ctx, n)
		}
		if err := r.items.Update(job, func(it *factory.Item) error { it.AgentField = fresh; return nil }); err != nil {
			return err
		}
		run = fresh
	}
	tick := time.NewTicker(afRemoteEvery)
	defer tick.Stop()
	for {
		tree, _, err := client.Tree(ctx, run.RunID)
		if err == nil {
			ended, failure := r.sync(ctx, client, job, tree)
			if ended {
				// A RUN THAT ENDED IS STILL READ TO ITS LAST LINE: a node
				// relays a step's record beside the step, so its last lines
				// (and its "ended") can reach the control plane after the run
				// is over, and the control plane holds them until this
				// factory acks them.
				drainCtx, cancel := context.WithTimeout(context.Background(), afStopDrain)
				r.readToEnd(drainCtx, client, run.RunID, job, r.items)
				cancel()
				return failure
			}
		}
		select {
		case <-ctx.Done():
			// THE PERSON'S STOP STOPS THE WHOLE RUN, not only this reading.
			cancelCtx, cancel := context.WithTimeout(context.Background(), afStopDrain)
			_ = client.CancelTree(cancelCtx, run.RunID)
			// AND THE RUN IS STILL READ TO ITS END. A step's last lines reach
			// the control plane after the stop, and the control plane holds
			// them until this factory acks them: a reader that went home at
			// the stop would leave them there for good, and the job would go
			// on saying its run is running. A worker lets its copy of a
			// stopped job go within seconds and the factory takes no more
			// reports for it, so the end is written on the factory's own job.
			jobs := r.items
			if r.floor != nil {
				jobs = r.floor
			}
			r.readToEnd(cancelCtx, client, run.RunID, job, jobs)
			cancel()
			return ctx.Err()
		case <-tick.C:
		}
		if c, _, ok := r.conn(); ok {
			client = c
		}
	}
}

// afJobEvent is the event that filed the job, as the trigger handed it over,
// or nothing.
func afJobEvent(it factory.Item) any {
	if strings.TrimSpace(it.SourceRef) == "" {
		return nil
	}
	return map[string]string{"source": string(it.Origin), "ref": it.SourceRef}
}

// sync writes one reading of the tree onto the job: its steps, and every
// harness step's new lines. It says whether the run has ended, and how.
func (r *afRemote) sync(ctx context.Context, client afClient, job int, root agentfield.TreeNode) (bool, error) {
	return r.syncTo(ctx, client, r.items, job, root)
}

// syncTo is sync writing the job in jobs: the runner's own store while the
// run is followed, the factory's once a stop has let the runner's copy go.
func (r *afRemote) syncTo(ctx context.Context, client afClient, jobs *store.Store, job int, root agentfield.TreeNode) (bool, error) {
	steps := afFlatten(root)
	it, err := jobs.Get(job)
	if err != nil {
		return false, nil
	}
	chats := map[string]string{}
	for _, step := range steps {
		if m := r.pull(ctx, client, it, step); m != nil {
			chats[step.node.ExecutionID] = m.file
		}
	}
	now := r.clock()
	ended := afEnded(root.Status)
	_ = jobs.Update(job, func(it *factory.Item) error {
		if it.Stream == nil {
			it.Stream = &factory.Stream{Started: now}
		}
		if it.AgentField == nil {
			it.AgentField = &factory.AgentFieldRun{}
		}
		run := it.AgentField
		run.Status = root.Status
		stages := make([]factory.Stage, 0, len(steps))
		phases := make([]factory.Phase, 0, len(steps))
		cur := -1
		for i, step := range steps {
			n := step.node
			// A STEP IS A CHAT ONLY WHEN IT HAS A CONVERSATION: an execution
			// that runs the codeaf harness relays its record and gets one, and
			// every other execution is the reasoner's plain code — drawn as a
			// native command step ("check"), with no chat and no steer box. A
			// reasoner that does some code before it calls the harness reads
			// as a check until the harness's first line arrives.
			kind := factory.StageCheck
			if chats[n.ExecutionID] != "" {
				kind = factory.StageChat
			}
			stages = append(stages, factory.Stage{Name: step.name, Label: step.label, Parent: step.parent, Group: step.group, Kind: kind, On: true})
			p := factory.Phase{Name: step.name, Kind: kind, State: afPhaseState(n.Status), Chat: chats[n.ExecutionID]}
			p.Started = afTime(n.StartedAt)
			if n.CompletedAt != nil {
				p.At = afTime(*n.CompletedAt)
			}
			if p.State == factory.PhaseSkipped {
				p.SkipReason = "cancelled"
			}
			if p.State == factory.PhaseFailed && n.StatusReason != nil {
				p.Note = *n.StatusReason
			}
			if p.State == factory.PhaseRunning && step.parent == "" && cur < 0 {
				cur = i
			}
			phases = append(phases, p)
		}
		if cur < 0 {
			cur = len(phases) - 1
		}
		it.Stages = stages
		it.Stream.Phases, it.Stream.Cur = phases, max(cur, 0)
		if ended {
			run.Ended = now
			if root.StatusReason != nil {
				run.Error = *root.StatusReason
			}
		}
		return nil
	})
	if !ended {
		return false, nil
	}
	if afSucceeded(root.Status) {
		return true, nil
	}
	words := "the AgentField run " + strings.ToLower(root.Status)
	if root.StatusReason != nil && strings.TrimSpace(*root.StatusReason) != "" {
		words += ": " + strings.TrimSpace(*root.StatusReason)
	}
	return true, errors.New(words)
}

// afStep is one execution of the run as a step of the job.
type afStep struct {
	node   agentfield.TreeNode
	name   string // unique in the job
	label  string // the reasoner, when name had to differ from it
	parent string // the calling step's name; "" at the top
	group  string // shared by siblings that ran at the same time
}

// afFlatten is every execution under root, each after the step that called
// it, siblings in the order they began. A STEP'S NAME IS ITS IDENTITY AND IS
// ONE WORD (factory.StageWord: 2 to 12 letters and digits), so it is the
// reasoner's letters — search_docs is searchdocs — numbered when the reasoner
// is called more than once in the run (investigate1, investigate2, …), and
// the reasoner's own spelling is its label whenever the two differ.
func afFlatten(root agentfield.TreeNode) []afStep {
	count := map[string]int{}
	var tally func(n agentfield.TreeNode)
	tally = func(n agentfield.TreeNode) {
		for _, c := range n.Children {
			count[afReasoner(c)]++
			tally(c)
		}
	}
	tally(root)
	seen := map[string]int{}
	taken := map[string]bool{}
	var out []afStep
	var walk func(n agentfield.TreeNode, parent string)
	walk = func(n agentfield.TreeNode, parent string) {
		kids := append([]agentfield.TreeNode(nil), n.Children...)
		// A call that has not begun has no start yet and goes last.
		sort.SliceStable(kids, func(i, j int) bool {
			a, b := afTime(kids[i].StartedAt), afTime(kids[j].StartedAt)
			if a.IsZero() || b.IsZero() {
				return !a.IsZero() && b.IsZero()
			}
			return a.Before(b)
		})
		named := make([]afStep, len(kids))
		for i, c := range kids {
			reasoner := afReasoner(c)
			n := 0
			if count[reasoner] > 1 {
				seen[reasoner]++
				n = seen[reasoner]
			}
			step := afStep{node: c, name: afStepWord(reasoner, n, taken), parent: parent}
			if step.name != reasoner {
				step.label = reasoner
			}
			named[i] = step
		}
		afGroup(named)
		for _, step := range named {
			out = append(out, step)
			walk(step.node, step.name)
		}
	}
	walk(root, "")
	return out
}

// afStepWord is reasoner as one step word, numbered n when n > 0, and not
// one already taken in the job.
func afStepWord(reasoner string, n int, taken map[string]bool) string {
	base := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, reasoner)
	if utf8.RuneCountInString(base) < 2 {
		base = "step" + base
	}
	for try := n; ; try++ {
		suffix := ""
		if try > 0 {
			suffix = strconv.Itoa(try)
		}
		word := []rune(base)
		if len(word)+len(suffix) > 12 {
			word = word[:12-len(suffix)]
		}
		name := string(word) + suffix
		if !taken[name] {
			taken[name] = true
			return name
		}
		if try == 0 {
			try = 1
		}
	}
}

func afReasoner(n agentfield.TreeNode) string {
	if name := strings.TrimSpace(n.ReasonerID); name != "" {
		return name
	}
	return "step"
}

// afGroup gives siblings whose times overlap one group, named by the first of
// them. Steps are in the order they began; one still running has no end yet.
func afGroup(steps []afStep) {
	for i := 0; i < len(steps); {
		end := afEnd(steps[i].node)
		j := i + 1
		for j < len(steps) && !afTime(steps[j].node.StartedAt).IsZero() && (end.IsZero() || afTime(steps[j].node.StartedAt).Before(end)) {
			if e := afEnd(steps[j].node); e.IsZero() || (!end.IsZero() && e.After(end)) {
				end = e
			}
			j++
		}
		if j-i > 1 {
			for k := i; k < j; k++ {
				steps[k].group = steps[i].name
			}
		}
		i = j
	}
}

// afEnd is when n ended, zero while it runs.
func afEnd(n agentfield.TreeNode) time.Time {
	if n.CompletedAt == nil {
		return time.Time{}
	}
	return afTime(*n.CompletedAt)
}

// pull keeps one step's conversation: every relayed line after the last one
// kept is appended, then the record says so, then the control plane is told.
// It answers the step's conversation, nil while it has none.
func (r *afRemote) pull(ctx context.Context, client afClient, it factory.Item, step afStep) *afMirror {
	exec, name := step.node.ExecutionID, step.name
	m := r.mirror(it, exec, name, false)
	// AN ACK THAT FAILED IS SENT AGAIN: the lines are kept, so the next read
	// answers nothing, and without this the pipe would hold them until its
	// retention ceiling.
	if m != nil && m.state.Acked > m.cpAcked {
		if client.RelayAck(ctx, exec, m.state.Acked) == nil {
			m.cpAcked = m.state.Acked
		}
	}
	if r.drained(exec) {
		return m
	}
	var after int64
	if m != nil {
		after = m.state.Acked
	}
	entries, err := client.Relay(ctx, exec, after)
	if err != nil {
		return m
	}
	if len(entries) == 0 {
		// A step that ended a while ago with nothing left to read is not read
		// again: a plain step never has lines, and a harness step's last
		// lines reach the control plane before its execution ends.
		if end := afEnd(step.node); afEnded(step.node.Status) && (m == nil || !m.state.Steerable) && !end.IsZero() && r.clock().Sub(end) > afDrainAfter {
			r.markDrained(exec)
		}
		return m
	}
	// THE RECORD IS KEYED BY (EXECUTION, SEQ). After a control plane restart
	// a node's replay can bring a batch this factory already kept, and a
	// resend can arrive beside newer entries: in seq order, every seq at or
	// below the kept one is skipped, so each lands once.
	slices.SortStableFunc(entries, func(a, b agentfield.RelayEntry) int { return cmp.Compare(a.Seq, b.Seq) })
	through := after
	var lines [][]byte
	started, closed := false, false
	for _, e := range entries {
		if e.Seq <= through {
			continue // already kept: never twice
		}
		through = e.Seq
		switch e.Kind {
		case "started":
			started = true
		case "line":
			lines = append(lines, append([]byte(nil), e.Data...))
		case "ended":
			closed = true
		}
		// "person" is the node saying a steer arrived; the record's own steer
		// line follows when codeaf reads it, so it is not written twice.
	}
	if m == nil && (started || len(lines) > 0) {
		m = r.mirror(it, exec, name, true)
	}
	if m == nil {
		// Nothing a conversation is made of: a plain step. Acked so the
		// control plane holds nothing for it.
		_ = client.RelayAck(ctx, exec, through)
		return nil
	}
	if err := m.append(lines, r.workspace); err != nil {
		return m // unkept and unacked: the next reading asks from after again
	}
	if started {
		m.state.Steerable = true
	}
	if closed {
		m.state.Steerable = false
		defer r.markDrained(exec)
	}
	m.state.Acked = through
	if err := m.save(); err != nil {
		return m
	}
	// THE ACK FOLLOWS THE RECORD. A factory that dies between the two is asked
	// again from its record, skips those seqs, and acks then.
	if client.RelayAck(ctx, exec, through) == nil {
		m.cpAcked = through
	}
	if len(lines) > 0 && r.news != nil {
		r.news(m.file)
	}
	return m
}

// afRelayFile sits beside a mirrored step's transcript: which execution it is
// and how much of it is kept. It is WorkAF's own record of the relay, so it
// lives with the conversation, not in the run's memory.
const afRelayFile = "agentfield.json"

// afRelayState is afRelayFile.
type afRelayState struct {
	ExecutionID string `json:"execution_id"`
	Step        string `json:"step"`
	Job         int    `json:"job"`
	// Acked is the last relay seq this conversation has.
	Acked int64 `json:"acked"`
	// Steerable is true from the run's "started" to its "ended": a codeaf
	// harness run working now.
	Steerable bool `json:"steerable"`
}

func afReadRelay(file string) (afRelayState, bool) {
	var s afRelayState
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), afRelayFile))
	if err != nil || json.Unmarshal(raw, &s) != nil || s.ExecutionID == "" {
		return s, false
	}
	return s, true
}

func (m *afMirror) save() error {
	raw, err := json.Marshal(m.state)
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(m.file), afRelayFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// mirror is exec's conversation: the one this run made, or the one a step of
// the job already names, or, when make is set, a new one owned by the job.
func (r *afRemote) mirror(it factory.Item, exec, step string, make bool) *afMirror {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.files == nil {
		r.files = map[string]*afMirror{}
	}
	if m := r.files[exec]; m != nil {
		return m
	}
	if it.Stream != nil {
		for _, ph := range it.Stream.Phases {
			if ph.Chat == "" {
				continue
			}
			if s, ok := afReadRelay(ph.Chat); ok && s.ExecutionID == exec {
				m := &afMirror{file: ph.Chat, id: appAgentID(ph.Chat), header: afHasHeader(ph.Chat), state: s}
				r.files[exec] = m
				return m
			}
		}
	}
	if !make {
		return nil
	}
	bucket, err := v3ProjectDir(r.workspace)
	if err != nil {
		return nil
	}
	place, err := v3MintSession(bucket, r.workspace, r.workspace, false)
	if err != nil {
		return nil
	}
	file := place.Transcript()
	owner := factoryChatOwner{Root: chatKey(r.floor.Root()), Job: it.ID, Repo: it.Repo, Num: it.Num, Created: it.Created, Step: step}
	if raw, err := json.Marshal(owner); err == nil {
		_ = os.MkdirAll(filepath.Dir(file), 0o700)
		_ = os.WriteFile(filepath.Join(filepath.Dir(file), factoryOwnerFile), raw, 0o600)
	}
	m := &afMirror{file: file, id: place.ID(), state: afRelayState{ExecutionID: exec, Step: step, Job: it.ID}}
	// THE HEADER IS WRITTEN WITH THE FOLDER. A held chat opens this
	// conversation as soon as its step is listed, and an opener that finds
	// the record empty writes a header of its own — so a header left for the
	// first relayed line to bring would land second, and the record would
	// open twice. The node's own header is then skipped as a repeat.
	if os.WriteFile(file, append(afLocalHeader(nil, m.id, afHeaderCwd(file, r.workspace)), '\n'), 0o600) != nil {
		return nil
	}
	m.header = true
	if m.save() != nil {
		return nil
	}
	r.files[exec] = m
	return m
}

func afHasHeader(file string) bool {
	info, err := os.Stat(file)
	return err == nil && info.Size() > 0
}

// append writes lines to the conversation. The record's own header names the
// node's folder and cwd, so it is replaced by this conversation's; every other
// line is written byte for byte.
func (m *afMirror) append(lines [][]byte, workspace string) error {
	if !m.header {
		// Something else may have opened the conversation since this mirror
		// was found, and opening an empty record writes its header.
		m.header = afHasHeader(m.file)
	}
	var buf bytes.Buffer
	for _, line := range lines {
		line = bytes.TrimRight(line, "\r\n")
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if afIsHeader(line) {
			if m.header {
				continue
			}
			line = afLocalHeader(line, m.id, afHeaderCwd(m.file, workspace))
		} else if !m.header {
			// A record whose header was lost still opens as a conversation.
			buf.Write(afLocalHeader(nil, m.id, afHeaderCwd(m.file, workspace)))
			buf.WriteByte('\n')
		}
		m.header = true
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if buf.Len() == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.file), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(m.file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func afIsHeader(line []byte) bool {
	var h struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(line, &h) == nil && h.Type == "session"
}

// afLocalHeader is the node's header with this conversation's id and cwd.
func afLocalHeader(line []byte, id, cwd string) []byte {
	h := map[string]any{"type": "session", "version": 1}
	if len(line) > 0 {
		_ = json.Unmarshal(line, &h)
	}
	h["id"], h["cwd"] = id, cwd
	if _, ok := h["timestamp"]; !ok {
		h["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	raw, _ := json.Marshal(h)
	return raw
}

// afHeaderCwd is the cwd a header in file names: none for a sealed cell's.
func afHeaderCwd(file, workspace string) string {
	if filepath.Base(filepath.Dir(file)) == ".cell" {
		return ""
	}
	return workspace
}

func afPhaseState(status string) factory.PhaseState {
	switch strings.ToLower(status) {
	case "running", "waiting":
		return factory.PhaseRunning
	case "succeeded", "completed", "success", "done":
		return factory.PhaseDone
	case "failed", "timeout", "error":
		return factory.PhaseFailed
	case "cancelled", "canceled":
		return factory.PhaseSkipped
	}
	return factory.PhasePending
}

func afEnded(status string) bool {
	switch afPhaseState(status) {
	case factory.PhaseDone, factory.PhaseFailed, factory.PhaseSkipped:
		return true
	}
	return false
}

func afSucceeded(status string) bool { return afPhaseState(status) == factory.PhaseDone }

func afTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// agentFieldConn is the host's control plane client now, and its node id.
func (h *appFactoryHost) agentFieldConn() (afClient, string, bool) {
	if afTestConn != nil {
		return afTestConn()
	}
	client, settings, ok := h.agentFieldNode().Conn.Client()
	if !ok || client == nil {
		return nil, "", false
	}
	return client, settings.NodeID, true
}

// afTestConn replaces the control plane in tests.
var afTestConn func() (afClient, string, bool)

// afTellNews tells a held step chat that its transcript grew, through the
// same stage news a native step's events tell: a boundary, so the chat reads
// the journal's new entries and says each one.
func afTellNews(file string) {
	tellStageNews(file, session.Event{Kind: session.EventAssistantDone})
}

// ── STEERING A MIRRORED STEP ────────────────────────────────────────────────

// afSteerTarget is the harness execution behind the conversation file, and
// whether it is working now; false when file is no AgentField step's.
func afSteerTarget(file string) (afRelayState, bool) {
	if strings.TrimSpace(file) == "" {
		return afRelayState{}, false
	}
	return afReadRelay(file)
}

// The refusals a steer to an AgentField step can meet, in a person's words.
const (
	afSteerEnded     = "this step has ended"
	afSteerNoFactory = "this factory is not connected to AgentField, so the step cannot be steered"
)

// agentFieldSteer sends a person's words to the AgentField run behind a, when
// a is a step an AgentField node ran. handled is false for every other
// conversation, which then takes the words as it always has. STEERING IS THE
// CODEAF HARNESS'S ALONE: only a harness step has a conversation here at all,
// and only while it is working is it steerable; the node refuses anything
// else, and that refusal is the person's answer.
func (b *appBackend) agentFieldSteer(a *appAgent, text string) (bool, error) {
	b.mu.Lock()
	file := a.transcript
	b.mu.Unlock()
	state, ok := afSteerTarget(file)
	if !ok {
		return false, nil
	}
	if !state.Steerable {
		return true, appwire.Errorf(appwire.CodeConflict, afSteerEnded)
	}
	b.local.mu.Lock()
	host := b.local.host
	b.local.mu.Unlock()
	if host == nil {
		return true, appwire.Errorf(appwire.CodeConflict, afSteerNoFactory)
	}
	client, _, ok := host.agentFieldConn()
	if !ok {
		return true, appwire.Errorf(appwire.CodeConflict, afSteerNoFactory)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.Steer(ctx, state.ExecutionID, text); err != nil {
		return true, appwire.Errorf(appwire.CodeConflict, "%s", afSteerWords(err))
	}
	return true, nil
}

// afSteerWords is the refusal in a person's words: the control plane's own
// sentence when it gave one ("this step has ended", "the node did not take
// the note: …"), with the node's execution-speak said of the step.
func afSteerWords(err error) string {
	msg := err.Error()
	var refusal *agentfield.Error
	if errors.As(err, &refusal) && strings.TrimSpace(refusal.Message) != "" {
		msg = strings.TrimSpace(refusal.Message)
	}
	const nodeSays = "this execution is not running the codeaf harness, so it cannot be steered"
	if strings.Contains(msg, nodeSays) {
		return "this step is not running the codeaf harness, so it cannot be steered"
	}
	return msg
}

// ── factory.agentfield_run ──────────────────────────────────────────────────

// FactoryAgentFieldRun is a RunOn job's run as a tree, read from the job's own
// steps: each step under the one its parent names.
func (b *appBackend) FactoryAgentFieldRun(_ context.Context, p appwire.FactoryAgentFieldRunParams) (appwire.FactoryAgentFieldRun, error) {
	host, err := b.localHostOf(p.Factory)
	if err != nil {
		return appwire.FactoryAgentFieldRun{}, err
	}
	it, err := host.floor.Get(p.Job)
	if err != nil || strings.TrimSpace(it.RunOn) == "" {
		return appwire.FactoryAgentFieldRun{}, appwire.Errorf(appwire.CodeNotFound, "this job is not run on AgentField")
	}
	return afRunTree(it), nil
}

// afRunTree folds a RunOn job's flat steps into their tree.
func afRunTree(it factory.Item) appwire.FactoryAgentFieldRun {
	out := appwire.FactoryAgentFieldRun{Job: it.ID, Target: strings.TrimSpace(it.RunOn), Steps: []appwire.FactoryAgentFieldStep{}}
	phases := map[string]factory.Phase{}
	if it.Stream != nil {
		for _, ph := range it.Stream.Phases {
			phases[ph.Name] = ph
		}
		out.StartedAt, out.EndedAt = afWireTime(it.Stream.Started), afWireTime(it.Stream.Ended)
	}
	byName := map[string]*appwire.FactoryAgentFieldStep{}
	order := []string{}
	running := false
	for _, st := range it.Stages {
		ph := phases[st.Name]
		step := appwire.FactoryAgentFieldStep{Name: st.Name, Label: st.Label, Group: st.Group, State: afWireState(ph.State), Note: ph.Note, Children: []appwire.FactoryAgentFieldStep{}}
		step.StartedAt, step.EndedAt = afWireTime(ph.Started), afWireTime(ph.At)
		if chat := strings.TrimSpace(ph.Chat); chat != "" {
			step.Agent, step.Transcript = appAgentID(chat), afHasHeader(chat)
			if s, ok := afReadRelay(chat); ok {
				step.ExecutionID = s.ExecutionID
				step.Steerable = s.Steerable && step.State == "running"
			}
		}
		running = running || step.State == "running"
		byName[st.Name] = &step
		order = append(order, st.Name)
	}
	// Children are attached deepest first, so a parent carries its whole
	// subtree by the time it is attached itself.
	parent := map[string]string{}
	for _, st := range it.Stages {
		parent[st.Name] = st.Parent
	}
	depth := func(name string) int {
		d := 0
		for p := parent[name]; p != "" && d < len(order); p = parent[p] {
			d++
		}
		return d
	}
	byDepth := append([]string(nil), order...)
	sort.SliceStable(byDepth, func(i, j int) bool { return depth(byDepth[i]) > depth(byDepth[j]) })
	kids := map[string][]string{}
	for _, name := range order {
		if p := parent[name]; p != "" && byName[p] != nil {
			kids[p] = append(kids[p], name)
		}
	}
	for _, name := range byDepth {
		for _, k := range kids[name] {
			byName[name].Children = append(byName[name].Children, *byName[k])
		}
	}
	for _, name := range order {
		if p := parent[name]; p == "" || byName[p] == nil {
			out.Steps = append(out.Steps, *byName[name])
		}
	}
	switch {
	case it.State == factory.StateShipped:
		out.State = "done"
	case it.Failure != nil:
		out.State, out.Error = "failed", it.Failure.Detail
	case running:
		out.State = "running"
	case len(order) == 0:
		out.State = "queued"
	}
	return out
}

func afWireState(s factory.PhaseState) string {
	switch s {
	case factory.PhaseRunning, factory.PhaseWaiting:
		return "running"
	case factory.PhaseDone:
		return "done"
	case factory.PhaseFailed:
		return "failed"
	case factory.PhaseSkipped:
		return "cancelled"
	}
	return "queued"
}

func afWireTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

var _ appwire.FactoryAgentFieldRunner = (*appBackend)(nil)
