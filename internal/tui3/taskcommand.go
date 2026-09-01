package tui3

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// taskCommandAgent is the whole of what /task needs from the session, and it is
// two calls now that ordinary work has ONE ROAD. The planner door it used to
// carry is gone from the engine too, along with the `/task adaptive` that was its
// only caller; the planner ENGINE is untouched and a person can still name a run
// outright in the conversation, which the session reads for itself. Either way
// the verb is not in this surface's vocabulary — a door listed here that no
// command opens is an invitation to open it again.
type taskCommandAgent interface {
	StartTask(context.Context, string) (uint64, string, error)
	JudgeDecomposable(context.Context, string) (bool, []string, string)
}

// taskSizedMsg is the sizing call's answer on its way back to the surface: the
// brief that was read, and whether there was more than one job in it.
//
// IT CARRIES NEITHER A SKETCH NOR A ROW ANY MORE. The parts the judge named and
// its reason for naming them were supporting context for a planner, and the
// person's `starting a task` row decided which road the answer opened — and
// there is one road, so both are questions with no reader. What survives of the
// sketch is what it was always standing in for: a yes ARMS the worker to divide
// off the material itself (internal/session's task_divide.go), from evidence
// rather than from a guess made before anybody opened the files.
type taskSizedMsg struct {
	brief    string
	parallel bool
	// wait names the forming block this answer settles. Two `/task` commands can
	// be in flight at once — the block is a list now (formingblock.go) — and a
	// settle that took whichever block was newest would collapse somebody else's
	// wait and leave this one turning forever.
	wait uint64
}

type taskStartedMsg struct {
	kind, id, title string
	err             error
	// wait is [taskSizedMsg.wait], for the same reason and on the same terms.
	wait uint64
}

func (a *app) runTaskCommand(arg string) tea.Cmd {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		// A BARE /task IS THE ROSTER AND NOT A USAGE LINE. The margin's `+ /task`
		// row types this command into the draft (margin.go), so the word arrives in
		// the box in front of somebody who has not said what the work is yet — and
		// a person who sends it as it stands is asking the only question the command
		// can answer with no brief behind it: what work is there. That is the page
		// /history opens ([app.openTaskPage]), and the two forms of the one command
		// are then the pair of errands a person has about tasks — start one, or go
		// and look at the ones that already ran.
		return a.openTaskPage()
	}
	door, ok := a.agent.(taskCommandAgent)
	if !ok {
		a.note("could not start the task · this session has no task door")
		return nil
	}
	// THE WHOLE VOCABULARY IS TWO FORMS: a brief, or `solo` and a brief. A first
	// word that is neither of those is simply the beginning of the brief, so the
	// split is taken once here and the brief defaults to everything typed.
	word, rest, _ := strings.Cut(arg, " ")
	rest = strings.TrimSpace(rest)
	brief, solo := arg, false
	switch word {
	case "solo":
		brief, solo = rest, true
	case "adaptive":
		// THE RETIRED WORD IS A WORD NOW AND NOT A ROAD. `/task adaptive` used to
		// open a planned graph over the brief, and ordinary task work does not go
		// that way any more. There are two things this surface must not do about
		// that. It must not GUESS, because guessing means editing somebody's
		// sentence — a brief that genuinely opens "adaptive rate limiting for the
		// api" would lose its first word to a shape nobody asked for — so the words
		// are kept whole and run the one road there is. And it must not stay QUIET,
		// because the person who did mean the old shape would then get something
		// other than what they typed with nothing on screen saying so, which is the
		// one outcome a retirement owes a line about. So: the brief is untouched,
		// and one line says the word steers nothing.
		a.note(taskAdaptiveRetiredNote)
		if rest == "" {
			// Nothing but the retired word is no brief at all, and shaping a task
			// called "adaptive" would spend a model call on somebody's muscle memory.
			brief = ""
		}
	}
	if brief == "" {
		a.note("usage: /task <brief> · /task solo <brief>")
		return nil
	}
	// AN EXPLICIT SOLO IS THE LAST WORD. `/task solo` says outright that this is
	// one worker and nothing is to be read for width first, so neither the sizing
	// call nor the person's standing answer to it has anything left to decide.
	if solo {
		return a.startTaskDoor(door, brief, 0)
	}
	// And where they have said in advance that one worker is what they want and
	// that they do not want the brief read for width first, the sizing call is
	// not made. It is a small call, but it is a call, and its whole remaining
	// product is a road this person has said they would rather not pay to open:
	// the work can still divide off what its own brief already enumerates
	// (internal/splitgate), which costs nothing at all.
	preset := config.TaskStartAt(a.profileDir)
	if preset == config.TaskStartSingle {
		return a.startTaskDoor(door, brief, 0)
	}
	seq := a.beginPreflight(0, taskSizingNote, brief)
	ctx := a.ctx
	return func() tea.Msg {
		parallel, _, _ := door.JudgeDecomposable(ctx, brief)
		return taskSizedMsg{brief: brief, parallel: parallel, wait: seq}
	}
}

// startTaskDoor hands the work through, and says so while it goes.
//
// THE WAIT IS NAMED BECAUSE IT IS NOT INSTANT ANY MORE. The door shapes the
// brief before it admits anything (internal/session's task_shape.go), which is
// a model call of its own, and a command that appeared to do nothing for several
// seconds would read as a command that had not registered. The forming block
// says the one true thing about the pause in the same voice `sizing it up…` says
// its own, and [app.settleShaping] collapses it the moment the task lands —
// including when nothing shaped it, because the block was about the attempt.
// seq is the wait the sizing phase already opened for this command, and zero
// where the command opened none — the phase changes IN PLACE on the block a
// person is already reading, rather than collapsing one block and raising
// another under it ([app.beginPreflight]).
func (a *app) startTaskDoor(door taskCommandAgent, brief string, seq uint64) tea.Cmd {
	ctx := a.ctx
	// WHO ELSE IS ALREADY IN THESE FILES, SAID BEFORE THE SPEND. `/task` shows no
	// proposal card — the person typed the brief, so there is nothing to consent
	// to — which means this note is the only place the fact can reach them, and
	// this is the last line before the door is opened and the shaping call is
	// paid for. It is a note and NOT a gate: the very next statement hands the
	// work over regardless, because a claim another window wrote is evidence and
	// never an instruction (internal/session's taskpreflight.go).
	//
	// The local reading is cached, so this costs a readdir at most once every
	// three seconds (taskview.go's [app.elsewhere]). A HOSTED SURFACE SAYS
	// NOTHING: this seam cannot ask the far roster, and consulting the laptop
	// would describe another machine's work.
	if !a.hosted() {
		if line := session.PreflightNote(a.workspace, a.elsewhere(), brief); line != "" {
			a.note(line)
		}
	}
	// AND WHAT THIS PERSON'S OWN CHECKOUT IS ABOUT TO SEND. The task works in a
	// copy of the folder AS IT STANDS (internal/session's groundladder.go), so
	// half-finished edits go with the work — which is what almost everybody
	// wants and is worth one line for the person who was in the middle of
	// something experimental. Said in the same breath as the line above and for
	// the same reason: this is the last moment before the spend when knowing it
	// can still change what somebody does.
	//
	// It costs one `git status` per start and nothing at all per frame, and it is
	// silent on a clean tree (internal/session's taskpreflight.go).
	if line := session.UnsavedEditsNote(a.workspace); line != "" {
		a.note(line)
	}
	seq = a.beginPreflight(seq, taskShapingNote, brief)
	// AND THE BRIEF IS WATCHED WHILE IT IS WRITTEN. The shaper streams its answer
	// to whoever asked for it (internal/session's [session.WithBriefWatch]), and
	// what arrives here is the raw accumulated text on the goroutine making the
	// call — so this hand does one thing with it, which is put the newest version
	// on a lane the surface owns. Everything about reading it happens on the
	// update lane, where the app's own state lives.
	//
	// The lane holds ONE text and it is the NEWEST: a preview is a tail, and a
	// backlog of superseded tails is a queue of things nobody will ever want to
	// look at. A frame that misses a fragment misses nothing, because the next
	// fragment carries the whole answer again.
	stream := make(chan shapingRead, 1)
	return tea.Batch(a.pumpShaping(seq, stream), func() tea.Msg {
		defer close(stream)
		watched := session.WithBriefWatch(ctx, func(answer, thinking string) {
			read := shapingRead{answer: answer, thinking: thinking}
			select {
			case stream <- read:
			default:
				select {
				case <-stream:
				default:
				}
				select {
				case stream <- read:
				default:
				}
			}
		})
		id, title, err := door.StartTask(watched, brief)
		return taskStartedMsg{"single", strconv.FormatUint(id, 10), title, err, seq}
	})
}

// shapingRead is one reading of what the shaper has produced, and it is two
// strings because on a thinking model one of them is empty for the whole wait
// (internal/session's [session.BriefWatch]). They are kept apart the whole way
// so that nothing can draw the model's working and call it somebody's brief.
type shapingRead struct {
	answer   string
	thinking string
}

// shapingTailMsg carries one reading off the lane [app.startTaskDoor] opened,
// and carries the lane back with it so the pump can ask for the next one. done
// says the shaping call has returned and nothing more is coming.
type shapingTailMsg struct {
	wait   uint64
	read   shapingRead
	done   bool
	stream <-chan shapingRead
}

// pumpShaping waits for the next reading and hands it to the update lane. It is
// one message per fragment the surface actually gets to draw rather than one per
// token: the lane holds only the newest text, so a shaper writing faster than
// the frame clock collapses into whatever was there when the pump came round.
func (a *app) pumpShaping(seq uint64, stream <-chan shapingRead) tea.Cmd {
	return func() tea.Msg {
		read, open := <-stream
		return shapingTailMsg{wait: seq, read: read, done: !open, stream: stream}
	}
}

// The two phase words are named rather than typed at their two ends because the
// state that begins a phase and the state that settles it must agree. A spelling
// drift there would leave the wrong live phase on screen or fail to clear it.
const (
	taskSizingNote  = "sizing it up…"
	taskShapingNote = "shaping the brief…"
)

// taskWideNote is what the sizing call's yes says now that it opens nothing.
//
// IT IS A FACT AND NOT A WAIT, so it is written once and never taken back: a
// question was asked about this brief before the work started, the answer was
// that there is more than one job in it, and that answer is what lets the worker
// hand the parts out later (internal/session's task_divide.go). The person is
// told because the roster is about to grow rows nobody typed a command for, and
// a task that quietly becomes four tasks is a surface doing something unannounced.
//
// It says only what is true at the moment it is written. The worker still has to
// open the material, find the width is real and find a free hand before anything
// is handed out, so the line promises a possibility — "can split" — rather than
// a plan, and the transcript's own words for the split (`split into 3 parts:`)
// are what say it happened.
const taskWideNote = "the work looks wide · one worker starts, and it can split as it goes"

// taskAdaptiveRetiredNote answers `/task adaptive`, and says both halves of what
// happened: the word no longer picks anything, and it was left exactly where the
// person typed it. It names the road that replaced it in the same words
// [taskWideNote] uses, because they are the same road and a person who reads
// both should not have to work out that they are.
const taskAdaptiveRetiredNote = "/task adaptive retired · the word stays in your brief, and the work starts as one worker that can split as it goes"

// preflight is the one visible thing a task command is becoming: the person's
// words, its present phase, the moment that phase began, and — while the shaper
// is writing — the brief as far as it has got.
//
// THE WAIT DOES NOT ENTER THE NOTES LANE. Notes report facts that have landed;
// this scaffold exists only while a command is in flight and is drawn at the
// transcript tail as a live region. Its left hairline gives every row one owner,
// while the shared spinner and count-up say that owner is still changing. When
// the door answers, the state is cleared before the ordinary settled task or
// error row is written, so collapse is one replacement frame rather than a
// second announcement.
type preflight struct {
	note  string
	brief string
	// name is the block's identity line where there are no person-words to
	// quote: a PROPOSAL the person just approved. It is the card's own name, so
	// the block and the card that raised it can never call the work two things.
	name string
	// taskID owns the wait on the proposal road, and zero is the typed command's
	// wait. The two roads settle differently — a command's door answers with a
	// message, a proposal's task simply starts existing on the update lane — and
	// the id is how the second settle finds its own block and no other.
	taskID uint64
	// seq is the wait's own identity, handed out by [app.beginPreflight] and
	// carried back on the message that settles it. The taskID above cannot do
	// this job: a typed command has no task to name until the door has answered,
	// which is the exact moment the wait is over.
	seq uint64
	at  time.Time
	// tail is the brief as far as the shaper has written it — the DECODED value
	// of the one field the answer is previewed by, never the raw JSON around it
	// ([shapingPreview]). It is empty on the proposal road, where the brief was
	// written before the person was ever asked, and on every road until the first
	// fragment lands; a block with nothing to show draws no tail at all.
	tail string
	// think is what the shaper is REASONING while it has not started writing —
	// the model's working, kept apart from its answer all the way down
	// (internal/session's [session.BriefWatch]) and drawn in the italic this
	// surface already draws a think in (thinking.go).
	//
	// IT IS HERE BECAUSE ON A THINKING SHAPER IT IS THE ONLY THING THERE IS. The
	// shaper is allowed to reason, and a run measured against a real endpoint
	// spent the whole twenty-five seconds producing reasoning and never one
	// answer delta — so a block that could only draw the brief drew nothing for
	// exactly the wait it was built for. The brief takes the row the moment there
	// is a brief, and never gives it back.
	think string
	// open is the window somebody asked for: the last few lines of the brief
	// rather than the newest one. It is per-wait, because in a block of several
	// only the pointed one shows anything at all.
	open bool
}

// live reports whether a wait is up. It is the frame's eighth reason to paint.
func (p preflight) live() bool { return p.note != "" }

// waiting reports whether ANY forming block is up. It is the predicate the paint
// clock and the frame ask, and it is a question about the list rather than about
// one wait: a second `/task` typed while the first is still shaping must not let
// the surface go still when the first one lands.
func (a *app) waiting() bool { return len(a.waits) > 0 }

// beginPreflight puts a wait on screen and starts its clock, and answers with
// the identity the settle will come back with.
//
// A seq that names a wait already on screen changes that wait's PHASE IN PLACE.
// Sizing and shaping are two phases of one command, and a person watching must
// see the words on the third row change rather than a block collapse and a
// second one open under it.
func (a *app) beginPreflight(seq uint64, note, brief string) uint64 {
	if at := a.waitAtSeq(seq); at >= 0 {
		a.waits[at].note = note
		a.waits[at].at = a.now()
		// The phase is the wait's subject, so the preview it collected under the
		// old phase is not this phase's. Nothing has been written of the new one
		// yet, which is the honest thing to draw.
		a.waits[at].tail = ""
		a.follow()
		a.touch()
		return seq
	}
	a.waitSeq++
	a.waits = append(a.waits, preflight{seq: a.waitSeq, note: note, brief: brief, at: a.now()})
	// THE NEWEST WAIT IS THE POINTED ONE. It is the one the person just asked
	// for, and in a block of several it is the only one whose preview is drawn
	// (formingblock.go) — pointing anywhere else would be the surface deciding
	// which of somebody's commands they meant.
	a.waitAt = len(a.waits) - 1
	a.follow()
	a.touch()
	return a.waitSeq
}

// beginProposalWait is the SAME forming block raised by the OTHER door: a
// proposal card the person just answered yes. The engine shapes the brief
// before the task exists (internal/session's task_shape.go), which is the same
// pause the typed command stands in — and until this existed the yes was
// followed by seconds of nothing, the exact dead air the block was built to
// end. One forming vocabulary, two lawful entrances; the card itself stays,
// because it is a spend gate and not a rendering.
func (a *app) beginProposalWait(card *taskCard) {
	name := card.name
	if name == "" {
		name = card.title
	}
	a.waitSeq++
	a.waits = append(a.waits, preflight{
		seq: a.waitSeq, note: taskShapingNote, name: name, taskID: card.id, at: a.now(),
	})
	a.waitAt = len(a.waits) - 1
	a.follow()
	a.touch()
}

// settleProposalWait collapses a proposal's forming block the moment its task
// exists at all. Any update for the id is that moment: queued and running mean
// admitted, and a failure is a fact the task's own machinery announces — the
// block was only ever about the pause before there was anything to point at.
func (a *app) settleProposalWait(id uint64) {
	if id == 0 {
		return
	}
	for i := range a.waits {
		if a.waits[i].taskID == id {
			a.dropWait(i)
			return
		}
	}
}

// endPreflight stops the clock and takes the line away. The two halves are one
// call because they are one fact — this is no longer happening — and a surface
// that dropped the note while leaving the clock running would keep asking for
// frames forever on behalf of a row nobody can see.
func (a *app) endPreflight(seq uint64, note string) {
	if at := a.waitAtSeq(seq); at >= 0 && a.waits[at].note == note {
		a.dropWait(at)
		return
	}
	a.touch()
}

func (a *app) settleSizing(seq uint64)  { a.endPreflight(seq, taskSizingNote) }
func (a *app) settleShaping(seq uint64) { a.endPreflight(seq, taskShapingNote) }

// waitAtSeq is a wait's place in the list, or -1 for one that has already
// settled — which is the ordinary answer for a message that arrives after an
// error collapsed the block, and is why every caller is written to accept it.
func (a *app) waitAtSeq(seq uint64) int {
	if seq == 0 {
		return -1
	}
	for i := range a.waits {
		if a.waits[i].seq == seq {
			return i
		}
	}
	return -1
}

// dropWait takes one wait off the list and keeps the pointer on something real.
//
// The pointer follows the LIST rather than the wait it was on: a block whose
// pointed row settled must not point past its own end, and the nearest honest
// place to stand is wherever the list now ends.
func (a *app) dropWait(at int) {
	if at < 0 || at >= len(a.waits) {
		return
	}
	a.waits = append(a.waits[:at], a.waits[at+1:]...)
	if a.waitAt >= len(a.waits) {
		a.waitAt = len(a.waits) - 1
	}
	if a.waitAt < 0 {
		a.waitAt = 0
	}
	a.touch()
}

// shapingTail records one reading of what the shaper has produced.
//
// IT DIGESTS THE ANSWER THROUGH THE ONE SCANNER THIS SURFACE HAS. What arrives
// is a prefix of the shaper's JSON answer, and a prefix of a JSON object is not
// a payload — [shapingPreview] is the same tolerant read of one field that a
// forming tool call's arguments go through, which is the whole reason there is
// no second parser here to drift from the first. The reasoning is plain text and
// goes through nothing at all.
//
// A READING THAT SAYS LESS THAN WHAT IS ON SCREEN IS DROPPED, on both halves.
// The field being followed opens before it has any content, so the fragments
// after the brief's opening quote legitimately answer with nothing — and a
// preview that blanked itself every time the model paused would flicker at the
// person reading it.
func (a *app) shapingTail(seq uint64, read shapingRead) {
	at := a.waitAtSeq(seq)
	if at < 0 {
		return
	}
	p := &a.waits[at]
	changed := false
	if preview := shapingPreview(read.answer); preview != "" && preview != p.tail {
		p.tail, changed = preview, true
	}
	if think := strings.TrimSpace(read.thinking); think != "" && think != p.think {
		p.think, changed = think, true
	}
	if changed {
		a.touch()
	}
}
