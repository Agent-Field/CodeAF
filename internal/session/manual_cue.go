package session

// WHY THE MODEL DOES NOT OPEN THE MANUAL, AND WHAT IS DONE ABOUT IT HERE.
//
// tools_manual.go exists because a fluent answer about aforge is
// indistinguishable from a remembered one, and #307 made the lookup read the
// person's own words so that a lookup which HAPPENS lands on the right page.
// Measured on the wire afterwards (#321), what was left was bigger than
// everything the two of them fixed put together: **whether the model reaches
// for the tool at all swings by up to nine questions of twenty-five between
// runs.** It answers "who can see my files" from memory, or reads it literally
// and runs `ls`. A turn that never opened the manual cannot reach a page, so
// that one bit sits in front of every retrieval number this repository keeps.
//
// THE FIX IS EVIDENCE, NOT AN INSTRUCTION. The tool description already says
// what the manual is for and prompts/system.md's tool policy already names it;
// asking harder is asking the same model the same way. What the model has no
// way to know is that AN ANSWER EXISTS — the corpus is a few dozen sections
// nothing in its training data has ever seen, so "is this written down
// somewhere" is a question it can only guess at. So the harness answers it,
// for free, before the first request goes out: the top matching section TITLES,
// with an opening line each, ride beside the person's message. The model is
// told nothing; it is shown that the page it would need is there.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - IT COSTS NOTHING ON A TURN THAT IS NOT ABOUT AFORGE. The gate is
//     [manual.Corpus.Cued] — the corpus's own derived vocabulary, already the
//     resident's self-question trigger — so there is ONE answer in this
//     repository to "is this message about the product itself" rather than two
//     that drift. An uncued turn is byte-identical to what it was before this
//     file existed.
//
//   - IT NEVER ENTERS THE FIXED PREFIX. The block rides in the user message of
//     the turn that earned it, not in prompts/system.md and not in a tool
//     schema, so the ~150 bytes of headroom
//     TestTheFixedPrefixStaysUnderItsBudget guards are untouched — and so is
//     message[0], whose cached prefix a cue in the system block would disturb
//     on every cued turn of every conversation.
//
//   - TITLES, NOT TEXT. Each line is a heading and the section's opening
//     clause, cut. Handing over the bodies would answer the question here and
//     the model would never open anything — which is the failure this file is
//     about, arrived at from the other side. What is shown is that an answer
//     exists and where it is.
//
//   - THE PERSON NEVER SEES IT, AND NEITHER DOES THE CONVERSATION. It is not
//     recorded, so the transcript, the journal, a resume, an export and the
//     person's ask ([Agent.taskRequest], which the manual's own lookup reads
//     beside the model's query) are all exactly what they were. It is attached
//     to the copy of the transcript ONE turn's requests are made from and dies
//     with that turn — a hint about a question already answered is prompt
//     nobody is buying twice.
//
//   - AND IT IS BOUNDED TWICE. [manualCueSections] sections, each line cut at
//     [manualCueLineCap], and the whole block refused past [manualCueCap]. A
//     manual page that grows is a manual page that grows; it is not a turn's
//     prompt growing with it.

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// manualCueSections is how many titles are shown, and it is
	// [manualSections] rather than a number of its own: the cue is a preview of
	// the lookup the model is deciding whether to make, and a preview of four
	// that previewed six would be showing a page the tool would not return.
	manualCueSections = manualSections
	// manualCueLineCap bounds one section's opening clause. It is a sentence's
	// worth — enough to tell "this is the page" from "this is a page that
	// mentions the word", and short enough that four of them are a glance
	// rather than a document.
	manualCueLineCap = 120
	// manualCueCap is the ceiling on the whole block. Four titles and four
	// clauses sit well under it; the cap is what keeps a future long heading
	// from turning a free hint into a paragraph nobody budgeted for. A section
	// that would cross it is left out, and a block with nothing in it is no
	// block at all.
	manualCueCap = 1000
)

const (
	// manualCueMark is the block's first line and the thing that says whose
	// words the rest are not. It stands as its own constant because it is also
	// how the block is found again — a reader of a request that wants what the
	// person actually said cuts at it.
	manualCueMark = "(not from the person — aforge's own manual, matched against the message above)"
	// manualCueOpening is what the block says about itself. It is a REPORT and
	// not a request: it states where the lines came from, that they are titles
	// rather than text, and stops. There is no verb in it for the model to be
	// obedient to, because obedience is what the tool description already asks
	// for and what the measurement says does not arrive.
	manualCueOpening = manualCueMark + "\n" +
		"The manual has sections on this. Titles and opening words only; the sections themselves are in the manual.\n"
)

// manualCueTurn is one turn's cue: the block, the sentence it was matched
// against, and where that sentence sits in the transcript. All three are needed
// because the block is attached to a COPY of the transcript at request time
// rather than written into it — so the attaching has to find the message again,
// and has to be able to tell that it is still the same message when a
// compaction has moved everything under it.
type manualCueTurn struct {
	block string
	words string
	at    int
}

// cueTurnLocked decides this turn's cue, and clears the last one whatever it
// decides. It is called from [Agent.startTurnLocked] — the one place a turn
// starts — rather than from a door, because every road a person's words take to
// a turn goes through there and a second cueing seam would be a second answer
// to what this turn is carrying.
//
// IT IS A CONVERSATION'S ONLY. Inside a task node the opening message is a
// brief and not somebody asking a question, which is the same reason
// tools_manual.go reads the node's frozen request there: nobody is sitting in a
// worktree. A woken turn and the session's own notes are nobody asking either.
func (a *Agent) cueTurnLocked(user userMessage) {
	a.cued = manualCueTurn{}
	if a.config.ManualCueOff || a.config.InTask {
		return
	}
	if user.empty() || user.wake || user.authored {
		return
	}
	// AND A MESSAGE THAT ALREADY CARRIES SOMETHING FOR THE MODEL ALONE IS LEFT
	// ALONE. `said` is set by exactly one door — a draft the person MARKED
	// STANDING (standing_mark.go) — and that sentence is an order to shape a
	// card rather than a question about the product. Cueing it would be
	// answering a question nobody asked, on top of an instruction that says not
	// to do the sentence at all.
	if user.said != "" {
		return
	}
	words := strings.TrimSpace(user.text())
	block := manualCueFor(words)
	if block == "" {
		return
	}
	a.cued = manualCueTurn{block: block, words: words, at: len(a.messages) - 1}
}

// withManualCue is the attaching itself: the turn's messages with the block
// beside the sentence that earned it, and the same messages untouched when
// there is no cue.
//
// It writes into the caller's slice, which is already this turn's own copy
// ([Agent.snapshotWithReasoning]), and it REFUSES ON ANY DOUBT — a message that
// is no longer at that index, no longer the person's, or no longer the sentence
// the block was matched against is a transcript a compaction has rewritten
// under this turn, and a hint attached to the wrong message is worse than none.
func (a *Agent) withManualCue(messages []ai.Message) []ai.Message {
	a.mu.Lock()
	cued := a.cued
	a.mu.Unlock()
	if cued.block == "" || cued.at < 0 || cued.at >= len(messages) {
		return messages
	}
	carried := messages[cued.at]
	if carried.Role != "user" || messageContentText(carried) != cued.words {
		return messages
	}
	// The block joins the LAST TEXT PART rather than becoming a part of its own,
	// so the message keeps the shape it had: one text part in, one text part
	// out, and a reader that expected the person's words at Content[0] still
	// finds them there.
	content := append([]ai.ContentPart(nil), carried.Content...)
	last := len(content) - 1
	if content[last].Type != "text" {
		return messages
	}
	content[last].Text += "\n\n" + cued.block
	carried.Content = content
	messages[cued.at] = carried
	return messages
}

// manualCueFor is the block itself: empty for a message that is not about
// aforge, and empty again when the corpus has nothing for one that is.
//
// It is a free function so the shape of the block can be tested without an
// agent around it — the gate, the caps and the wording are the whole of what
// this mechanism is.
func manualCueFor(message string) string {
	if !manual.Chat().Cued(message) {
		return ""
	}
	sections := manual.Chat().Search(message, manualCueSections)
	if len(sections) == 0 {
		return ""
	}
	block := manualCueOpening
	shown := 0
	for _, section := range sections {
		line := "[" + section.Page + " · " + section.Title + "] " + manualCueClause(section.Body) + "\n"
		if len(block)+len(line) > manualCueCap {
			break
		}
		block += line
		shown++
	}
	if shown == 0 {
		return ""
	}
	return strings.TrimRight(block, "\n")
}

// manualCueClause is a section's opening words: its first line of prose, cut to
// [manualCueLineCap]. A section that opens on a list item or a heading has that
// line as its opening words like any other — the point is to show what the page
// is about, and the first thing written under a heading is what it is about.
func manualCueClause(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return clip(line, manualCueLineCap)
		}
	}
	return ""
}
