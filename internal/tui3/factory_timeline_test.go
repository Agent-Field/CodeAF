package tui3

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE RUN'S STORY'S PROOF ─────────────────────────────────────────────────
//
// factory_timeline.go is not wired into the item page on this branch (the
// facets lane does that), so these tests call its four functions directly on
// a lab item, the way the dispatcher will.

// factoryTLChat is a stage's conversation on disk, the way the runner leaves
// one: seeded by the session's own door, then the lines given appended.
func factoryTLChat(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := session.SeedConversation(path, t.TempDir(), "stage", "the stage's brief"); err != nil {
		t.Fatal(err)
	}
	factoryTLAppend(t, path, lines...)
	return path
}

// factoryTLAppend writes more lines onto a conversation, as its writer does.
func factoryTLAppend(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

// factoryTLCall is one tool call as the journal writes it, and its result
// when answered.
func factoryTLCall(id, tool, field, value string, answered bool) []string {
	args := `{\"` + field + `\":\"` + value + `\"}`
	out := []string{`{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"` + id + `","type":"function","function":{"name":"` + tool + `","arguments":"` + args + `"}}]}`}
	if answered {
		out = append(out, `{"type":"message","role":"tool","toolCallId":"`+id+`","content":"ok"}`)
	}
	return out
}

func factoryTLSaidLine(words string) string {
	return `{"type":"message","role":"assistant","content":"` + words + `"}`
}

const factoryTLPlanResult = "Add a reclaim step to session/trees: delete node_modules and target after a session is terminal, keep .git and receipts. Two files, one test. Nothing else moves."

// factoryTLLab is the verb lab with #1551 at its write stage: plan done with
// its conversation, write running with its own, and every stage after it to
// come. The item page is open on it and the transcripts are read.
func factoryTLLab(t *testing.T, shape func(it *factory.Item)) (*app, *factoryFake, string, string) {
	t.Helper()
	var plan []string
	plan = append(plan, factoryTLCall("p1", "read", "path", "internal/session/trees.go", true)...)
	plan = append(plan, factoryTLCall("p2", "grep", "pattern", "node_modules", true)...)
	plan = append(plan, factoryTLSaidLine(factoryTLPlanResult))
	planChat := factoryTLChat(t, plan...)
	var write []string
	write = append(write, factoryTLCall("w1", "read", "path", "internal/session/session.go", true)...)
	write = append(write, factoryTLCall("w2", "edit", "path", "internal/session/trees.go", true)...)
	write = append(write, factoryTLCall("w3", "bash", "command", "go build ./...", false)...)
	writeChat := factoryTLChat(t, write...)
	f := &factoryFake{}
	factoryShapeItem(f, 2, func(it *factory.Item) {
		s := *it.Stream
		s.Phases = []factory.Phase{
			{Name: "plan", State: factory.PhaseDone, Chat: planChat},
			{Name: "write", State: factory.PhaseRunning, Chat: writeChat},
			{Name: "test", State: factory.PhasePending},
			{Name: "review", State: factory.PhasePending},
			{Name: "neaten", State: factory.PhasePending},
			{Name: "proof", State: factory.PhasePending},
		}
		s.Cur = 1
		it.Stream = &s
		if shape != nil {
			shape(it)
		}
	})
	a := factoryVerbLab(t, f)
	a.linear = false
	now := factoryTestNow
	a.clock = func() time.Time { return now }
	factoryLabRead(t, a)
	factoryOn(t, a, 2)
	drive(t, a, key("enter"))
	if !a.fp.open {
		t.Fatal("enter did not open the item page")
	}
	factoryTLRead(t, a)
	return a, f, planChat, writeChat
}

// factoryTLRead runs the timeline's read to its end, as the beat would.
func factoryTLRead(t *testing.T, a *app) {
	t.Helper()
	a.fp.tl.readAt = time.Time{}
	if cmd := a.factoryTimelineWake(); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
}

// factoryTLItem is the item the page stands on.
func factoryTLItem(t *testing.T, a *app) factory.Item {
	t.Helper()
	it, ok := a.factoryCursorItem()
	if !ok {
		t.Fatal("no item under the cursor")
	}
	return it
}

// factoryTLPlain is the pane at width, room rows, plain.
func factoryTLPlain(t *testing.T, a *app, width, room int) []string {
	t.Helper()
	rows := a.factoryTimelinePane(factoryTLItem(t, a), width, room)
	if len(rows) != room {
		t.Fatalf("the pane drew %d rows for a room of %d", len(rows), room)
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		if w := ansi.StringWidth(r); w != width {
			t.Fatalf("pane row %d is %d cells, not %d: %q", i, w, width, ansi.Strip(r))
		}
		out[i] = ansi.Strip(r)
	}
	return out
}

func factoryTLRowWith(rows []string, words string) int {
	for i, r := range rows {
		if strings.Contains(r, words) {
			return i
		}
	}
	return -1
}

// factoryTLHitOf is the hit of kind for phase the last draw placed.
func factoryTLHitOf(t *testing.T, a *app, kind factoryTLKind, phase int) factoryTLHit {
	t.Helper()
	for _, h := range a.fp.tl.hits {
		if h.kind == kind && h.phase == phase {
			return h
		}
	}
	t.Fatalf("the draw placed no %d for phase %d: %+v", kind, phase, a.fp.tl.hits)
	return factoryTLHit{}
}

// A RUNNING ITEM IS ITS STORY at 120 columns: the finished plan folded to its
// head, two lines of what it came to and `▸ 2 steps` flush right; the running
// write open with its steps in the step gutter, the call still going on the
// spinner; the stages to come on one dim line; the rule and the box last.
// THE GRID HOLDS: every row is the pane's width, every head starts at the
// margin, every step's object in one column, and the button ends at the right
// margin. No banned word is drawn.
func TestFactoryTimelineDrawsTheRunsStory(t *testing.T) {
	a, f, _, _ := factoryTLLab(t, nil)
	seam, _, _ := talkSeam(t, f)
	a.factory = seam
	const width, room = 120, 24
	rows := factoryTLPlain(t, a, width, room)
	t.Logf("the pane at 120 columns:\n%s", strings.Join(rows, "\n"))
	lead := "" // the page lays the margins; the pane starts at its first cell
	done := a.icon(tokens.GStepDone)
	plan := factoryTLRowWith(rows, done+" plan")
	if plan != 0 || !strings.HasPrefix(rows[plan], lead+done+" plan") {
		t.Fatalf("the plan head is not first at the margin: %q", rows)
	}
	if !strings.Contains(rows[plan+1], "Add a reclaim step to session/trees") || !strings.Contains(rows[plan+2], "Two files, one test.") {
		t.Fatalf("the plan's result is not its two sentences under its head:\n%s", strings.Join(rows[:4], "\n"))
	}
	if strings.Contains(strings.Join(rows, "\n"), "Nothing else moves") {
		t.Fatal("the folded result ran past two sentences")
	}
	button := a.linearMark(tokens.GlyphCollapsed, ">") + " 2 steps"
	if !strings.HasSuffix(strings.TrimRight(rows[plan+2], " "), button) || len(strings.TrimRight(rows[plan+2], " ")) == 0 {
		t.Fatalf("the plan's last result line does not end in %q: %q", button, rows[plan+2])
	}
	if end := ansi.StringWidth(strings.TrimRight(rows[plan+2], " ")); end != width {
		t.Fatalf("the button ends at cell %d, not at the pane's edge %d", end, width)
	}
	// THE BUTTON'S SPOT IS WHERE IT IS DRAWN, counted from the first cell past
	// the margin, as the item page reads a press.
	if btn := factoryTLHitOf(t, a, factoryTLSteps, 0); btn.row != plan+2 || btn.x1 != width || btn.x0 != btn.x1-ansi.StringWidth(button) {
		t.Fatalf("the button's spot is %+v", btn)
	}
	if head := factoryTLHitOf(t, a, factoryTLHead, 0); head.row != plan || head.x0 != 0 {
		t.Fatalf("the plan head's spot is %+v", head)
	}
	write := factoryTLRowWith(rows, " write")
	if write != plan+4 || !strings.HasPrefix(rows[write], lead+a.factorySpin()+" write") {
		t.Fatalf("the running write head is not after a blank under plan, on the spinner: %d %q", write, rows)
	}
	read := factoryTLRowWith(rows, "internal/session/session.go")
	edit := factoryTLRowWith(rows, "internal/session/trees.go")
	bash := factoryTLRowWith(rows, "go build ./...")
	if read != write+1 || edit != write+2 || bash != write+3 {
		t.Fatalf("the write's steps are not under its head in order (%d %d %d):\n%s", read, edit, bash, strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[read], a.actionMarkFor(session.ActionRead)+" read") || !strings.HasPrefix(strings.TrimSpace(rows[bash]), a.factorySpin()+" bash") {
		t.Fatalf("the steps do not wear the step gutter:\n%s\n%s", rows[read], rows[bash])
	}
	col := -1
	for _, at := range []int{read, edit, bash} {
		c := strings.Index(rows[at], "internal/")
		if at == bash {
			c = strings.Index(rows[at], "go build")
		}
		if col >= 0 && c != col {
			t.Fatalf("a step's object stands at byte %d, another at %d:\n%s", c, col, strings.Join(rows[read:bash+1], "\n"))
		}
		col = c
	}
	pending := factoryTLRowWith(rows, a.factoryPendingMark()+" test")
	if pending != bash+2 || !strings.Contains(rows[pending], " test") || !strings.Contains(rows[pending], rowSep+a.factoryPendingMark()+" review") || !strings.Contains(rows[pending], "proof") {
		t.Fatalf("the stages to come are not one line after a blank: %q", rows)
	}
	if !strings.Contains(rows[room-2], strings.Repeat("─", width)) {
		t.Fatalf("no rule over the box: %q", rows[room-2])
	}
	if !strings.HasPrefix(rows[room-1], lead+tokens.GlyphPromptChat+" "+wordSaySomething) {
		t.Fatalf("the box is not the last row: %q", rows[room-1])
	}
	all := strings.Join(rows, "\n")
	for _, banned := range []string{"verdict", "auditor", "verified", "refuted"} {
		if strings.Contains(all, banned) {
			t.Fatalf("the pane says %q", banned)
		}
	}
	if at, ok := a.factoryTimelineAt(); !ok || at != 1 {
		t.Fatalf("the cursor did not land on the running write: %d %v", at, ok)
	}
}

// THE LIVE SECTION GROWS WHEN ITS TRANSCRIPT GROWS: a call written to the
// write stage's file is a step on the next beat, read from where the last read
// stopped; past [factoryTimelineLiveRows] the oldest are counted above.
func TestFactoryTimelineLiveSectionGrows(t *testing.T) {
	a, _, _, writeChat := factoryTLLab(t, nil)
	before := a.fp.tl.files[writeChat].off
	if strings.Contains(strings.Join(factoryTLPlain(t, a, 120, 40), "\n"), "go vet") {
		t.Fatal("a step drawn before it was written")
	}
	factoryTLAppend(t, writeChat, factoryTLCall("w4", "bash", "command", "go vet ./internal/session", false)...)
	if a.factoryTimelineWake() != nil {
		t.Fatal("the timeline read again inside its beat")
	}
	factoryTLRead(t, a)
	if after := a.fp.tl.files[writeChat].off; after <= before {
		t.Fatalf("the read did not move on from %d (now %d)", before, after)
	}
	rows := factoryTLPlain(t, a, 120, 40)
	if at := factoryTLRowWith(rows, "go vet ./internal/session"); at < 0 {
		t.Fatalf("the new step is not drawn:\n%s", strings.Join(rows, "\n"))
	}
	for i := 0; i < 20; i++ {
		factoryTLAppend(t, writeChat, factoryTLCall("x"+itoa(i), "read", "path", "f"+itoa(i)+".go", true)...)
	}
	factoryTLRead(t, a)
	rows = factoryTLPlain(t, a, 120, 40)
	more := factoryTLRowWith(rows, wordMoreAbove)
	head := factoryTLRowWith(rows, " write")
	if more != head+1 || !strings.Contains(rows[more], "… 13 "+wordMoreAbove) {
		t.Fatalf("the older steps are not counted under the head:\n%s", strings.Join(rows, "\n"))
	}
	if last := factoryTLRowWith(rows, "f19.go"); last != head+factoryTimelineLiveRows {
		t.Fatalf("the newest step is not the section's last of %d rows: %d after %d", factoryTimelineLiveRows, last, head)
	}
}

// DIVE IN AND BACK: a click on `▸ 2 steps` shows the plan's whole
// conversation through the task page's renderer with the keys that leave on
// the last row, `esc` comes back to the story with the cursor on plan, and
// `enter` on the running write's head dives into it too. A second `enter`
// opens the stage's conversation itself.
func TestFactoryTimelineDiveInAndBack(t *testing.T) {
	a, _, planChat, _ := factoryTLLab(t, nil)
	it := factoryTLItem(t, a)
	factoryTLPlain(t, a, 120, 24)
	btn := factoryTLHitOf(t, a, factoryTLSteps, 0)
	if _, took := a.factoryTimelinePress(it, btn.x0+1, btn.row); !took || !a.fp.tl.diving || a.fp.tl.dive != 0 {
		t.Fatalf("a click on the steps did not dive into plan: %+v", a.fp.tl)
	}
	rows := factoryTLPlain(t, a, 120, 24)
	all := strings.Join(rows, "\n")
	if !strings.Contains(all, "node_modules") || !strings.Contains(all, "Nothing else moves") {
		t.Fatalf("the dive does not draw the plan's conversation:\n%s", all)
	}
	if want := factoryHintClause(keyOpen, wordOpensTheChat) + rowSep + factoryHintClause(keyBack, wordBack); strings.TrimSpace(rows[23]) != want {
		t.Fatalf("the dive's last row is %q, want %q", rows[23], want)
	}
	if _, took := a.factoryTimelineKey(it, key("esc")); !took || a.fp.tl.diving {
		t.Fatal("esc did not leave the dive")
	}
	if at, ok := a.factoryTimelineAt(); !ok || at != 0 {
		t.Fatalf("back on %d, not on plan", at)
	}
	if _, took := a.factoryTimelineKey(it, key("esc")); took {
		t.Fatal("esc on the story was taken")
	}
	factoryTLPlain(t, a, 120, 24)
	a.factoryTimelineKey(it, key("down"))
	if at, _ := a.factoryTimelineAt(); at != 1 {
		t.Fatalf("down did not walk to write: %d", at)
	}
	if _, took := a.factoryTimelineKey(it, key("enter")); !took || !a.fp.tl.diving || a.fp.tl.dive != 1 {
		t.Fatal("enter on the write head did not dive")
	}
	if rows := factoryTLPlain(t, a, 120, 24); !strings.Contains(strings.Join(rows, "\n"), "go build") {
		t.Fatalf("the write's dive does not draw its calls:\n%s", strings.Join(rows, "\n"))
	}
	a.factoryTimelineKey(it, key("esc"))
	a.factoryTimelineKey(it, key("up"))
	a.factoryTimelineKey(it, key("enter"))
	var opened string
	a.open = func(where, file string) (Conversation, error) {
		opened = file
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: file, Workspace: where}, nil
	}
	cmd, took := a.factoryTimelineKey(it, key("enter"))
	if !took || cmd == nil {
		t.Fatal("a second enter in the dive opened nothing")
	}
	drive(t, a, runCmd(cmd)...)
	if opened != planChat {
		t.Fatalf("the second enter opened %q, not the plan's conversation", opened)
	}
}

// A CLICK ON A HEAD FOLDS AND OPENS IT: the finished plan opens to its
// steps, and folds back to its result.
func TestFactoryTimelineClickFoldsAHead(t *testing.T) {
	a, _, _, _ := factoryTLLab(t, nil)
	it := factoryTLItem(t, a)
	factoryTLPlain(t, a, 120, 30)
	head := factoryTLHitOf(t, a, factoryTLHead, 0)
	a.factoryTimelinePress(it, head.x0, head.row)
	rows := factoryTLPlain(t, a, 120, 30)
	if factoryTLRowWith(rows, "grep") < 0 || factoryTLRowWith(rows, wordSaid) < 0 {
		t.Fatalf("the opened plan does not show its steps:\n%s", strings.Join(rows, "\n"))
	}
	if at, _ := a.factoryTimelineAt(); at != 0 {
		t.Fatal("the click did not put the cursor on plan")
	}
	head = factoryTLHitOf(t, a, factoryTLHead, 0)
	a.factoryTimelinePress(it, head.x0, head.row)
	if rows := factoryTLPlain(t, a, 120, 30); factoryTLRowWith(rows, "grep") >= 0 {
		t.Fatalf("the second click did not fold plan:\n%s", strings.Join(rows, "\n"))
	}
	if _, took := a.factoryTimelinePress(it, 0, factoryTLRowWith(factoryTLPlain(t, a, 120, 30), "go build")); took {
		t.Fatal("a click on a step line did something")
	}
}

// THE POINTER PAINTS ONE HEAD: resting on plan's head puts the pointer's
// ground on that row and on no other the cursor does not already hold, and
// moving off takes it away.
func TestFactoryTimelineHoverPaintsOneHead(t *testing.T) {
	a, _, _, _ := factoryTLLab(t, nil)
	a.pal = newPalette(tokens.TrueColor, false)
	it := factoryTLItem(t, a)
	ground := factoryGround(a)
	painted := func() []int {
		var out []int
		for i, r := range a.factoryTimelinePane(it, 120, 24) {
			if strings.Contains(r, ground) {
				out = append(out, i)
			}
		}
		return out
	}
	before := painted()
	head := factoryTLHitOf(t, a, factoryTLHead, 0)
	if !a.factoryTimelineHover(it, head.x0+1, head.row) {
		t.Fatal("the pointer on plan's head changed nothing")
	}
	if a.factoryTimelineHover(it, head.x0+1, head.row) {
		t.Fatal("the pointer resting still changed something")
	}
	after := painted()
	if len(after) != len(before)+1 || !slices.Contains(after, head.row) {
		t.Fatalf("the hover painted rows %v over %v, want one more: %d", after, before, head.row)
	}
	a.factoryTimelineHover(it, 0, 22)
	if got := painted(); len(got) != len(before) {
		t.Fatalf("the ground stayed after the pointer left: %v", got)
	}
}

// THE BOX TYPES AND SENDS: the cursor walks down to the box, `enter` opens
// the floor's typing row on the pane's last row, the words type into it, and
// `enter` opens the item's conversation with the words typed in its box.
func TestFactoryTimelineBoxTypesAndSends(t *testing.T) {
	a, f, _, _ := factoryTLLab(t, nil)
	seam, _, chat := talkSeam(t, f)
	a.factory = seam
	var opened string
	a.open = func(where, file string) (Conversation, error) {
		opened = file
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: file, Workspace: where}, nil
	}
	it := factoryTLItem(t, a)
	factoryTLPlain(t, a, 120, 24)
	for i := 0; i < 5; i++ {
		a.factoryTimelineKey(it, key("down"))
	}
	if at, ok := a.factoryTimelineAt(); ok || at != 0 || a.fp.tl.at != -1 {
		t.Fatalf("the cursor did not reach the box: %+v", a.fp.tl.at)
	}
	a.factoryTimelineKey(it, key("enter"))
	if ask := a.fp.act.ask; ask == nil || ask.kind != factoryAskManager {
		t.Fatal("enter on the box opened no typing row")
	}
	factoryType(t, a, "keep the old flag")
	rows := factoryTLPlain(t, a, 120, 24)
	if !strings.Contains(rows[23], "keep the old flag") {
		t.Fatalf("the typed words are not in the box: %q", rows[23])
	}
	for _, foot := range a.factoryFootRows(100) {
		if strings.Contains(ansi.Strip(foot), "keep the old flag") {
			t.Fatal("the box's typing row is drawn a second time at the pane's foot")
		}
	}
	drive(t, a, key("enter"))
	if opened != chat || a.pageShowing() {
		t.Fatalf("enter in the box opened %q (page showing %v), want %q", opened, a.pageShowing(), chat)
	}
	if got := a.input.String(); got != "keep the old flag" {
		t.Fatalf("the conversation's box holds %q", got)
	}
}

// THE MANAGER'S LINES INTERLEAVE: a progress line is drawn after the section
// it is about, the person's reply after it, an ordinary reply in the
// conversation not at all, and a line about a stage still to come after the
// line of stages to come.
func TestFactoryTimelineManagerLinesInterleave(t *testing.T) {
	progress := func(words string) string {
		return `{"type":"message","role":"assistant","content":"` + words + `","presentation":{"audience":"human","kind":"factory-progress"}}`
	}
	talk := factoryTLChat(t,
		`{"type":"message","role":"user","content":"what is the plan"}`,
		factoryTLSaidLine("an ordinary reply"),
		progress("plan done · 2m · $0.04 · a reclaim step"),
		`{"type":"message","role":"user","content":"keep the old flag"}`,
		progress("test failed 1 of 2 · asking you"),
	)
	a, _, _, _ := factoryTLLab(t, func(it *factory.Item) { it.Talk = talk })
	rows := factoryTLPlain(t, a, 120, 30)
	all := strings.Join(rows, "\n")
	if strings.Contains(all, "an ordinary reply") || strings.Contains(all, "what is the plan") {
		t.Fatalf("the conversation around the progress is drawn:\n%s", all)
	}
	plan := factoryTLRowWith(rows, "Two files, one test.")
	mgr := factoryTLRowWith(rows, wordManager+rowSep+"plan done")
	you := factoryTLRowWith(rows, wordYou+rowSep+"keep the old flag")
	write := factoryTLRowWith(rows, " write")
	pending := factoryTLRowWith(rows, " review")
	failed := factoryTLRowWith(rows, wordManager+rowSep+"test failed 1 of 2")
	if !(plan < mgr && mgr < you && you < write && pending < failed) {
		t.Fatalf("the manager's lines are out of place (%d %d %d %d %d %d):\n%s", plan, mgr, you, write, pending, failed, all)
	}
	if mgr != plan+2 {
		t.Fatalf("the progress line does not stand apart under plan:\n%s", all)
	}

	// AN ITEM WITH NO CONVERSATION draws no manager line.
	b, _, _, _ := factoryTLLab(t, nil)
	if strings.Contains(strings.Join(factoryTLPlain(t, b, 120, 30), "\n"), wordManager+rowSep) {
		t.Fatal("an item with no conversation drew a manager line")
	}
}

// BEFORE A RUN the pane is the issue's one line and the box saying `r runs
// it`, and the cursor's only stop is the box.
func TestFactoryTimelineBeforeARun(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	seam, _, _ := talkSeam(t, f)
	a.factory = seam
	factoryOn(t, a, 8)
	drive(t, a, key("enter"))
	rows := factoryTLPlain(t, a, 120, 12)
	if !strings.Contains(rows[0], "your own words; no triage needed beyond sizing") {
		t.Fatalf("the issue's line is not first: %q", rows[0])
	}
	want := tokens.GlyphPromptChat + " " + factoryHintClause(keyRun, wordRunsIt) + rowSep + wordTellMeFirst
	if strings.TrimSpace(rows[11]) != want {
		t.Fatalf("the box before a run is %q, want %q", rows[11], want)
	}
	if a.fp.tl.at != -1 || len(a.fp.tl.stops) != 1 {
		t.Fatalf("the stops before a run are %v", a.fp.tl.stops)
	}
}

// THE CLOCK THE SURFACE SAW: a phase watched from its start to its end says
// how long it took and what it spent, `✓ plan · 2m · $0.04`.
func TestFactoryTimelineClockTheSurfaceSaw(t *testing.T) {
	stage, spent, at := 0, 1.0, factoryTestNow
	f := &factoryFake{}
	f.shape = func(s *factory.Snapshot) {
		s.Now = at
		for i := range s.Items {
			if s.Items[i].ID != 2 {
				continue
			}
			st := *s.Items[i].Stream
			st.Spent = spent
			st.Phases = []factory.Phase{{Name: "plan", State: factory.PhaseRunning}, {Name: "write", State: factory.PhasePending}}
			if stage == 1 {
				st.Phases[0].State, st.Phases[1].State = factory.PhaseDone, factory.PhaseRunning
			}
			s.Items[i].Stream = &st
		}
	}
	a := factoryVerbLab(t, f)
	now := factoryTestNow
	a.clock = func() time.Time { return now }
	factoryLabRead(t, a)
	factoryOn(t, a, 2)
	drive(t, a, key("enter"))
	a.factoryTimelineWake()
	stage, spent, at = 1, 1.04, factoryTestNow.Add(2*time.Minute)
	now = at
	factoryLabRead(t, a)
	a.factoryTimelineWake()
	rows := factoryTLPlain(t, a, 120, 12)
	if want := a.icon(tokens.GStepDone) + " plan" + rowSep + "2m" + rowSep + "$0.04"; !strings.Contains(rows[0], want) {
		t.Fatalf("the plan head is %q, want %q", rows[0], want)
	}
}

// ONE CURSOR, TWO COLUMNS: walking the left column to a stage row moves the
// story's cursor to that section, a click on a section head selects its row,
// and `esc` inside a dive climbs back to the story before it leaves the page.
func TestFactoryTimelineAndTheRowsShareOneCursor(t *testing.T) {
	a, _, _, _ := factoryTLLab(t, nil)
	it := factoryTLItem(t, a)
	factoryTLPlain(t, a, 120, 24)
	rows := a.factoryItemRows(it)
	write := -1
	for i, r := range rows {
		if r.kind == factoryPageStage && r.at == 1 {
			write = i
		}
	}
	if write < 0 {
		t.Fatalf("no row for the write stage in %+v", rows)
	}
	a.factoryStageSelect(write)
	if at, ok := a.factoryTimelineAt(); !ok || at != 1 {
		t.Fatalf("selecting the write row left the story on %d (%v)", at, ok)
	}
	a.factoryTLMove(-1)
	if r, _ := a.factoryPageRowAt(it); r.kind != factoryPageStage || r.at != 0 {
		t.Fatalf("walking the story up did not select the plan row: %+v", r)
	}
	a.factoryTLDiveIn(0)
	a.factoryStageSelect(write)
	if a.fp.tl.diving {
		t.Fatal("selecting another stage's row stayed inside the plan's dive")
	}
	a.factoryTLDiveIn(1)
	if cmd, took := a.factoryLayoutKey(key("esc")); !took || a.fp.tl.diving || !a.fp.open || cmd != nil {
		t.Fatalf("esc in a dive: took=%v diving=%v open=%v", took, a.fp.tl.diving, a.fp.open)
	}
}
