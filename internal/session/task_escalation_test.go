package session

// THE THIRD MOMENT: work handed over from INSIDE the answer that began it.
//
// Chat could already escalate before the work started — the route judge reading
// a tool-less turn (route_judge.go), and a model proposing a task on the first
// step of a turn. Neither of those covers the moment the material reveals its
// own size: several calls in, with the findings in hand. Nothing here is a new
// road. The teaching lives in prompts/system.md, the findings ride in the brief
// propose_task already takes, and the only thing the code adds is the one line a
// person reads when it happens ([taskEscalationNote]).
//
// So these tests pin the three things that could quietly stop being true: that
// the prompt still teaches the PRINCIPLE rather than a rule, that a
// findings-rich brief reaches the worker whole, and that the line is that line.

import (
	"context"
	"strings"
	"testing"
)

// ── the teaching ────────────────────────────────────────────────────────────

// THE PROMPT TEACHES A JUDGEMENT, NOT A THRESHOLD.
//
// aforge is a general harness: a research sweep, a writing project and a
// mechanical code change are the same shape of problem to this law, and the
// moment the prompt says "after N tool calls" or reaches for a worked example
// about files, it stops being true for two of the three. So this pins both
// halves — the sentences that carry the principle, and the vocabulary that would
// narrow it back down to code.
func TestTheSystemPromptTeachesTheHandoffFromInsideTheWork(t *testing.T) {
	for _, want := range []string{
		"THE QUESTION IS ASKED AGAIN WHILE YOU WORK.",
		"the moment you can NAME the scale in front of you",
		"AND WHAT YOU HAVE ALREADY LEARNED GOES WITH IT.",
		"The brief is the dowry",
	} {
		if !strings.Contains(systemPrompt, want) {
			t.Errorf("prompts/system.md does not say %q, so nothing teaches the model to hand work over mid-answer", want)
		}
	}

	teaching := passageBetween(t, systemPrompt,
		"THE QUESTION IS ASKED AGAIN WHILE YOU WORK.", "never that you looked.")

	// THE NARROWING VOCABULARY. Each of these would read as an instruction about
	// programming to a model in the middle of a literature review, and the
	// principle is meant to hold there identically.
	for _, narrow := range []string{
		"file", "code", "repo", "test", "commit", "function", "package",
	} {
		if strings.Contains(strings.ToLower(teaching), narrow) {
			t.Errorf("the teaching says %q, which narrows a general law to coding work:\n%s", narrow, teaching)
		}
	}
	// AND NO RULE WITH A NUMBER IN IT. A threshold is the shape of instruction
	// this law must never take: it invites the model to count instead of to
	// notice, and every number is wrong for some kind of work.
	if strings.ContainsAny(teaching, "0123456789") {
		t.Errorf("the teaching carries a number, so it reads as a threshold rather than a judgement:\n%s", teaching)
	}
}

// passageBetween cuts the paragraph under test out of the prompt, so an
// assertion about ITS wording cannot be satisfied or broken by the rest of a
// three-hundred-line page.
func passageBetween(t *testing.T, text, from, to string) string {
	t.Helper()
	start := strings.Index(text, from)
	if start < 0 {
		t.Fatalf("prompts/system.md has no passage starting %q", from)
	}
	end := strings.Index(text[start:], to)
	if end < 0 {
		t.Fatalf("the passage starting %q never reaches %q", from, to)
	}
	return text[start : start+end+len(to)]
}

// AND THE FIELD THE DOWRY RIDES IN SAYS SO. The principle is in the prompt; the
// instruction about where to put the findings belongs on the argument that
// carries them, because that is what the model reads at the moment it writes
// one.
func TestTheBriefArgumentAsksForWhatTheTurnAlreadyLearned(t *testing.T) {
	agent, _ := newTestAgent(t, &routedCompleter{}, nil)
	task, found := onBelt(agent, "propose_task")
	if !found {
		t.Fatal("propose_task is not on the belt")
	}
	for _, want := range []string{
		"WHERE YOU ARE ALREADY MID-WORK",
		"WHAT YOU HAVE LEARNED IS PART OF THE BRIEF",
		// The worker is now given a bounded list of the calls that ran and their
		// inputs (admission.go), and nothing of what they RETURNED — so the
		// argument asks for the findings on those grounds rather than on the older
		// "it cannot see them at all".
		"never what they returned",
		"IF THIS REPLACES A FAILED TASK",
		"inherits neither its transcript nor its report",
	} {
		if !strings.Contains(string(task.Schema), want) {
			t.Errorf("propose_task's schema never says %q, so a mid-answer proposal has no reason to write its findings down", want)
		}
	}
}

// ── the dowry reaches the worker whole ──────────────────────────────────────

// A FINDINGS-RICH BRIEF IS NOT CLIPPED ON ITS WAY TO THE WORKER.
//
// The whole point of handing over mid-answer is that the team starts warm, and a
// brief cut at some limit nobody mentioned would drop exactly the tail a model
// writes last: what it would have done next. Nothing on this road trims it
// today — this is the pin that says so, from the tool call to the document the
// node actually reads.
func TestAMidAnswerBriefReachesTheWorkerUnclipped(t *testing.T) {
	// Long enough to run past every bound this package holds prose to — the
	// verbatim ask is capped at briefAskLimit, and a brief that shared that cap
	// would lose its tail here.
	findings := strings.Repeat("what the sweep found, source by source. ", 400)
	brief := "FINDINGS-HEAD\n" + findings + "\nwhat I would have done next: FINDINGS-TAIL"
	if len(brief) <= briefAskLimit {
		t.Fatalf("the brief under test is %d bytes, which does not exceed the %d-byte ask bound it must outrun",
			len(brief), briefAskLimit)
	}

	completer := &routedCompleter{parent: []step{
		proposeCall("Sweep the sources", brief),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 1
	})
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.finish("swept", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "look into how the pricing is set across the material")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	node := ran.await(t)

	for name, text := range map[string]string{
		"the spec's own brief":          node.spec.brief,
		"the assembled brief":           node.assembledBrief(),
		"the document the worker reads": node.instruction(),
	} {
		if !strings.Contains(text, "FINDINGS-HEAD") || !strings.Contains(text, "FINDINGS-TAIL") {
			t.Errorf("%s lost an end of the findings (%d bytes of %d)", name, len(text), len(brief))
		}
		if !strings.Contains(text, findings) {
			t.Errorf("%s does not carry the findings whole", name)
		}
	}
	waitDoneNode(t, node)
}

// ── the line a person reads ─────────────────────────────────────────────────

// IT IS THAT LINE, AND IT IS ONE LINE.
//
// A person reads this on a turn they did not ask to be interrupted on, so it is
// pinned as an exact string rather than as a shape: the wording IS the feature.
// The machinery check reuses [plainWords], which is the net this package already
// holds every person-facing sentence to (task_audit.go).
func TestTheHandoffLineIsTheLineAndCarriesNoMachinery(t *testing.T) {
	const want = "this one wants more hands · handing it over with everything found so far"
	if taskEscalationNote != want {
		t.Fatalf("the handoff line reads %q, want %q", taskEscalationNote, want)
	}
	if plain := plainWords(taskEscalationNote); plain != taskEscalationNote {
		t.Errorf("the line carries machinery vocabulary; plainly it would read %q", plain)
	}
	if strings.Contains(taskEscalationNote, "\n") {
		t.Error("the line is more than one line")
	}
	// LOWERCASE AND UNPUNCTUATED AT THE END, which is the register every dim
	// note in this surface is written in: a remark, never an announcement.
	if taskEscalationNote != strings.ToLower(taskEscalationNote) {
		t.Errorf("the line is not lowercase: %q", taskEscalationNote)
	}
	if strings.HasSuffix(taskEscalationNote, ".") {
		t.Errorf("the line ends in a full stop, which makes a remark into an announcement: %q", taskEscalationNote)
	}
	if !strings.Contains(taskEscalationNote, " · ") {
		t.Errorf("the line has no middle dot, so it is not the observation-then-promise the surface already speaks in: %q", taskEscalationNote)
	}
}

// A PROPOSAL MADE FROM INSIDE THE WORK DRAWS THE LINE, AND STILL DRAWS THE CARD.
//
// The person's wish was to be told and have the work open. The card already does
// both — silence starts it — so the escalation keeps its consent window and gains
// the line above it. What must hold either way: exactly one task, admitted
// through the one door.
func TestAProposalFromInsideTheWorkIsAnnouncedAndStillOffered(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		lsCall("call-look"),
		proposeCall("Sweep the sources", "everything the look turned up"),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 1
	})
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.finish("swept", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "work out how the pricing is set across all of this")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := noticesSaying(collected, taskEscalationNote); got != 1 {
		t.Fatalf("the handoff line reached the person %d times, want once: %v", got, kinds(collected))
	}
	proposal, ok := firstOfKind(collected, EventTaskProposal)
	if !ok {
		t.Fatalf("no card was offered, so the countdown never gave the person a window to redirect: %v", kinds(collected))
	}
	if proposal.Task.Deadline.IsZero() {
		t.Error("the card carries no deadline, so nothing opens on silence")
	}
	// THE LINE COMES FIRST. It is the reason the card is there, and a card that
	// arrived above its own explanation would read as an interruption twice.
	if noticeAt(collected, taskEscalationNote) > indexOfKind(collected, EventTaskProposal) {
		t.Error("the card was drawn before the line that explains it")
	}
	node := ran.await(t)
	waitDoneNode(t, node)
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted, want exactly one", count)
	}
}

// AND A PROPOSAL MADE BEFORE ANY WORK DRAWS NOTHING. The line promises the
// findings go with the work; on the first step of a turn there are none, and a
// line that says otherwise is the surface telling a person something untrue in
// order to sound warm.
func TestAProposalMadeBeforeAnyWorkIsSilent(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		proposeCall("Sweep the sources", "the whole brief"),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 1
	})
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.finish("swept", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "work out how the pricing is set across all of this")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	if got := noticesSaying(collected, taskEscalationNote); got != 0 {
		t.Fatalf("a proposal made before any work drew the handoff line %d times, want none", got)
	}
	if _, ok := firstOfKind(collected, EventTaskProposal); !ok {
		t.Fatalf("the ordinary proposal card is gone: %v", kinds(collected))
	}
	waitDoneNode(t, ran.await(t))
}

// ── small helpers ───────────────────────────────────────────────────────────

func noticesSaying(events []Event, text string) int {
	count := 0
	for _, event := range events {
		if event.Kind == EventNotice && strings.Contains(event.Text, text) {
			count++
		}
	}
	return count
}

func noticeAt(events []Event, text string) int {
	for index, event := range events {
		if event.Kind == EventNotice && strings.Contains(event.Text, text) {
			return index
		}
	}
	return len(events)
}

func indexOfKind(events []Event, kind EventKind) int {
	for index, event := range events {
		if event.Kind == kind {
			return index
		}
	}
	return len(events)
}
