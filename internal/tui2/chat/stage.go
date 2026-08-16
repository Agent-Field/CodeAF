package chat

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The skeleton card's stage line.
//
// THE DEFECT. When the head hands work over, the command row exists seconds to
// minutes before the task does — compile runs, then the planner, then the
// receipt — and the conversation draws a card skeleton over that whole interval
// saying `creating task…` and breathing. It is true, and it is the same
// sentence at second one and at minute two. A reader watching it has no way to
// tell a compile that is working from a compile that is wedged, which is the
// same complaint the live turn's activity answers one level up.
//
// The interval is NOT unnarrated in the journal. The planner reports its phases
// (internal/plan), and cmd/aforge's poster writes each one as a replaceable row
// carrying [store.MessageProgress] — `setting working standards · 2 of 3`,
// `designing the approach · 1 of 4`. It writes them through thread.Record
// (13.18's three-class law), so the row is anchored to the task-to-be and NOT
// said in the conversation: the room can read it, the thread never hears it,
// and the head's prompt window never carries one.
//
// So the skeleton goes and reads it. It knows the command it was born from, the
// node the planner anchors to is that command's own id, and the newest progress
// row under that node is the stage. One indexed read per pending skeleton per
// poll, and there are no pending skeletons at all except during the seconds this
// exists for.

// stageRead bounds the node read behind one stage line.
//
// It reads from the START of the node's trail rather than tailing it, and that
// is a deliberate simplification for the one window this is used in: a task that
// has not been minted yet has an empty trail, and a compile-and-plan pass writes
// tens of rows into it, not hundreds. A skeleton that outlived this bound would
// hold the last stage it saw — which is still better than the bare pulse, and
// stops being anybody's problem the moment the card is named.
const stageRead = 64

// readStages asks for the newest phase each pending command has reached.
//
// It rides the trip the messages already paid for, like every other optional
// read on this result, and a backend that cannot answer simply produces no
// stages — the skeleton keeps the pulse it has always had (10.2.8: an absent
// figure is absent, never invented).
func (r *pollResultMsg) readStages(backend Backend, commands []store.Command) {
	if len(commands) == 0 {
		return
	}
	graph, ok := backend.(Graph)
	if !ok {
		return
	}
	for _, command := range commands {
		// A splice can mint either spelling and the planner anchors to the one
		// the reconciler chose, so both are asked for — the same two ids the
		// skeleton itself is reconciled under ([commandJobIDs]).
		for _, node := range commandJobIDs(command.Seq) {
			messages, err := graph.NodeMessages(node, 0, stageRead)
			if err != nil {
				continue
			}
			progress, found := newestProgress(messages, command.Seq)
			if !found {
				continue
			}
			if r.stages == nil {
				r.stages = make(map[int64]store.MessageProgress, len(commands))
			}
			r.stages[command.Seq] = progress
			break
		}
	}
}

// newestProgress is the last structured progress block in a node's trail that
// belongs to this command.
//
// IT READS THE COLUMNS AND NEVER THE PROSE. 13.3.1's rule is that v2 never scans
// a body back into structure, and the producer already writes both: the body is
// the readable journal line, and the payload beside it is the phase, the count
// and the generated title. A row with no payload is a row this line has nothing
// to say about.
func newestProgress(messages []store.Message, commandSeq int64) (store.MessageProgress, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Progress == nil || message.Role != store.RoleSystem {
			continue
		}
		// The command is the key the card subscribes by. A node can carry
		// progress from a later replan under a different command, and a skeleton
		// showing that would be narrating somebody else's work.
		if commandSeq != 0 && message.CommandSeq != 0 && message.CommandSeq != commandSeq {
			continue
		}
		if strings.TrimSpace(message.Progress.Phase) == "" {
			continue
		}
		return *message.Progress, true
	}
	return store.MessageProgress{}, false
}

// applyStages puts each pending command's newest phase on the skeleton drawn for
// it, and reports whether anything moved.
//
// ONLY A PROVISIONAL CARD, which is the whole scope of this line. The moment the
// board can name the task the skeleton becomes the card
// ([messageBlock.refreshCard]), and from then on the phase is derived from the
// graph like every other live cell on it — so the stage line does not have to be
// taken down, it simply stops being written.
func (a *App) applyStages(stages map[int64]store.MessageProgress) bool {
	if len(stages) == 0 || a.transcript == nil {
		return false
	}
	moved := false
	for i := 0; i < a.transcript.Len(); i++ {
		block, ok := a.transcript.Block(i).(*messageBlock)
		if !ok || !block.provisional || block.source == nil {
			continue
		}
		progress, found := stages[block.source.CommandSeq]
		if !found {
			continue
		}
		line := stageLine(progress)
		if line == "" || block.phase == line {
			continue
		}
		// The breathe stays. What changed is that the words now say where the
		// long thing has got to, and the dot still says it is alive — §18.2's
		// one motion on this block, unchanged.
		block.phase, block.breathing = line, true
		block.version++
		block.measured = false
		moved = true
	}
	return moved
}

// stageLine is one phase as the card says it: the planner's own words, and the
// count when there is one.
//
// `reading the ask`
// `setting working standards · 2 of 3`
//
// THE GENERATED TITLE IS NOT ON THIS ROW. [store.MessageProgress] also carries
// `Latest` — the real title the planner just produced — and the card has a place
// for those already: the subtree twigs that appear under it the moment the plan
// lands ([messageBlock.partRows]). Putting one on the phase row would be the
// same fact in two places a second apart, and would make the one line the reader
// is watching jump in length with every title.
func stageLine(progress store.MessageProgress) string {
	phase := strings.TrimSpace(progress.Phase)
	if phase == "" {
		return ""
	}
	if progress.Total <= 0 {
		return phase
	}
	return phase + " " + tokens.GlyphSeparator + " " +
		itoa(progress.Done) + " of " + itoa(progress.Total)
}
