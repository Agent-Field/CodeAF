package session

// A PIECE THAT COMES HOME AFTER ITS PARENT STOPPED READING IS STILL ITS
// PARENT'S.
//
// ── WHAT A WORKER'S READING IS, AND WHERE IT ENDS ──
//
// A node that hands work out is held open while its pieces run, and every report
// that lands re-enters its model with a turn of its own ([childRun.foldParts]).
// That reading ends the moment the last piece it is owed has been read, and the
// runner withdraws the worker from the room there and then — it must, or a line
// said into that room would be taken by somebody who will never read it (#273).
//
// THE NODE IS NOT OVER WHEN ITS READING IS. From that withdrawal the node still
// has its check, its repair round, its merge round and its landing in front of
// it, "which on a checked node is minutes away" (task_child_run.go's own words).
// A piece landing in that window found an empty seat, and the delivery fell
// through to the PERSON'S CONVERSATION — the fallback written for a parent that
// has already landed ([Agent.taskNoteReaders]). Nothing was lost from the
// person's screen, and everything was lost from the family: the piece's result
// never reached the deliverable it was cut out of, and the parent's own report
// said nothing about it. At a fan-out of twenty pieces over three levels (#874)
// that window is hit by ordinary work, not by a race: a parent stopped at its
// threshold leaves its pieces running, they are cut with it, and every one of
// their landings arrives while the parent is still being checked.
//
// ── WHAT TAKES IT INSTEAD ──
//
// [landingFold] is the parent ITSELF as a reader. It takes the piece's news into
// the parent's own report, so the landing that is already on its way carries it:
// one account of what this node's work came to, in the family it belongs to,
// reaching the person through the parent's landing rather than beside it.
//
// IT IS NOT A TURN, AND IT CANNOT BE. A turn needs a worker, the worker's
// reading is over by definition here, and starting a second one for a node whose
// check is already running would be paying for a model to read a piece into a
// tree the checker is holding still. So what the fold can promise is exactly
// what it promises: the result is in the parent's report, and it is in the
// landing note the parent's own reader gets.
//
// AND THE CHECK DOES NOT SEE IT, WHICH IS SAID OUT LOUD RATHER THAN HIDDEN. The
// checker is handed THE WORKER'S OWN LAST WORDS about the work
// ([checkerConclusion], task_audit.go) — an account written before this piece
// came home — and this fold does not rewrite that account. The alternative is a
// second audit of the same tree, which is the person paying twice for one
// question, and the piece's own check already answered for the piece. So the
// promise here is exactly: the result is in the parent's report and in the
// landing note the parent's reader gets. The manual says the same sentence.
//
// ONLY A SETTLED PARENT SENDS ITS PIECES TO THE PERSON. That is the one case
// where the fallback is the honest answer and it is unchanged: the node has
// landed, its report is told, and news with nowhere to go belongs in front of
// somebody rather than nowhere.

import "strings"

// lateFoldLead is the one sentence the fold writes of its own, and it says the
// only thing the report cannot say for itself: this arrived after the work above
// it was over. The words are the ones a person already reads about pieces —
// nothing here is the machinery's own vocabulary.
const lateFoldLead = "A piece this task handed out came home after its own work was over:"

// landingFold is the parent node as a reader of its own pieces' news, for the
// window between its worker's last reading and its landing.
//
// IT IS A MAILBOX AND NOT A BRANCH IN THE DELIVERY, because "who reads this" is
// one ordered question with one answer ([deliverTo]), and a road that asked it
// twice — once through the seat, once through an `if` somewhere else — is two
// policies free to disagree about the same piece.
type landingFold struct {
	at   conversationID
	node *TaskNode
}

func (f landingFold) address() conversationID { return f.at }

// accept folds the piece's news into the parent's report, or refuses.
//
// THE REFUSAL IS THE SAME FACT THE SEAT REFUSES ON, read from the node's side:
// a settled parent has landed and said everything it is going to say. Nobody is
// woken, and there is no queue — the report IS the delivery — so the receipt
// carries no reader and [Agent.postTaskMessage] makes the mark itself.
func (f landingFold) accept(message delivery) deliveryReceipt {
	if !f.node.foldLatePart(message.note.text()) {
		return deliveryReceipt{to: f.at, state: deliveryNobody}
	}
	return deliveryReceipt{to: f.at, state: deliveryAccepted}
}

// foldLatePart takes one piece's news into this node's report and answers
// whether it was taken.
//
// THE LOCK IS THE JOIN POINT. The landing writes its own account under the same
// hold ([TaskNode.finish]), so a piece arriving beside a landing is either in
// the report the landing composes or is refused because the node has settled —
// never appended to a string nobody will read again.
func (n *TaskNode) foldLatePart(news string) bool {
	news = strings.TrimSpace(news)
	if n == nil || n.graph == nil || news == "" {
		return false
	}
	n.graph.mu.Lock()
	if n.state.settled() {
		n.graph.mu.Unlock()
		return false
	}
	// A NODE RESTORED FROM A CHECKPOINT HAS ITS REPORT AND NOT THE HALVES IT WAS
	// MADE OF: the record keeps what a person reads (task_store.go) and nothing
	// else, which is right — the halves are this file's bookkeeping and not a
	// fact about the work. Adopting what is there as the landing's own half is
	// what makes a fold after a restart an addition rather than a replacement.
	if n.landed == "" {
		n.landed = n.report
	}
	n.late = append(n.late, news)
	n.composeReportLocked()
	n.graph.mu.Unlock()
	// AND THE DISK IS TOLD, outside the lock, exactly as a landing tells it
	// ([TaskNode.finish]): a piece folded into a report nobody wrote down is a
	// piece lost to the next life of the session.
	n.graph.checkpoint()
	return true
}

// composeReportLocked is THE ONE PLACE a node's report is put together out of
// its two halves: what its landing wrote, and every piece that came home after
// its worker stopped reading. Both writers call it under the graph's lock, so
// the field always holds the whole of what this node has to say and neither half
// can overwrite the other.
//
// A NODE THAT NEVER RAN A WORKER HAS NEITHER HALF. The two roads that write a
// report for one — a task stopped while it was still queued, and one blocked by
// its dependency (both in [TaskGraph.admit]'s neighbourhood) — set the field
// directly, and they are honest: a node that was never started handed nothing
// out, so there is no piece of it to come home late.
func (n *TaskNode) composeReportLocked() {
	n.report = withReport(n.landed, n.lateBlockLocked())
}

// lateBlockLocked is every folded piece under the one sentence that says what
// they are. Nothing folded draws nothing at all — the emptiness law, applied to
// a report.
func (n *TaskNode) lateBlockLocked() string {
	if len(n.late) == 0 {
		return ""
	}
	return withReport(lateFoldLead, strings.Join(n.late, "\n\n"))
}
