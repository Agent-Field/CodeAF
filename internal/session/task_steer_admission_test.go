package session

// A NAMED CORRECTION IS ON THE DISK BEFORE ANYBODY IS TOLD IT ARRIVED.
//
// The identity on a send is what lets a person ask again for a crossing nobody
// answered without correcting the worker twice, and the engine's answer to that
// second ask comes off the node's RECORD. A record that lived only in memory
// made the promise for as long as the process did: an engine that died between
// taking a correction and its next transition came back having forgotten the
// name, so the retry arrived as a second correction. These are the laws that
// close that, and the failures they are written against are crashes rather than
// bugs — so every one of them reads the file on disk and none of them closes
// the session first.
//
// WHAT IS IN SCOPE IS THE ADMISSION OF A DIRECTION AND NOTHING ELSE. Whatever a
// worker then DOES with the words — a command it runs, a file it writes — is
// its own work and is not made exactly-once by any of this.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// steerStore points a running graph at a checkpoint file of its own and answers
// the path. It is the shape of a session that has one: nothing else about the
// node changes.
func steerStore(t *testing.T, node *TaskNode, path string) string {
	t.Helper()
	graph := node.graph
	graph.mu.Lock()
	graph.store = newTaskStore(path)
	graph.mu.Unlock()
	graph.checkpoint()
	return path
}

// keptDirections is the node's directions AS THE FILE HOLDS THEM, read back
// through the same door a resume takes ([restoredAssignment]).
func keptDirections(t *testing.T, path string, id uint64) taskAssignment {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the checkpoint: %v", err)
	}
	var document taskDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("the checkpoint is not a document: %v\n%s", err, raw)
	}
	for _, record := range document.Nodes {
		if record.ID == id {
			return restoredAssignment(record.Assignment)
		}
	}
	t.Fatalf("no node %d in the checkpoint:\n%s", id, raw)
	return taskAssignment{}
}

// THE FIRST LAW. The identity is on the disk by the time the receipt is back,
// and a NEW ENGINE reading that file recognises the same send — which is what a
// retry after a crash actually is. Nothing is closed and no transition is made:
// this is the state the process would have died in.
func TestARestoredCheckpointRecognisesTheSameSend(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	path := steerStore(t, agent.taskNode(id), filepath.Join(t.TempDir(), "tasks.json"))
	from := SteerSource{Scope: "window-before-crash", Seq: 1, At: time.Now()}

	first, err := agent.SteerTaskFrom(id, "make it CSV", from)
	if err != nil {
		t.Fatalf("SteerTaskFrom: %v", err)
	}
	if first.Direction == 0 {
		t.Fatalf("receipt = %+v, want the direction it was written down as", first)
	}

	// The engine died here. This is the record the next one opens.
	restored := keptDirections(t, path, id)
	direction, order := restored.already(from.spoken().id, "make it CSV")
	if order != directionAgain {
		t.Fatalf("the restored record answered %v, so the person's retry would be delivered a second time", order)
	}
	if direction != first.Direction {
		t.Fatalf("the restored record kept direction %d, want %d", direction, first.Direction)
	}

	// AND ONE NAME STILL CARRIES ONE SENTENCE ACROSS THE RESTART. Different words
	// under a name the record holds are refused rather than answered as arrived.
	if _, order := restored.already(from.spoken().id, "make it TSV"); order != directionMismatch {
		t.Fatalf("the restored record answered %v to different words under the same name", order)
	}
}

// AND THE SAME IS TRUE WHEN A WORKER IS ACTUALLY READING. The held path above
// keeps the words on the record; this one hands them to somebody, and the write
// happens before the hand-off rather than after it.
func TestADeliveredCorrectionIsWrittenDownBeforeItIsHandedOver(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	node := agent.taskNode(id)
	path := steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))

	worker, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM",
		InTask: true, tasker: node.graph, taskID: id, taskDepth: 1,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent for the worker: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	node.openRoom().speaking(worker)

	from := SteerSource{Scope: "window-1", Seq: 2, At: time.Now()}
	receipt, err := agent.SteerTaskFrom(id, "use the staging bucket", from)
	if err != nil {
		t.Fatalf("SteerTaskFrom: %v", err)
	}
	if receipt.Held {
		t.Fatalf("receipt = %+v, want a delivery to the worker standing in the room", receipt)
	}
	kept := keptDirections(t, path, id)
	if _, order := kept.already(from.spoken().id, "use the staging bucket"); order != directionAgain {
		t.Fatalf("a correction a worker has already read is not on the disk (%v)", order)
	}
}

// A WRITE THAT FAILED DELIVERED NOTHING AND SAYS SO. The failure is arranged
// rather than waited for: the checkpoint's directory is an ordinary FILE, which
// no process can make a directory of, on any machine and whoever is running it.
func TestACorrectionThatCouldNotBeWrittenDownIsNotDelivered(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	node := agent.taskNode(id)
	blocked := filepath.Join(t.TempDir(), "in-the-way")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	graph := node.graph
	graph.mu.Lock()
	graph.store = newTaskStore(filepath.Join(blocked, "tasks.json"))
	graph.mu.Unlock()

	from := SteerSource{Scope: "window-1", Seq: 1, At: time.Now()}
	if _, err := agent.SteerTaskFrom(id, "make it CSV", from); err == nil {
		t.Fatal("a correction the engine could not write down came back as a receipt")
	} else if !strings.Contains(err.Error(), "could not be written down") {
		t.Fatalf("the refusal does not say what happened: %v", err)
	}

	// THE ROLLBACK IS THE HALF THAT MATTERS. A direction left on the record would
	// answer the next ask with "already on the record" for words nobody has.
	if said := saidTo(t, agent, id); len(said) != 0 {
		t.Fatalf("the record kept %+v for a correction that was never delivered", said)
	}
	if _, order := node.heardBefore(from.spoken(), "make it CSV"); order != directionRefused {
		t.Fatalf("a rolled-back correction is remembered as %v, so a retry would be answered as delivered", order)
	}

	// AND THE RETRY IS AN ORDINARY NEW ADMISSION once the disk works again.
	steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
	receipt, err := agent.SteerTaskFrom(id, "make it CSV", from)
	if err != nil {
		t.Fatalf("the retry after a failed write: %v", err)
	}
	if receipt.Again || receipt.Direction == 0 {
		t.Fatalf("the retry answered %+v, want a fresh admission", receipt)
	}
}

// NOBODY READS A CORRECTION WHOSE WRITE IS STILL DECIDING.
//
// This is the boundary the store's lock alone could not hold: a worker's drain,
// a landing's revalidation and every other reader of a node's record take the
// GRAPH's lock and know nothing about the store's. Admitted under one lock and
// written under another, a direction would be readable — and actionable — while
// its own write was still in flight, and a failed write would then have rolled
// back words a worker had already been given.
//
// The probe stands inside the write on purpose ([taskStore.duringWrite]) and
// asks a reader what it can see. It can only fail when the boundary leaks.
func TestNoReaderSeesADirectionWhoseWriteIsStillDeciding(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	node := agent.taskNode(id)

	// A checkpoint that cannot be written: its directory is an ordinary file.
	blocked := filepath.Join(t.TempDir(), "in-the-way")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newTaskStore(filepath.Join(blocked, "tasks.json"))
	var leaked []taskDirection
	saw := make(chan []taskDirection, 1)
	store.duringWrite = func() {
		asking := make(chan struct{})
		go func() {
			close(asking)
			saw <- node.directionsNow()
		}()
		<-asking
		// The reader is on its way to the record. It must not get there.
		select {
		case got := <-saw:
			leaked = got
		case <-time.After(50 * time.Millisecond):
		}
	}
	node.graph.mu.Lock()
	node.graph.store = store
	node.graph.mu.Unlock()

	from := SteerSource{Scope: "window-1", Seq: 1, At: time.Now()}
	if _, err := agent.SteerTaskFrom(id, "make it CSV", from); err == nil {
		t.Fatal("a correction the engine could not write down came back as a receipt")
	}
	if len(leaked) != 0 {
		t.Fatalf("a reader saw %+v while the write was still deciding, so a worker could act on a correction that was then rolled back", leaked)
	}
	// AND ONCE THE WRITE HAS FAILED THERE IS NOTHING THERE TO SEE.
	if got := <-saw; len(got) != 0 {
		t.Fatalf("after the rollback the record still holds %+v", got)
	}

	// Put a working checkpoint back so the fixture's own landing can write.
	steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
}

// A CORRECTION THE DISK WILL NOT GIVE UP IS NOT REPORTED AS REFUSED. It reached
// the record and the engine could not take it back off the checkpoint, so
// nobody can say whether a resume would find it — and a definite refusal would
// send the person's next enter as a NEW correction under a new name.
func TestADirectionTheDiskWillNotGiveUpIsUncertainRatherThanRefused(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	node := agent.taskNode(id)
	path := steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
	from := SteerSource{Scope: "window-1", Seq: 9, At: time.Now()}

	heard, err := admitDirection(node, "make it CSV", directionFromPerson, from.spoken())
	if err != nil {
		t.Fatalf("admitting the correction: %v", err)
	}
	kept := keptDirections(t, path, id)
	if _, order := kept.already(from.spoken().id, "make it CSV"); order != directionAgain {
		t.Fatalf("the admitted direction is not on the disk (%v)", order)
	}

	// The disk goes away under it, so the rollback cannot happen.
	blocked := filepath.Join(t.TempDir(), "in-the-way")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	node.graph.mu.Lock()
	node.graph.store = newTaskStore(filepath.Join(blocked, "tasks.json"))
	node.graph.mu.Unlock()

	if err := dropDirection(node, heard, from.spoken()); err == nil {
		t.Fatal("a rollback the disk refused was reported as done, so the refusal above it would be a lie")
	}
	// AND THE SENTENCE THE CALLER PUTS ON IT IS ONE EVERY SURFACE ALREADY KNOWS
	// HOW TO HOLD: keep the send under its own name and offer to ask again.
	if !errors.Is(errDirectionUncertain, ErrSendUnanswered) {
		t.Fatal("an uncertain direction does not read as a send nobody answered for, so a surface would hand the words back")
	}

	// Put a working checkpoint back so the fixture's own landing can write.
	steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
}

// TWO ASKS FOR ONE SEND ARE ONE DIRECTION, however they interleave. This is the
// window the admission lock exists for: without it the second ask reads a
// receipt whose write has not finished — or has already failed and been rolled
// back — and reports a delivery no disk holds.
func TestConcurrentAsksForOneSendAdmitItOnce(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	path := steerStore(t, agent.taskNode(id), filepath.Join(t.TempDir(), "tasks.json"))
	from := SteerSource{Scope: "window-1", Seq: 3, At: time.Now()}

	const asks = 8
	receipts := make([]SteerReceipt, asks)
	errs := make([]error, asks)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for i := range asks {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			receipts[i], errs[i] = agent.SteerTaskFrom(id, "make it CSV", from)
		}()
	}
	close(start)
	wait.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("ask %d: %v", i, err)
		}
		if receipts[i].Direction != receipts[0].Direction {
			t.Fatalf("ask %d took direction %d, want the one receipt %d", i, receipts[i].Direction, receipts[0].Direction)
		}
	}
	if said := saidTo(t, agent, id); len(said) != 1 {
		t.Fatalf("one send became %d directions: %+v", len(said), said)
	}
	kept := keptDirections(t, path, id)
	if _, order := kept.already(from.spoken().id, "make it CSV"); order != directionAgain {
		t.Fatalf("the one admitted direction is not on the disk (%v)", order)
	}
}
