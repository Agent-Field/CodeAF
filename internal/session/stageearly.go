package session

// ── the second class of early start: a call held at its commit ──────────────
//
// A turn's reply can carry several calls, and the stream shows each of them
// whole the moment its arguments close (internal/provider's sse.go), which for
// the second and third proposal of a batch is many seconds before the reply
// itself ends: a groomed proposal is the longest thing a model writes, and the
// measurement that justified this (docs/design/task-start/DESIGN.md) put the
// stream still running a median of five to twelve seconds after the first
// proposal of a two-proposal reply had closed.
//
// The turn loop already starts READS early ([warmBatch.consider]), because a
// read the stream later takes back costs only the work. It cannot start a
// mutating call early, because a reply that fails is asked for again and the
// call would run twice (loop.go's [earlyTools] states the law). What it CAN
// start early is a call whose tool is cut in two where it stops being safe —
// internal/exec/bare's [bare.StagedTool] — because the half that runs early
// can be taken back, and the half that cannot waits on a [bare.Hold] until the
// reply is whole and recorded. For `propose_task` the first half is the card
// and its countdown, which is the whole of what the person was waiting through:
// the countdown now runs beside the stream instead of after it.
//
// WHICH TOOLS ARE IN THIS CLASS IS A PROPERTY OF THE TOOL, never a list of
// names. A tool built by [bare.StagedTool] answers [bare.Tool.Stages], and
// nothing else can: the field behind it is unexported, so a tool cannot join
// the class without being built in two halves.
//
// ITS ONE CALLER IS THE TURN LOOP'S EARLY START ([warmBatch.consider]), with
// the hold's lifetime kept by the same batch: released when the batch claims
// the call, withdrawn on every other road out of the attempt. loop.go belonged
// to another lane when this landed, so those lines arrived as a seam of their
// own; docs/design/task-start/DESIGN.md ("The seam in loop.go") names all five.

// stagesEarly reports whether a call to this tool may start while the reply
// carrying it is still arriving, held at its commit until the reply is whole.
// A tool the belt does not carry does not: the dispatcher's miss would only be
// said earlier, which buys nothing.
func (a *Agent) stagesEarly(name string) bool {
	for _, tool := range a.beltTools() {
		if tool.Name == name {
			return tool.Stages()
		}
	}
	return false
}
