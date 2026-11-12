package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/effort"
)

// ── the rung survives the process ───────────────────────────────────────────
//
// The defect this whole scope exists to answer is the one the conversation
// model had before chatmodel.go: a person dials a rung, works in it, closes the
// terminal, and comes back to an install-default session as though they had
// chosen nothing. A live field is not a setting.

func TestTheConversationRungIsWrittenDownAndReadBack(t *testing.T) {
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
	})

	if got := agent.ConversationEffort(); got != "" {
		t.Fatalf("a fresh conversation reports %q, want nothing at all", got)
	}
	if !agent.SetConversationEffort("xhigh") {
		t.Fatal("xhigh was refused; it is a rung on the ladder")
	}
	meta, err := LoadMeta(dir)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Effort != "xhigh" {
		t.Fatalf("meta.json says %q, want xhigh — the dial did not reach the folder", meta.Effort)
	}

	if agent.SetConversationEffort("deepest") {
		t.Fatal("a word that is not a rung was accepted")
	}
	if got := agent.ConversationEffort(); got != "xhigh" {
		t.Fatalf("a refused word moved the rung to %q", got)
	}

	// AND THE WAY BACK IS THE WHOLE POINT. A second session opened on the same
	// folder reads the rung the first one left, which is what the door does at
	// launch (cmd/codeaf's v3SavedEffort).
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	second, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
	})
	second.SetConversationEffort(meta.Effort)
	if got := second.ConversationEffort(); got != "xhigh" {
		t.Fatalf("the reopened conversation reports %q, want the xhigh it was left at", got)
	}

	// TURNING IT BACK OFF IS ALSO A CHOICE AND IS ALSO WRITTEN DOWN. A rung that
	// only ever went up would leave a conversation somebody deliberately quieted
	// coming back loud.
	if !second.SetConversationEffort("off") {
		t.Fatal("off was refused; it is how a person clears the rung")
	}
	if meta, _ = LoadMeta(dir); meta.Effort != "" {
		t.Fatalf("after off meta.json says %q, want nothing", meta.Effort)
	}
}

// A session with no folder keeps the rung in memory and writes nothing, which
// is what a headless --once and every test agent are: the dial still works,
// there is simply nowhere for it to be remembered.
func TestAConversationWithNoFolderStillTakesARungAndWritesNothing(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if !agent.SetConversationEffort("max") {
		t.Fatal("max was refused")
	}
	if got := agent.ConversationEffort(); got != "max" {
		t.Fatalf("ConversationEffort() = %q, want max", got)
	}
}

// The rung set on one piece of work rides the checkpoint, so a task that
// outlives the process comes back at the depth it was set to rather than at the
// conversation's.
func TestATasksRungRidesItsCheckpoint(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	checkpoint := taskCheckpointPath(journal)
	repo := newTestRepo(t)

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = repo
		config.SessionFile = journal
		config.InTask = true
	})
	graph := agent.graph()
	graph.mu.Lock()
	graph.run = func(*TaskNode) {}
	graph.mu.Unlock()
	graph.rehydrate(taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 1,
		Nodes: []taskRecord{{
			ID: 1, Title: "Think hard about this one", Brief: "the deep part",
			Acceptance: "done", State: TaskRunning,
		}},
	}, repo, TaskSettleAsk)

	if err := agent.SetTaskEffort(1, "max"); err != nil {
		t.Fatalf("SetTaskEffort: %v", err)
	}
	if got := agent.TaskEffort(1); got != "max" {
		t.Fatalf("TaskEffort(1) = %q, want max", got)
	}
	if _, err := os.Stat(checkpoint); err != nil {
		t.Fatalf("setting a rung did not write a checkpoint: %v", err)
	}
	document, found := loadTaskCheckpoint(checkpoint)
	if !found {
		t.Fatal("the checkpoint was not found")
	}
	if len(document.Nodes) != 1 || document.Nodes[0].Effort != "max" {
		t.Fatalf("the checkpoint holds %+v, want the node's rung on it", document.Nodes)
	}

	// And back off disk into a graph that never saw the setter.
	fresh := newTaskGraph()
	fresh.home = agent
	fresh.run = func(*TaskNode) {}
	fresh.rehydrate(document, repo, TaskSettleAsk)
	node := fresh.node(1)
	if node == nil {
		t.Fatal("the node did not come back")
	}
	if got := node.effortRung(); got != effort.Max {
		t.Fatalf("the restored node runs at %q, want the max it was set to", got)
	}

	// A word that is not a rung is refused at the door rather than written, and
	// a task that is over is refused in the same words every other room door
	// refuses one.
	if err := agent.SetTaskEffort(1, "deepest"); err == nil {
		t.Fatal("a word that is not a rung was accepted")
	}
	if err := agent.SetTaskEffort(99, "max"); err == nil {
		t.Fatal("a task this session does not have was accepted")
	}
}

// A checkpoint written by a build with a rung this one does not know resumes the
// work rather than failing to load it: the ladder's next rung down is a correct
// answer where an unreadable checkpoint is not.
func TestARungThisBuildDoesNotKnowResumesAsAbsence(t *testing.T) {
	for _, word := range []string{"", "deepest", "off"} {
		if got := restoredRung(word); got != effort.None {
			t.Fatalf("restoredRung(%q) = %q, want absence", word, got)
		}
	}
	if got := restoredRung("XHIGH"); got != effort.XHigh {
		t.Fatalf("restoredRung(%q) = %q, want xhigh however it was cased", "XHIGH", got)
	}
}

// ── one word for absence, in what a person reads ────────────────────────────
//
// ABSENCE IS CALLED `auto` AND NOTHING ELSE. The settings row calls it auto
// (internal/config's EffortWord), the model picker's walk calls it auto and the
// task control's does too, so a refusal that offered `off` was the last place
// this dial was spelled two ways. `off` is still READ — a profile written by an
// older build means what it meant — it is just not what anybody is offered.
func TestTheRefusalForAnUnknownRungCallsAbsenceAuto(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	err := agent.SetTaskEffort(1, "nonsense")
	if err == nil {
		t.Fatal("an unknown thinking level was accepted")
	}
	for _, word := range []string{"low", "medium", "high", "xhigh", "max", "auto"} {
		if !strings.Contains(err.Error(), word) {
			t.Errorf("the refusal %q does not offer %q", err, word)
		}
	}
	if strings.Contains(err.Error(), "off") {
		t.Fatalf("the refusal still calls absence off: %q", err)
	}
}

// AN EFFORT SET WHILE THE OPENING STAMP IS STILL OWED IS THE ONE KEPT (#2549).
//
// A person's message owes a deferred meta.json write that carries the model,
// effort, approval, places and trees as they stood when the message was sent.
// The app sets an effort right after a conversation opens, so that write can
// still be owed. It used to land after the effort's own write and put the old,
// empty rung back, so a restart read the conversation as unset. The opening
// stamp is held here until the effort's write is waiting on it, which is the
// whole ordering in question, with no clock in it. Before the fix there was no
// wait: the effort wrote first and the held stamp overwrote it on release.
//
// THE STAMP IS OWED DIRECTLY AND NOT THROUGH A TURN. A turn that also publishes
// the model owes [Agent.stampModel] behind it, and that write settles the user
// stamp and then writes a fresh snapshot, which hides the overwrite. The app's
// failure is the state where the opening stamp is the only write still owed.
func TestAnEffortSetWhileTheOpeningStampIsOwedIsTheOneKept(t *testing.T) {
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
	})

	held := make(chan struct{}, 1)
	release := make(chan struct{})
	settling := make(chan struct{}, 1)
	userStampHeld = func() {
		select {
		case held <- struct{}{}:
		default:
		}
		<-release
	}
	metaStampSettling = func() {
		select {
		case settling <- struct{}{}:
		default:
		}
	}
	t.Cleanup(func() { userStampHeld, metaStampSettling = nil, nil })

	agent.mu.Lock()
	agent.stampUserLocked("say done")
	agent.mu.Unlock()
	<-held

	done := make(chan bool, 1)
	go func() { done <- agent.SetConversationEffort("low") }()
	select {
	case <-settling:
		close(release)
		if !<-done {
			t.Fatal("low was refused; it is a rung on the ladder")
		}
	case took := <-done:
		close(release)
		if !took {
			t.Fatal("low was refused; it is a rung on the ladder")
		}
	}
	agent.SettleWrites()

	meta, err := LoadMeta(dir)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Effort != "low" {
		t.Fatalf("meta.json says %q after the opening stamp landed, want the low set after it was owed", meta.Effort)
	}
	if meta.LastUserAt.IsZero() {
		t.Fatal("the opening stamp never landed, so this proved nothing about its order")
	}
}
