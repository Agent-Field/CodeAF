package session

// A READING BESIDE THE WORK: WHAT A NODE ASKS A MODEL ABOUT ITS OWN WORK WHILE
// ITS WORKER IS ALREADY AT IT.
//
// ── WHAT WAS TRUE ──
//
// A task node made two model calls of its own before its worker's first request,
// and both stood IN FRONT of the work: the memory router, as an argument to the
// worker's constructor (six seconds on the measured node), and the division
// reading, as a call the worker was built and then left waiting on (two hundred
// and nineteen seconds on the same node, a stream that produced tokens the whole
// time and was therefore never cut). The person watched a card with a clock on it
// and "nothing on this page yet" for four minutes and eighteen seconds, and the
// worker's first request went out after all of it.
//
// Neither call's answer was ever needed BEFORE the first request. Memory is an
// aid the next request can carry as well as this one; a division's parts can be
// handed out of work that is already under way, which is what a worker calling
// `divide_work` mid-run has always done.
//
// ── WHAT IS TRUE NOW ──
//
// Both calls are a [besideWork]: started next to the work, bound to the node's
// own context, and joined before the node leaves the code that started them. What
// each one's answer does when it lands is the caller's, because the two answers
// reach the worker through two different doors — the memory block through the
// note the next request is assembled with (memory.go's [nodeMemory]), the division
// through the steering queue under the lock the runner's tail reads its news
// under (task_divide_sketch.go's [sizingBeside]). What they share is the part
// that is easy to get wrong three times: who owns the goroutine, what cancels it,
// and where it is waited for.
//
// THE LAW: NO READING OUTLIVES THE NODE THAT STARTED IT. A reading is cancelled
// and waited for on every road out of its owner, so nothing it does can land on a
// node that has already moved on — a memory block on a worker that is closed, a
// part admitted under a node that has landed.
//
// AND A READING NEVER DECIDES WHETHER THE WORK MAY START. It may improve what the
// work is given (memory), or change what the work is (a division, a finding that
// only a person can do it), but the worker's first request never waits for it.
// Whatever it has to say arrives at the next moment the worker can hear it, and
// is dropped when there is nobody left to hear it.

import "context"

// besideWork is one reading running beside a node's work.
//
// It is a value and not a bare goroutine so that the join has a name: [besideWork.end]
// is the one place a reading is waited for, and every owner calls it on every road
// out, which is what makes the law above something a reader can check rather than
// something each call site has to remember.
type besideWork struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// beside starts one reading beside the work. The reading is handed its own
// context, cut off the owner's, so that [besideWork.end] can stop it without
// touching the work it runs beside.
func beside(ctx context.Context, read func(context.Context)) *besideWork {
	ctx, cancel := context.WithCancel(ctx)
	work := &besideWork{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(work.done)
		read(ctx)
	}()
	return work
}

// end cancels the reading and waits for it to return. It is safe to call on a
// nil reading and more than once, because an owner with several roads out calls
// it from each of them.
//
// THE WAIT IS SHORT BY CONSTRUCTION. A reading's model call honours its context
// and a cancelled one returns at once; what a reading does AFTER its answer — the
// admission of a division's parts — is a bounded piece of local work that is
// allowed to finish rather than be torn in half.
func (w *besideWork) end() {
	if w == nil {
		return
	}
	w.cancel()
	<-w.done
}
