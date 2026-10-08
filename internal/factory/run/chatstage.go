package run

// THE CHAT EXECUTOR: A STAGE IS A CONVERSATION.
//
// A chat stage runs as one ordinary conversation, made in the item's team and
// opened with a brief compiled from the stage's ask, the item, the notes and
// what the stages before it reported. The conversation works the way any
// conversation does — it reads, changes, runs, and hands parts to tasks — and
// it ends its round by calling `stage_result` (internal/session's
// tools_stage.go), which reaches the [StageDoor] this file hands it.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - THE BRIEF IS NEVER TRUSTED; THE RESULT IS READ BY CODE. This executor
//     hands back what the conversation reported and nothing more. Whether the
//     stage met its `until` is the loop's [factory.Met], asked of this result.
//
//   - A ROUND THAT NEVER REPORTED IS NOT DONE. A conversation that stops
//     without calling `stage_result` comes back Done:false with
//     [stageNoReport] as its output, which meets no until, so the loop counts
//     the round and stops at its max rather than passing a stage that said
//     nothing.
//
//   - A CANCELLED CTX IS A STOP OR A PAUSE, and the round returns no result:
//     the conversation is closed, which interrupts a turn still in flight.
//
//   - THE MAKER IS NOT THIS FILE'S. Making a conversation in a team is the
//     talk lane's machinery (cmd/codeaf's factory_talk.go); cmd/codeaf builds
//     the real [ConversationMaker] on it, and this package carries only the
//     interfaces and [FakeMaker] for tests. A live-model proof belongs in
//     internal/e2e, behind the e2e tag, once that maker exists.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// StageDoor is what a stage conversation reports through. It is spelled
// exactly as internal/session's StageDoor is, so the value the executor builds
// is the session's door without either package importing the other.
type StageDoor interface {
	Report(ctx context.Context, result factory.StageResult) error
	Edit(ctx context.Context, edit factory.PlanEdit) error
}

// ConversationSpec is everything the maker needs to open one stage round.
type ConversationSpec struct {
	// Team is the item's team id (its Stream's Room), "" when the item has none
	// yet; the maker makes it then, under the one `factory` team, the way the
	// talk lane does.
	Team string
	// Name is the conversation's name: `#12 · review`, or `#12 · review 1/2`
	// when the stage may run more than one round.
	Name string
	// Dir is the repository's checkout, "" when unknown.
	Dir string
	// Brief is the conversation's opening ([stageBrief]). The maker seeds it and
	// starts the first turn on it.
	Brief string
	// Stage is the door the conversation's `stage_result` and `plan_edit`
	// reach. The maker sets it as the conversation's session door
	// (session.Config.Stage) and on nothing else.
	Stage StageDoor
}

// Conversation is one open stage conversation.
type Conversation interface {
	// Send hands the person's words to the conversation while it runs.
	Send(ctx context.Context, words string) error
	// Wait returns when the conversation is idle: its turn has ended AND
	// nothing it handed out is still running, because a conversation that
	// spread its work over tasks ends its turn and is woken by their landings.
	// A non-nil error is a turn that could not go on.
	Wait(ctx context.Context) error
	// Captions is the conversation's running account, one line at a time, for
	// the item's stream. A nil channel is a conversation that says nothing.
	Captions(ctx context.Context) <-chan string
	// ID names the conversation so the phase strip can open it.
	ID() string
	// Close lets go of the conversation and interrupts a turn still in flight.
	// The conversation itself stays where it was made, to be opened.
	Close()
}

// ConversationMaker opens stage conversations.
type ConversationMaker interface {
	Open(ctx context.Context, spec ConversationSpec) (Conversation, error)
}

// spender is what a conversation may also answer: what its round cost.
type spender interface {
	Spent() float64
}

// The sentences this file writes. stageNoReport is the output of a round
// that ended without `stage_result`; the brief's last line is briefClosing.
const (
	stageNoReport     = "the stage ended without reporting"
	briefClosing      = "End by calling stage_result once."
	briefNoTeamPost   = "You have no team_post tool; report through stage_result only. Ignore team notices about renames."
	stageNoMaker      = "codeaf cannot open a conversation for a stage here"
	stageSecondReport = "this stage already reported, and its first report stands"
	steerNotDelivered = "your words did not reach the stage: "
)

// briefBodyMost is how much of the item's body the brief carries, in
// characters. The conversation can read the rest where the item lives.
const briefBodyMost = 2000

// briefLastMost is how much of the last round's findings a fix-then-check
// round's brief carries, in characters.
const briefLastMost = 1500

// handFold is how long a run of `read handed to quick task N` captions is
// held for more of the same before it is logged as one line. A variable so
// tests do not wait it out.
var handFold = 400 * time.Millisecond

// reportGrace is how long the executor waits for a turn to end after the
// conversation reported. The tool tells the model the stage ends when it
// stops; one that reports and keeps going is closed when this runs out, and
// its report stands. A variable so tests do not wait it out.
var reportGrace = 2 * time.Minute

// NewChatExecutor is the executor for [factory.StageChat] stages, opening each
// round's conversation through maker.
func NewChatExecutor(maker ConversationMaker) Executor {
	return chatExecutor{maker: maker}
}

type chatExecutor struct {
	maker ConversationMaker
}

// Run opens the round's conversation, forwards steering and captions while it
// works, and answers what it reported.
func (e chatExecutor) Run(ctx context.Context, job Job) (factory.StageResult, error) {
	if e.maker == nil {
		return factory.StageResult{}, errors.New(stageNoMaker)
	}
	door := newStageDoor()
	conv, err := e.maker.Open(ctx, ConversationSpec{
		Team:  itemTeam(job.Item),
		Name:  stageChatName(job),
		Dir:   job.Dir,
		Brief: stageBrief(job),
		Stage: door,
	})
	if err != nil {
		return factory.StageResult{}, err
	}
	defer conv.Close()
	log := job.Log
	if log == nil {
		log = func(string) {}
	}
	captions := conv.Captions(ctx)
	steer := job.Steer
	idle := make(chan error, 1)
	go func() { idle <- conv.Wait(ctx) }()

	// ONE BATCHED READ IS ONE LINE. A conversation that hands several reads
	// to a quick task at once captions each of them, and the 2026-10-08 hand
	// run logged `read handed to quick task 1` four times in a row; a run of
	// such captions for one quick task is held briefly and logged folded.
	var hand handRun
	var handTimer *time.Timer
	var handDue <-chan time.Time
	defer func() {
		if handTimer != nil {
			handTimer.Stop()
		}
	}()
	flush := func() {
		if line := hand.line(); line != "" {
			log(line)
		}
		hand = handRun{}
		handDue = nil
	}
	caption := func(line string) {
		if line = oneLine(line); line == "" {
			return
		}
		if tool, task, ok := handedCaption(line); ok {
			if !hand.add(tool, task) {
				flush()
				hand.add(tool, task)
			}
			if handTimer == nil {
				handTimer = time.NewTimer(handFold)
			} else {
				handTimer.Reset(handFold)
			}
			handDue = handTimer.C
			return
		}
		flush()
		log(line)
	}

	var grace <-chan time.Time
	var waitErr error
loop:
	for {
		select {
		case <-ctx.Done():
			return factory.StageResult{}, ctx.Err()
		case words, ok := <-steer:
			if !ok {
				steer = nil
				continue
			}
			if words = strings.TrimSpace(words); words == "" {
				continue
			}
			if err := conv.Send(ctx, words); err != nil {
				log(steerNotDelivered + oneLine(err.Error()))
			}
		case line, ok := <-captions:
			if !ok {
				captions = nil
				continue
			}
			caption(line)
		case <-handDue:
			flush()
		case <-door.reported:
			// THE REPORT IS IN AND THE TURN IS OWED ITS END. The door's channel
			// is closed, so it is taken out of the select and a timer stands in.
			door.reported = nil
			timer := time.NewTimer(reportGrace)
			defer timer.Stop()
			grace = timer.C
		case <-grace:
			break loop
		case waitErr = <-idle:
			break loop
		}
	}
	if ctx.Err() != nil {
		return factory.StageResult{}, ctx.Err()
	}
	// WHAT WAS ALREADY SAID IS STILL LOGGED: a caption sent just before the turn
	// ended is in the channel, not lost.
	for drained := false; captions != nil && !drained; {
		select {
		case line, ok := <-captions:
			if !ok {
				drained = true
			} else {
				caption(line)
			}
		default:
			drained = true
		}
	}
	flush()
	res := door.result()
	if !res.Done && door.empty() {
		res.Output = stageNoReport
		if waitErr != nil {
			res.Output += ": " + oneLine(waitErr.Error())
		}
	}
	res.Chat = conv.ID()
	if s, ok := conv.(spender); ok {
		res.Spent = s.Spent()
	}
	return res, nil
}

// itemTeam is the item's team id, "" when it has none yet.
func itemTeam(it factory.Item) string {
	if it.Stream == nil {
		return ""
	}
	return strings.TrimSpace(it.Stream.Room)
}

// stageChatName is the conversation's name: the item's ref and the stage's,
// with the round over the rounds when there may be more than one.
func stageChatName(job Job) string {
	name := job.Item.Ref() + " · " + stageLabel(job.Stage)
	if job.Stage.Max > 1 {
		round := job.Round
		if round < 1 {
			round = 1
		}
		name += " " + strconv.Itoa(round) + "/" + strconv.Itoa(job.Stage.Max)
	}
	return name
}

// stageLabel is a stage's name, or its ask's first words when it has none.
func stageLabel(s factory.Stage) string {
	if name := oneLine(s.Name); name != "" {
		return name
	}
	return factory.ParseStage(s.Ask).Name
}

// stageBrief is the conversation's opening: the ask in one line, the item, the
// notes, what came before, the stage's knobs in words, and the one sentence
// that says how it ends.
//
// EVERY PART IS LEFT OUT WHEN THERE IS NOTHING TO SAY (the emptiness law),
// except the ask and the closing, which every stage has.
func stageBrief(job Job) string {
	var b strings.Builder
	para := func(lines ...string) {
		var kept []string
		for _, l := range lines {
			if l = strings.TrimRight(l, " \n"); l != "" {
				kept = append(kept, l)
			}
		}
		if len(kept) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.Join(kept, "\n"))
	}
	ask := stageLabel(job.Stage)
	if a := oneLine(job.Stage.Ask); a != "" {
		ask += ": " + a
	}
	para(ask)
	head := job.Item.Ref()
	if t := oneLine(job.Item.Title); t != "" {
		head += " · " + t
	}
	if r := oneLine(job.Item.Repo); r != "" {
		head += " · " + r
	}
	para(head, cutBody(job.Item.Body))
	para(stagePlace(job))
	var notes []string
	for _, n := range job.Notes {
		if n = oneLine(n); n != "" {
			notes = append(notes, "- "+n)
		}
	}
	if len(notes) > 0 {
		para(append([]string{"notes:"}, notes...)...)
	}
	if prior := priorLines(job); len(prior) > 0 {
		para(append([]string{"before this stage:"}, prior...)...)
	}
	para(fixThenCheck(job))
	para(stageKnobs(job))
	para(briefNoTeamPost)
	para(briefClosing)
	return b.String()
}

// fixThenCheck is the paragraph that makes a round after the first a fix
// before it is a look: `round 2 of review. The last round found: … Fix those
// in the checkout first, run the tests, then review again and report only
// what remains.` "" on round 1, for a stage whose until is done, and when the
// last round left nothing to name.
//
// A ROUND THAT ONLY LOOKS AGAIN FINDS THE SAME THING AGAIN. On the 2026-10-08
// hand run review found one finding, the person said one more round, and round
// 2 reviewed the same unchanged tree and found it again; until clean never
// converged, because no round was ever asked to change anything.
func fixThenCheck(job Job) string {
	if job.Round < 2 || job.Last == nil {
		return ""
	}
	switch strings.TrimSpace(job.Stage.Until) {
	case factory.UntilClean, factory.UntilGreen, factory.UntilProven:
	default:
		return ""
	}
	found := lastFound(*job.Last)
	if found == "" {
		return ""
	}
	name := stageLabel(job.Stage)
	return "round " + strconv.Itoa(job.Round) + " of " + name + ". The last round found: " + found +
		" Fix those in the checkout first, run the tests, then " + name + " again and report only what remains."
}

// lastFound is what a round found, in words: its output, then each claim it
// could not show, cut to [briefLastMost] characters and closed with a full
// stop so the next sentence reads as one.
func lastFound(r factory.StageResult) string {
	var parts []string
	if out := strings.TrimSpace(r.Output); out != "" {
		parts = append(parts, oneLine(out))
	}
	var bad []string
	for _, cl := range r.Claims {
		if !cl.OK {
			if t := oneLine(cl.Text); t != "" {
				bad = append(bad, t)
			}
		}
	}
	if len(bad) > 0 {
		parts = append(parts, "not shown: "+strings.Join(bad, "; "))
	}
	found := strings.Join(parts, " · ")
	if r := []rune(found); len(r) > briefLastMost {
		found = strings.TrimSpace(string(r[:briefLastMost])) + " …"
	}
	if found != "" && !strings.HasSuffix(found, ".") {
		found += "."
	}
	return found
}

// handedSuffix is how a caption says a call went to a quick task:
// `read handed to quick task 1` (internal/session's readhandoff.go hint).
const handedSuffix = " handed to quick task "

// handedCaption reads `read handed to quick task 1` as the tool and the task.
func handedCaption(line string) (tool, task string, ok bool) {
	i := strings.Index(line, handedSuffix)
	if i <= 0 {
		return "", "", false
	}
	tool, task = line[:i], line[i+len(handedSuffix):]
	if strings.ContainsAny(tool, " ") || task == "" {
		return "", "", false
	}
	if _, err := strconv.Atoi(task); err != nil {
		return "", "", false
	}
	return tool, task, true
}

// handRun is a run of calls handed to one quick task, each tool counted in
// the order it first came.
type handRun struct {
	task  string
	tools []string
	count map[string]int
}

// add counts one call, and answers false when it went to another task.
func (h *handRun) add(tool, task string) bool {
	if h.task != "" && h.task != task {
		return false
	}
	if h.count == nil {
		h.count = map[string]int{}
	}
	h.task = task
	if h.count[tool] == 0 {
		h.tools = append(h.tools, tool)
	}
	h.count[tool]++
	return true
}

// line is the run as one line: `read ×4 · find handed to quick task 1`, and
// the caption as it came when there was one call.
func (h *handRun) line() string {
	if h.task == "" {
		return ""
	}
	words := make([]string, 0, len(h.tools))
	for _, t := range h.tools {
		if n := h.count[t]; n > 1 {
			words = append(words, t+" ×"+strconv.Itoa(n))
		} else {
			words = append(words, t)
		}
	}
	return strings.Join(words, " · ") + handedSuffix + h.task
}

// stagePlace is the run's stages in order and which one this is, with the
// sentence that keeps a stage to its own part: `stages: plan › write › test ›
// review › proof · this is plan: do this stage's part, and leave the rest to
// the stages after it`. "" when the item carries no phases to name.
//
// A STAGE THAT DOES NOT KNOW IT IS ONE OF SEVERAL DOES ALL OF THEM. On the
// 2026-10-08 run on factory-demo, plan's brief said only `plan: read the
// issue and say how`, and the conversation (unattended, at the allow posture,
// told its work is judged on being done) wrote the fix and its test before
// write had started. The order is read off the item's phases, which are the
// stages that fit this item, so a skipped stage is not named.
func stagePlace(job Job) string {
	if job.Item.Stream == nil || len(job.Item.Stream.Phases) < 2 {
		return ""
	}
	names := make([]string, 0, len(job.Item.Stream.Phases))
	for _, ph := range job.Item.Stream.Phases {
		if n := oneLine(ph.Name); n != "" {
			names = append(names, n)
		}
	}
	me := stageLabel(job.Stage)
	if len(names) < 2 || me == "" {
		return ""
	}
	line := "stages: " + strings.Join(names, " › ") + " · this is " + me
	if names[len(names)-1] == me {
		return line + ", the last: do this stage's part"
	}
	return line + ": do this stage's part, and leave the rest to the stages after it"
}

// cutBody is the item's body, at most [briefBodyMost] characters, cut at a
// rune and marked when cut.
func cutBody(body string) string {
	body = strings.TrimSpace(body)
	r := []rune(body)
	if len(r) <= briefBodyMost {
		return body
	}
	return strings.TrimSpace(string(r[:briefBodyMost])) + " …"
}

// priorLines is one line per stage that ran before this one: `write: done ·
// 3 claims · what it said`. The names come from the item's stages that are on
// and before this one, when they line up with Prior one for one; otherwise
// each is named by its place.
func priorLines(job Job) []string {
	if len(job.Prior) == 0 {
		return nil
	}
	before := job.Item.Stages
	if job.Index >= 0 && job.Index < len(before) {
		before = before[:job.Index]
	}
	var names []string
	for _, s := range before {
		if s.On {
			names = append(names, stageLabel(s))
		}
	}
	lines := make([]string, 0, len(job.Prior))
	for i, r := range job.Prior {
		name := "stage " + strconv.Itoa(i+1)
		if len(names) == len(job.Prior) && names[i] != "" {
			name = names[i]
		}
		lines = append(lines, name+": "+resultWords(r))
	}
	return lines
}

// resultWords is one result in a row: done or incomplete, then only the counts
// that are not zero, then the first line of what it said.
func resultWords(r factory.StageResult) string {
	parts := []string{"incomplete"}
	if r.Done {
		parts[0] = "done"
	}
	if r.Findings > 0 {
		parts = append(parts, plural(r.Findings, "finding"))
	}
	if len(r.Claims) > 0 {
		parts = append(parts, plural(len(r.Claims), "claim"))
	}
	if r.Exit != 0 {
		parts = append(parts, "exit "+strconv.Itoa(r.Exit))
	}
	if out := firstLine(r.Output); out != "" {
		parts = append(parts, out)
	}
	return strings.Join(parts, " · ")
}

// stageKnobs is the stage's structure in words: `until clean · max 2 · fanout
// per-finding · effort strong`, with `round 2` once it has looped.
func stageKnobs(job Job) string {
	s := job.Stage
	until := strings.TrimSpace(s.Until)
	if until == "" {
		until = factory.UntilDone
	}
	parts := []string{"until " + until}
	if s.Max > 1 {
		parts = append(parts, "max "+strconv.Itoa(s.Max))
	}
	if f := oneLine(s.Fanout); f != "" && f != "one" {
		parts = append(parts, "fanout "+f)
	}
	if e := oneLine(s.Effort); e != "" {
		parts = append(parts, "effort "+e)
	}
	if job.Round > 1 {
		parts = append(parts, "round "+strconv.Itoa(job.Round))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// firstLine is the first non-empty line of s, at most 120 characters.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = oneLine(l); l != "" {
			if r := []rune(l); len(r) > 120 {
				return strings.TrimSpace(string(r[:120])) + " …"
			}
			return l
		}
	}
	return ""
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// stageDoor is the door one round hands its conversation. It keeps the first
// report and every proposal, merged in the order they came.
type stageDoor struct {
	mu       sync.Mutex
	report   *factory.StageResult
	edit     factory.PlanEdit
	edited   bool
	reported chan struct{}
}

func newStageDoor() *stageDoor { return &stageDoor{reported: make(chan struct{})} }

// Report keeps the round's result. A SECOND REPORT IS REFUSED, in words the
// model reads, because the brief and the tool both say once and a stage that
// changed its account would leave two answers for one round.
func (d *stageDoor) Report(_ context.Context, result factory.StageResult) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.report != nil {
		return errors.New(stageSecondReport)
	}
	r := result
	r.Claims = append([]factory.Claim(nil), result.Claims...)
	r.Notes = append([]string(nil), result.Notes...)
	d.report = &r
	close(d.reported)
	return nil
}

// Edit keeps a proposal. Nothing is applied here: the loop applies the
// round's edit through [factory.Adapt] after the round.
func (d *stageDoor) Edit(_ context.Context, edit factory.PlanEdit) error {
	if edit.Empty() {
		return errors.New("the proposal changes nothing")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.edit.Add = append(d.edit.Add, edit.Add...)
	d.edit.On = append(d.edit.On, edit.On...)
	d.edit.Skip = append(d.edit.Skip, edit.Skip...)
	if why := strings.TrimSpace(edit.Why); why != "" {
		d.edit.Why = why
	}
	d.edited = true
	return nil
}

// result is the report, or a zero result when there was none, with the
// round's merged proposal on it either way.
func (d *stageDoor) result() factory.StageResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	var r factory.StageResult
	if d.report != nil {
		r = *d.report
	}
	if d.edited {
		e := d.edit
		r.Edit = &e
	}
	return r
}

// empty says no report arrived.
func (d *stageDoor) empty() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.report == nil
}

// ── the fake maker ─────────────────────────────────────────────────────────

// FakeMaker opens [FakeConversation]s for tests, here and in the loop's. Each
// conversation runs Script in its own goroutine as the model would, and its
// turn ends when Script returns. A nil Script is a conversation that stops at
// once without reporting.
type FakeMaker struct {
	Script func(ctx context.Context, c *FakeConversation)
	// Fail, when set, is what Open answers instead of a conversation.
	Fail error

	mu    sync.Mutex
	convs []*FakeConversation
}

// Open starts one fake conversation.
func (m *FakeMaker) Open(ctx context.Context, spec ConversationSpec) (Conversation, error) {
	if m.Fail != nil {
		return nil, m.Fail
	}
	m.mu.Lock()
	c := &FakeConversation{
		Spec:     spec,
		id:       fmt.Sprintf("fake-%d", len(m.convs)+1),
		heard:    make(chan string, 16),
		captions: make(chan string, 64),
		done:     make(chan struct{}),
		closed:   make(chan struct{}),
	}
	m.convs = append(m.convs, c)
	m.mu.Unlock()
	turn, cancel := context.WithCancel(ctx)
	go func() {
		<-c.closed
		cancel()
	}()
	go func() {
		defer close(c.done)
		if m.Script != nil {
			m.Script(turn, c)
		}
	}()
	return c, nil
}

// Opened is every conversation opened so far, in order.
func (m *FakeMaker) Opened() []*FakeConversation {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*FakeConversation(nil), m.convs...)
}

// FakeConversation is one conversation [FakeMaker] opened.
type FakeConversation struct {
	Spec ConversationSpec

	id       string
	heard    chan string
	captions chan string
	done     chan struct{}
	closed   chan struct{}

	mu       sync.Mutex
	sent     []string
	isClosed bool
}

// Send records the words and hands them to the script through Heard.
func (c *FakeConversation) Send(_ context.Context, words string) error {
	c.mu.Lock()
	c.sent = append(c.sent, words)
	c.mu.Unlock()
	select {
	case c.heard <- words:
	default:
	}
	return nil
}

// Wait returns when the script does, or when ctx ends.
func (c *FakeConversation) Wait(ctx context.Context) error {
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Captions is what Say sends.
func (c *FakeConversation) Captions(context.Context) <-chan string { return c.captions }

// ID is `fake-1`, `fake-2`, … in the order they were opened.
func (c *FakeConversation) ID() string { return c.id }

// Close ends the script's context; a second Close does nothing.
func (c *FakeConversation) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.isClosed {
		c.isClosed = true
		close(c.closed)
	}
}

// Closed says whether the executor closed it.
func (c *FakeConversation) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isClosed
}

// Sent is every word the executor forwarded, in order.
func (c *FakeConversation) Sent() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.sent...)
}

// Heard is the script's side of Send.
func (c *FakeConversation) Heard() <-chan string { return c.heard }

// Say puts one caption on the conversation's running account.
func (c *FakeConversation) Say(line string) { c.captions <- line }
