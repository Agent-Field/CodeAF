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
// THE WORDS ARE THE SENDER'S. The fold writes no sentence of its own: it keeps
// the message AS THE SENDER SAID A RECORD SHOULD KEEP IT ([delivery.record]) — a
// piece's landing as its own head line and report ([landingRecord]), a settled
// node's re-addressed decisions as the sentence that already says what they are
// ([readdressedLead]) — so a fold can never put one message under a heading
// that is true only of another, and never copies a model's instructions into
// something a person reads. Which road the news came by is not this reader's
// question.
//
// AND THE FOLD IS THE ACKNOWLEDGEMENT. A delivery is not written down as
// announced until the recipient's own record holds it ([durableDelivery]); for
// a fold the recipient's record IS the parent's report, written to the
// checkpoint by [TaskNode.foldLatePart] before the sender settles, so the sender
// settles at once ([Agent.postTaskMessage]). A piece folded and then restarted
// over is therefore announced on the checkpoint, and is not told a second time.
//
// ONLY A SETTLED PARENT SENDS ITS PIECES TO THE PERSON. That is the one case
// where the fallback is the honest answer and it is unchanged: the node has
// landed, its report is told, and news with nowhere to go belongs in front of
// somebody rather than nowhere.

import "strings"

// landingFold is the parent node as a reader of its own pieces' news, for the
// window between its worker's last reading and its landing.
//
// IT IS A MAILBOX AND NOT A BRANCH IN THE DELIVERY, because "who reads this" is
// one ordered question with one answer ([deliverTo]), and a road that asked it
// twice — once through the seat, once through an `if` somewhere else — is two
// policies free to disagree about the same piece. And it is a PURE mailbox: it
// takes the message as the sender wrote it and says nothing of its own.
type landingFold struct {
	at   conversationID
	node *TaskNode
}

func (f landingFold) address() conversationID { return f.at }

// accept folds the message into the parent's report, or refuses.
//
// THE REFUSAL IS THE SAME FACT THE SEAT REFUSES ON, read from the node's side:
// a settled parent has landed and said everything it is going to say. Nobody is
// woken, and there is no queue — the report IS the delivery — so the receipt
// carries no reader and [Agent.postTaskMessage] makes the mark and settles the
// delivery itself.
func (f landingFold) accept(message delivery) deliveryReceipt {
	if !f.node.foldLatePart(message.recorded()) {
		return deliveryReceipt{to: f.at, state: deliveryNobody}
	}
	return deliveryReceipt{to: f.at, state: deliveryAccepted}
}

// foldLatePart takes one message into this node's report and answers whether it
// was taken.
//
// THE LOCK IS THE JOIN POINT. The landing writes its own account under the same
// hold ([TaskNode.landLocked]), so a message arriving beside a landing is either
// in the report the landing composes or is refused because the node has settled
// — never appended to a string nobody will read again.
//
// AND THE CHECKPOINT IS WRITTEN BEFORE THIS ANSWERS, outside the lock, exactly
// as a landing writes it ([TaskNode.finish]). It is the recipient's record the
// sender's acknowledgement rests on: the sender settles only after an accepted
// receipt, and by then the report that holds its message is on disk.
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
	n.late = append(n.late, news)
	n.composeReportLocked()
	n.graph.mu.Unlock()
	n.graph.checkpoint()
	return true
}

// landLocked is how every road writes the node's OWN account of itself — a
// landing ([TaskNode.finish]), a node stopped while it was still queued, a node
// its dependency blocked — and it is the reason the report has one author. The
// caller holds the graph's lock.
func (n *TaskNode) landLocked(report string) {
	n.landed = strings.TrimSpace(report)
	n.composeReportLocked()
}

// composeReportLocked is THE ONE PLACE a node's report is put together out of
// its two halves: what its landing wrote, and every message that came home after
// its worker stopped reading. Both writers call it under the graph's lock, so
// the field always holds the whole of what this node has to say and neither
// half can overwrite the other.
func (n *TaskNode) composeReportLocked() {
	n.report = withReport(n.landed, strings.Join(n.late, "\n\n"))
}

// foldedLandedLocked is the landing's own half AS THE RECORD KEEPS IT, which is
// only when there is a folded half beside it. Everywhere else the report is the
// landing's account entire, so writing it twice would be the same words stored
// under two names for nothing ([taskRecord.Landed]).
func (n *TaskNode) foldedLandedLocked() string {
	if len(n.late) == 0 {
		return ""
	}
	return n.landed
}

// landedHalf is the landing's own account read back off a record: the half the
// record kept beside a folded one, or the whole report when nothing was folded —
// which is every record written before this field existed.
func (r taskRecord) landedHalf() string {
	if len(r.Late) == 0 {
		return r.Report
	}
	return r.Landed
}
