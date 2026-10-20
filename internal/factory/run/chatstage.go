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
//   - A ROUND ENDS WHEN THE STEP STOPS, WITH ITS LAST REPORT. Nothing in the
//     conversation decides the stage is finished: it has no goal owner
//     (internal/session's newPrincipalFor), so it works until it is idle. Every
//     `stage_result` it calls replaces the one before, so a step that reported
//     early and went on working is judged on what it said last. One that stops
//     without reporting is asked again, then made to report, and in the end
//     reported for (stagereport.go), because a model that forgot the closing
//     call has almost always done the work. Only when all of that comes back
//     empty is the round Done:false with [stageNoReport] as its output, which
//     meets no until. [StageWall] bounds the whole round.
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
	// Handle is the conversation's address in the item's team, given once and
	// never chosen again: the stage's own name, `review`, and `review2` for its
	// second round's conversation ([stageHandle]). "" leaves the team to guess.
	Handle string
	// Dir is the repository's checkout, "" when unknown.
	Dir string
	// Brief is the conversation's opening ([stageBrief]). The maker seeds it and
	// starts the first turn on it.
	Brief string
	// Model is the model the conversation opens on: the job's own
	// (Item.Model), "" for the default the maker's launch already carries.
	Model string
	// Resume is the transcript of a conversation to reopen instead of making
	// one, "" for a new one. The maker then continues it with Brief as a
	// carry-on note (internal/session's CarryOn), joins no team (it is a
	// member already), and the person's chat is the same chat.
	Resume string
	// Stage is the door the conversation's `stage_result` and `plan_edit`
	// reach. The maker sets it as the conversation's session door
	// (session.Config.Stage) and on nothing else.
	Stage StageDoor
}

// Conversation is one open stage conversation.
type Conversation interface {
	// Send hands the person's words to the conversation while it runs.
	Send(ctx context.Context, words string) error
	// Note hands codeaf's own words to the conversation, as the next turn when
	// it is idle. The chat shows them as codeaf's, never as the person's.
	Note(ctx context.Context, words string) error
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

// modeler is what a conversation may also do: say which model it is on, and
// switch to another for its next turn, as a person's pick does in any chat.
type modeler interface {
	Model() string
	SetModel(model string)
}

// The sentences this file writes. stageNoReport is the output of a round
// that ended without `stage_result` however it was asked (stagereport.go);
// the brief's last line is briefClosing.
const (
	stageNoReport   = "the stage ended without reporting"
	briefClosing    = "End by calling stage_result with your final result."
	briefNoTeamPost = "You have no team_post tool; report through stage_result only."
	briefAsk        = "A question the brief and the checkout do not settle goes to the item's manager through ask, and you wait for its answer; ask only what you cannot settle yourself."
	stageNoMaker    = "codeaf cannot open a conversation for a stage here"
	// stageWalled is the output of a round [StageWall] ended before it reported.
	stageWalled       = "the step ran for its whole allowance without reporting"
	steerNotDelivered = "your words did not reach the stage: "
	// stageCarryOn is the note a paused stage is carried on with; the maker
	// puts the carry-on lead on it so the chat shows it as codeaf's, not the
	// person's. stageNoResume is the log line when the chat cannot be reopened.
	stageCarryOn  = "You were paused and the person has run you again. Carry on with this stage from exactly where you stopped, in this same conversation; do not start over. " + briefClosing
	stageNoResume = "the stage's chat could not be reopened, starting it again: "
	// stageWhilePaused leads what was answered while the stage was paused.
	stageWhilePaused = "While you were paused, your question was answered: "
)

// carryOnBrief is the note a paused stage is carried on with: [stageCarryOn],
// with what was answered while it was paused before its closing line.
func carryOnBrief(lines []string) string {
	var kept []string
	for _, l := range lines {
		if l = oneLine(l); l != "" {
			kept = append(kept, l)
		}
	}
	if len(kept) == 0 {
		return stageCarryOn
	}
	head := strings.TrimSuffix(stageCarryOn, briefClosing)
	return head + stageWhilePaused + strings.Join(kept, "; ") + ". " + briefClosing
}

// briefBodyMost is how much of the item's body the brief carries, in
// characters: the whole of what the source keeps, so a pull request's claims
// at the end of its body reach the step.
const briefBodyMost = 6000

// briefLastMost is how much of the last round's report a later round's brief
// carries, in characters. It was 1,500 until 2026-10-20, which cut a review's
// list of what it found partway through the first few findings.
const briefLastMost = 4000

// StageWall is the most one round of a chat stage runs for. The runner owns it
// because nothing inside the conversation does: a stage has no goal owner to
// spend a wall against. cmd/codeaf hands the same figure to the conversation as
// its budget, so what it hands out to tasks is bounded by it too.
const StageWall = 2 * time.Hour

// stageWall is [StageWall], a variable so tests do not wait it out.
var stageWall = StageWall

// NewChatExecutor is the executor for [factory.StageChat] stages, opening each
// round's conversation through maker.
func NewChatExecutor(maker ConversationMaker) Executor {
	return chatExecutor{maker: maker}
}

type chatExecutor struct {
	maker ConversationMaker
}

// Run opens the round's conversation, forwards steering and captions while it
// works, and answers the last thing it reported, or the report made for it.
func (e chatExecutor) Run(ctx context.Context, job Job) (factory.StageResult, error) {
	if e.maker == nil {
		return factory.StageResult{}, errors.New(stageNoMaker)
	}
	door := newStageDoor()
	door.ask, door.stage = job.Ask, job.Stage
	log := job.Log
	if log == nil {
		log = func(string) {}
	}
	spec := ConversationSpec{
		Team:   itemTeam(job.Item),
		Name:   stageChatName(job),
		Handle: stageHandle(job),
		Dir:    job.Dir,
		Brief:  stageBrief(job),
		Model:  strings.TrimSpace(job.Item.Model),
		Stage:  door,
	}
	var conv Conversation
	var err error
	// A PAUSED STAGE CARRIES ON IN ITS OWN CHAT. A chat that cannot be
	// reopened is said so and the stage opens a new one on the whole brief,
	// rather than failing a round the person only paused.
	if job.Resume != "" {
		resume := spec
		resume.Resume, resume.Brief = job.Resume, carryOnBrief(job.CarryOn)
		if conv, err = e.maker.Open(ctx, resume); err != nil {
			if ctx.Err() != nil {
				return factory.StageResult{}, ctx.Err()
			}
			log(stageNoResume + oneLine(err.Error()))
			conv = nil
		}
	}
	if conv == nil {
		if conv, err = e.maker.Open(ctx, spec); err != nil {
			return factory.StageResult{}, err
		}
	}
	defer conv.Close()
	if job.Opened != nil {
		job.Opened(conv.ID())
	}
	captions := conv.Captions(ctx)
	steer, models := job.Steer, job.Models
	idle := make(chan error, 1)
	go func() { idle <- conv.Wait(ctx) }()

	caption := func(line string) {
		if line = oneLine(line); line != "" {
			log(line)
		}
	}

	var waitErr error
	asker := stageAsker{job: job}
	walled := false
	wall := time.NewTimer(stageWall)
	defer wall.Stop()
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
		case model, ok := <-models:
			if !ok {
				models = nil
				continue
			}
			if m, ok := conv.(modeler); ok && strings.TrimSpace(model) != "" {
				m.SetModel(strings.TrimSpace(model))
			}
		case line, ok := <-captions:
			if !ok {
				captions = nil
				continue
			}
			caption(line)
		case <-wall.C:
			walled = true
			break loop
		case waitErr = <-idle:
			// THE ROUND ENDS WHEN THE STEP STOPS, NOT WHEN IT FIRST REPORTS. A step
			// that reported and went on working is waited on until it is idle,
			// however long that takes inside the wall, and its last report stands;
			// one that is idle with nothing reported is asked again (stagereport.go)
			// until there is nothing left to ask. A spending limit is never asked
			// past: the turn it would start is refused before it begins.
			if !door.empty() || ctx.Err() != nil {
				break loop
			}
			if _, stopped := spendStopOf(waitErr); stopped {
				break loop
			}
			line, ok := asker.next(ctx, conv, waitErr)
			if !ok {
				break loop
			}
			log(line)
			go func() { idle <- conv.Wait(ctx) }()
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
	res := door.result()
	if door.empty() {
		made, line := unreported(ctx, conv, job, spec.Brief, waitErr, walled)
		if ctx.Err() != nil {
			return factory.StageResult{}, ctx.Err()
		}
		if line != "" {
			log(line)
		}
		made.Edit = res.Edit
		res = made
	}
	res.Chat = conv.ID()
	if s, ok := conv.(spender); ok {
		res.Spent = s.Spent()
	}
	if m, ok := conv.(modeler); ok {
		res.Model = m.Model()
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

// stageHandle is the stage conversation's address in the item's team: the
// stage's name, lowercase, with the round after it from round 2 on (`review2`),
// cut to fit a handle. "" when the name cannot be one.
//
// A STAGE IS CALLED WHAT IT IS, FOR GOOD. A member with no given handle is
// renamed by its title model, and the team then hears `@plan is now @dispute`
// and so does the stage, in the middle of a plan the person reads as `plan`
// (2026-10-18). A given handle is never chosen again (internal/teams).
func stageHandle(job Job) string {
	var b strings.Builder
	for _, r := range strings.ToLower(stageLabel(job.Stage)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	name, round := b.String(), ""
	if job.Round > 1 {
		round = strconv.Itoa(job.Round)
	}
	if most := stageHandleMost - len(round); len(name) > most {
		name = name[:max(most, 0)]
	}
	if len(name) < 2 {
		return ""
	}
	return name + round
}

// stageHandleMost is the longest a team handle may be (internal/teams'
// HandleMax), spelled here so this package does not import the teams store.
const stageHandleMost = 12

// stageAsk is the brief's first line, the stage's name and its ask: `review:
// read it as a stranger would`. A reminder says it again (stagereport.go).
func stageAsk(job Job) string {
	ask := stageLabel(job.Stage)
	if a := oneLine(job.Stage.Ask); a != "" {
		ask += ": " + a
	}
	return ask
}

// stageLabel is a stage's name, or its ask's first words when it has none.
func stageLabel(s factory.Stage) string {
	if name := oneLine(s.Name); name != "" {
		return name
	}
	return factory.ParseStage(s.Ask).Name
}

// stageBrief is the conversation's opening: the ask in one line, the step's
// role, the item as the step needs it (brief.go: its facts, files, checks,
// comments and where the change is in the work tree), the stages and which
// one this is (and, for the plan, where its plan goes), the notes, what the
// steps before it handed on (handover.go: the plan whole, the files it names,
// each step's report, the claims so far), the stage's knobs in words, and
// the sentences that say where questions go and how it ends.
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
	para(stageAsk(job))
	para(stageRole(job))
	for _, p := range itemContext(job) {
		para(p...)
	}
	para(stagePlace(job))
	para(planBrief(job))
	var notes []string
	for _, n := range job.Notes {
		if n = oneLine(n); n != "" {
			notes = append(notes, "- "+n)
		}
	}
	if len(notes) > 0 {
		para(append([]string{"notes:"}, notes...)...)
	}
	for _, p := range handover(job) {
		para(p...)
	}
	para(fixThenCheck(job))
	para(stageKnobs(job))
	para(untilSays(job))
	para(briefNoTeamPost)
	para(briefAsk)
	para(briefClosing)
	return b.String()
}

// fixThenCheck is the paragraph that opens every round after the first with
// what the round before it reported, and what to do with it:
//
//	round 2 of review. The last round reported:
//	the refund is counted twice in Export
//	1 finding still open
//	not shown: refunds count once
//	Fix what it left in the checkout first, run the tests, then review again and report only what is still wrong.
//
// A stage that runs until done is told it did not finish and to carry on from
// what is in the checkout instead. "" on round 1 and when nothing came back.
//
// A ROUND THAT ONLY LOOKS AGAIN FINDS THE SAME THING AGAIN. On the 2026-10-08
// hand run review found one finding, the person said one more round, and round
// 2 reviewed the same unchanged tree and found it again; until clean never
// converged, because no round was ever asked to change anything.
//
// EVERY ROUND IS A FRESH CONVERSATION, SO THIS IS ALL IT KNOWS OF THE LAST.
// Until 2026-10-20 only a stage that runs until clean, green or proven was told,
// and only the first line of what it said: a plan or a write that went round
// again started from nothing, and a review lost the findings it had listed.
func fixThenCheck(job Job) string {
	if job.Round < 2 || job.Last == nil {
		return ""
	}
	name := stageLabel(job.Stage)
	head := "round " + strconv.Itoa(job.Round) + " of " + name + "."
	if found := lastFound(*job.Last); found != "" {
		head += " The last round reported:\n" + found
	}
	switch strings.TrimSpace(job.Stage.Until) {
	case factory.UntilClean, factory.UntilGreen, factory.UntilProven:
		return head + "\nFix what it left in the checkout first, run the tests, then " + name + " again and report only what is still wrong."
	}
	return head + "\nIt did not finish. Carry on from what it left in the checkout, and report done once the ask is carried out."
}

// untilSays is the sentence a stage judged on what it leaves reads about how
// it is judged: what findings count for one that runs until clean, what a
// claim needs for one that runs until proven, "" for any other stage. IT IS
// SAID HERE, ONCE PER STEP, NOT IN `stage_result`'s DESCRIPTION, which every
// call of every stage pays for.
//
// A REVIEW THAT FIXED WHAT IT FOUND IS CLEAN. Until 2026-10-20 nothing said
// whether findings counted what a round found or what it left, and a reviewer
// that found three problems, fixed them, and honestly reported three failed.
// A proof claim with blank evidence was never proven, and nothing said so.
func untilSays(job Job) string {
	switch strings.TrimSpace(job.Stage.Until) {
	case factory.UntilClean:
		return "findings counts only what is still wrong once this round's own fixes are in: a problem you found and fixed is not a finding, and " + stageLabel(job.Stage) + " is clean at zero."
	case factory.UntilProven:
		return "every claim needs its evidence, what shows it (the command and its result, the file, the screenshot): a claim with none is not proven."
	}
	return ""
}

// lastFound is a round's report as the next round reads it, one part to a
// line: what it said, how many findings it left open, and each claim it could
// not show with its evidence. It is cut to [briefLastMost] characters and
// marked where it was cut. ITS NOTES ARE NOT REPEATED HERE: every round's notes
// are folded into the job's ([floorLoop.fold]) and open the brief already.
func lastFound(r factory.StageResult) string {
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(r.Output), "\n") {
		if l = oneLine(l); l != "" {
			lines = append(lines, l)
		}
	}
	if r.Findings > 0 {
		lines = append(lines, plural(r.Findings, "finding")+" still open")
	}
	for _, cl := range r.Claims {
		if cl.OK {
			continue
		}
		if t := oneLine(cl.Text); t != "" {
			if ev := oneLine(cl.Evidence); ev != "" {
				t += " (" + ev + ")"
			}
			lines = append(lines, "not shown: "+t)
		}
	}
	found := strings.Join(lines, "\n")
	if r := []rune(found); len(r) > briefLastMost {
		found = strings.TrimSpace(string(r[:briefLastMost])) + " …"
	}
	return found
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
func cutBody(body string) string { return cutAt(body, briefBodyMost) }

// resultWords is one result's head in a row: done or incomplete, then only
// the counts that are not zero, `done · 2 findings · 3 claims · exit 1`. What
// it said and what it claimed are carried under it (handover.go).
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
	return strings.Join(parts, " · ")
}

// stageKnobs is the stage's structure in words: `until clean · max 2 · fanout
// per-finding · thinking strong`, with `round 2` once it has looped.
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
		parts = append(parts, "thinking "+e)
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

// stageDoor is the door one round hands its conversation. It keeps the last
// report, how many there were, and every proposal, merged in the order they
// came.
type stageDoor struct {
	mu      sync.Mutex
	report  *factory.StageResult
	reports int
	edit    factory.PlanEdit
	edited  bool
	// stage is the step the round runs, whose until a report is checked
	// against before it is held ([factory.ReportFault]). The zero stage runs
	// until done, which no report contradicts.
	stage factory.Stage
	// ask is the round's road to the manager (Job.Ask), nil for none.
	ask func(ctx context.Context, q factory.Asked) (string, error)
}

// errNobodyToAsk is a question from a round that has no road to the manager.
var errNobodyToAsk = errors.New("this stage has nobody to ask")

// Ask puts the stage's question to the item's manager and answers its
// answer, or the person's when the manager sent it on (inbox.go). It waits
// until there is one, or ctx ends.
func (d *stageDoor) Ask(ctx context.Context, q factory.Asked) (string, error) {
	if d.ask == nil {
		return "", errNobodyToAsk
	}
	return d.ask(ctx, q)
}

func newStageDoor() *stageDoor { return &stageDoor{} }

// Report keeps the round's result. EVERY REPORT IS TAKEN AND THE LAST ONE
// STANDS: a careful step reports `done: false, the test fails`, fixes the
// test, and says so again, and until 2026-10-20 its first report was final and
// a correct step was judged a failed one.
//
// THIS IS THE ONE PLACE A REPORT IS ACCEPTED. An error returned here reaches
// the model as `not reported: <why>` in the same turn (internal/session's
// tools_stage.go) and the report held before it stands, so a check of a
// report against its step's rule belongs here and nowhere else: the step puts
// it right and calls again.
//
// A REPORT THAT CONTRADICTS ITS STEP'S RULE IS NOT HELD. A review that says
// done with findings still open, or a proof claim marked ok with nothing
// showing it, is answered with the fix ([factory.ReportFault], the judge's own
// rules) and the earlier report stands. Until 2026-10-20 such a report was
// held and failed by the judge after the round, so a whole round was spent to
// learn what the call itself gave away. An honest shortfall, done: false or a
// claim marked ok: false, is held like any report and judged into another
// round. A refused call is not counted in Reports: nothing was reported.
func (d *stageDoor) Report(_ context.Context, result factory.StageResult) error {
	if why := factory.ReportFault(d.stage, result); why != "" {
		return errors.New(why)
	}
	r := result
	r.Claims = append([]factory.Claim(nil), result.Claims...)
	r.Notes = append([]string(nil), result.Notes...)
	r.Reports, r.Inferred = 0, false
	d.mu.Lock()
	defer d.mu.Unlock()
	d.report = &r
	d.reports++
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

// result is the last report and how many there were, or a zero result when
// there was none, with the round's merged proposal on it either way.
func (d *stageDoor) result() factory.StageResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	var r factory.StageResult
	if d.report != nil {
		r = *d.report
		r.Reports = d.reports
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
	// FollowUp is the turn words sent after the script's turn ended start, as
	// a conversation starts a next turn on a message that arrives while it is
	// idle. Nil is a turn that ends at once without reporting.
	FollowUp func(ctx context.Context, c *FakeConversation, words string)
	// Infer, when set, is the report the factory makes for a conversation that
	// never reported, from the ask it is handed ([inferrer]). Nil is a
	// conversation that cannot make one.
	Infer func(ctx context.Context, c *FakeConversation, ask string) (factory.StageResult, error)
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
		maker:    m,
		id:       fakeID(spec, len(m.convs)+1),
		heard:    make(chan string, 16),
		captions: make(chan string, 64),
		done:     make(chan struct{}),
		closed:   make(chan struct{}),
	}
	m.convs = append(m.convs, c)
	m.mu.Unlock()
	turn, cancel := context.WithCancel(ctx)
	c.turn = turn
	go func() {
		<-c.closed
		cancel()
	}()
	done := c.done
	go func() {
		defer close(done)
		if m.Script != nil {
			m.Script(turn, c)
		}
	}()
	if m.Infer != nil {
		return inferringFake{c}, nil
	}
	return c, nil
}

// inferringFake is a [FakeConversation] that can make its step's report,
// which a fake opened by a maker with Infer is.
type inferringFake struct{ *FakeConversation }

// Infer keeps the ask and answers the maker's Infer.
func (f inferringFake) Infer(ctx context.Context, ask string) (factory.StageResult, error) {
	f.keepInferred(ask)
	return f.maker.Infer(ctx, f.FakeConversation, ask)
}

func (c *FakeConversation) keepInferred(ask string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inferred = append(c.inferred, ask)
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

	maker    *FakeMaker
	turn     context.Context
	id       string
	heard    chan string
	captions chan string
	done     chan struct{}
	closed   chan struct{}

	mu       sync.Mutex
	sent     []string
	noted    []string
	model    string
	isClosed bool
	// required is the tool the current turn must call, "" for none;
	// failure is what its Wait answers; last is what the model last said;
	// inferred is every ask the factory's own report was made from.
	required string
	failure  error
	last     string
	inferred []string
}

// Model is the model the fake is on: the spec's, then the last SetModel.
func (c *FakeConversation) Model() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.model == "" {
		return c.Spec.Model
	}
	return c.model
}

// SetModel switches the fake's model, as a person's pick does.
func (c *FakeConversation) SetModel(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.model = model
}

// Send records the words and hands them to the script through Heard while
// its turn runs, or starts the maker's FollowUp turn on them when it has ended.
func (c *FakeConversation) Send(_ context.Context, words string) error {
	return c.deliver(words, false, "")
}

// Note is Send for codeaf's own words, kept apart in Noted.
func (c *FakeConversation) Note(_ context.Context, words string) error {
	return c.deliver(words, true, "")
}

// NoteRequiring is Note for a turn that must call tool first ([requirer]); the
// FollowUp turn reads it with Required, as a model reads `tool_choice`.
func (c *FakeConversation) NoteRequiring(_ context.Context, words, tool string) error {
	return c.deliver(words, true, tool)
}

func (c *FakeConversation) deliver(words string, note bool, required string) error {
	c.mu.Lock()
	if note {
		c.noted = append(c.noted, words)
	} else {
		c.sent = append(c.sent, words)
	}
	idle := false
	select {
	case <-c.done:
		idle = true
	default:
	}
	if !idle {
		c.mu.Unlock()
		select {
		case c.heard <- words:
		default:
		}
		return nil
	}
	done := make(chan struct{})
	c.done, c.required, c.failure = done, required, nil
	c.mu.Unlock()
	go func() {
		defer close(done)
		if f := c.maker.FollowUp; f != nil {
			f(c.turn, c, words)
		}
	}()
	return nil
}

// Wait returns when the current turn does, with what Fail said of it, or
// when ctx ends.
func (c *FakeConversation) Wait(ctx context.Context) error {
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	select {
	case <-done:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.failure
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Fail ends the script's current turn with err, as a provider's refusal or a
// spending limit ends a real one; the next turn starts clean.
func (c *FakeConversation) Fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failure = err
}

// Required is the tool the current turn must call, "" for none.
func (c *FakeConversation) Required() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.required
}

// Answer is the script saying words, which LastWords then answers.
func (c *FakeConversation) Answer(words string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = words
}

// LastWords is what the script last said with Answer.
func (c *FakeConversation) LastWords() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// Inferred is every ask the factory's own report was made from, in order.
func (c *FakeConversation) Inferred() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.inferred...)
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

// Noted is every note codeaf itself sent, in order.
func (c *FakeConversation) Noted() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.noted...)
}

// Heard is the script's side of Send.
func (c *FakeConversation) Heard() <-chan string { return c.heard }

// Say puts one caption on the conversation's running account.
func (c *FakeConversation) Say(line string) { c.captions <- line }

// fakeID is a fake conversation's id: the one it reopens, else a new number.
func fakeID(spec ConversationSpec, n int) string {
	if spec.Resume != "" {
		return spec.Resume
	}
	return fmt.Sprintf("fake-%d", n)
}
