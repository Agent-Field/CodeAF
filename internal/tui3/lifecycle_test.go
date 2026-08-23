package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE UX-POLISH WAVE, checked where it is visible.
//
// Four defects and one absence are the subject: a mutating call that spun while
// nothing was running, a consent question that spun and whispered, a status bar
// parked at the far end of the screen from everything that moves, and a surface
// that never reacted to a pointer at all.

// announced is the event session sends when the model has finished ASKING for a
// call — the row exists, nothing has started (session/loop.go).
func announced(tool, hint, args string) session.Event {
	return session.Event{Kind: session.EventToolAnnounced, Tool: tool, Hint: hint, Args: args}
}

func beginWith(tool, hint, args string) session.Event {
	return session.Event{Kind: session.EventToolBegin, Tool: tool, Hint: hint, Args: args}
}

// firstTool is the index of the first tool entry.
func firstTool(t *testing.T, a *app) int {
	t.Helper()
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			return i
		}
	}
	t.Fatalf("no tool entry:\n%s", strings.Join(plainRows(a), "\n"))
	return -1
}

// toolRowText is the first tool row, painted.
func toolRowText(t *testing.T, a *app) string {
	t.Helper()
	for _, r := range rows(a) {
		if r.hit == hitTool {
			return r.text
		}
	}
	t.Fatalf("no tool row:\n%s", strings.Join(plainRows(a), "\n"))
	return ""
}

const editPayload = `{"path":"internal/session/loop.go","edits":[{"oldText":"const argsLimit = 400","newText":"const argsLimit = 8192"}]}`

// ── 1. the state machine ────────────────────────────────────────────────────

// ONE CALL, FOUR STATES, AND ONLY ONE OF THEM SPINS. The row is drawn once and
// walks: announced → asked about → running → done. A second row anywhere in
// that walk would be the surface telling the person the model called the tool
// twice.
func TestAToolWalksFromQueuedThroughConsentToDone(t *testing.T) {
	agent := &wiredAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{{
		announced("bash", "bash rm -rf build", `{"command":"rm -rf build"}`),
	}}}}
	a := newTestApp(agent)
	typeLine(t, a, "clean the build")

	at := firstTool(t, a)
	if got := a.entries[at].status; got != toolQueued {
		t.Fatalf("an announced call is in state %v, want queued", got)
	}
	line := plain(toolRowText(t, a))
	if !strings.Contains(line, glyphQueued) {
		t.Fatalf("a queued call has no ◌: %q", line)
	}
	if strings.ContainsAny(line, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("A QUEUED CALL IS SPINNING. Nothing has started: %q", line)
	}

	// The question. It lands on the row that is already there.
	drive(t, a, streamEventMsg{gen: a.gen, ev: consentEvent(1, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`)})
	if got := a.entries[at].status; got != toolConsent {
		t.Fatalf("the asked-about call is in state %v, want consent", got)
	}
	if line := plain(toolRowText(t, a)); !strings.Contains(line, glyphAsk) {
		t.Fatalf("a call waiting on a person has no ?: %q", line)
	}
	if strings.ContainsAny(plain(toolRowText(t, a)), "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("A CONSENT-PENDING CALL IS SPINNING: %q", plain(toolRowText(t, a)))
	}

	// Answered, it runs — and now, and only now, it spins.
	drive(t, a, key("a"))
	if got := a.entries[at].status; got != toolRunning {
		t.Fatalf("the allowed call is in state %v, want running", got)
	}
	if line := plain(toolRowText(t, a)); !strings.ContainsAny(line, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("a running call does not spin: %q", line)
	}

	// Done. One row for the whole life of the call.
	drive(t, a, streamEventMsg{gen: a.gen, ev: toolEnd("bash", "")})
	if got := a.entries[at].status; got != toolOK {
		t.Fatalf("the finished call is in state %v, want ok", got)
	}
	calls := 0
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			calls++
		}
	}
	if calls != 1 {
		t.Fatalf("one call drew %d rows", calls)
	}
}

// A begin with no announcement behind it still draws its own row: a provider
// that does not stream tool calls, or a surface that attached mid-batch.
func TestABeginWithNoAnnouncementStillDrawsARow(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("read", "read foo/bar.go"),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "read it")

	at := firstTool(t, a)
	if a.entries[at].status != toolRunning {
		t.Fatalf("an unannounced begin is in state %v, want running", a.entries[at].status)
	}
	if a.entries[at].began.IsZero() {
		t.Fatal("the call's clock never started")
	}
}

// Two announcements of the same tool pair with their own begins by PAYLOAD, so
// three edits to three files do not start each other's clocks.
func TestAnnouncementsPairWithTheirOwnBeginsByPayload(t *testing.T) {
	first := `{"path":"a.go","edits":[{"oldText":"x","newText":"y"}]}`
	second := `{"path":"b.go","edits":[{"oldText":"p","newText":"q"}]}`
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		announced("edit", "edit a.go", first),
		announced("edit", "edit b.go", second),
		beginWith("edit", "edit b.go", second),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "edit both")

	var states []toolState
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			states = append(states, a.entries[i].status)
		}
	}
	if len(states) != 2 {
		t.Fatalf("two announced calls drew %d rows", len(states))
	}
	if states[0] != toolQueued || states[1] != toolRunning {
		t.Fatalf("the begin landed on the wrong row: %v", states)
	}
}

// A FAILURE OPENS ITSELF. Every other expansion waits to be asked for; the one
// row whose detail is the reason the person is looking at the screen does not.
func TestAFailedToolAutoExpandsItsDetail(t *testing.T) {
	a := toolApp(t, tokens.ANSI256, failedCall("bash",
		`{"command":"go build ./..."}`,
		"undefined: railMid",
		"undefined: railMid\n\nCommand exited with code 1"))

	at := firstTool(t, a)
	if !a.entries[at].open {
		t.Fatal("a failed call did not open itself")
	}
	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "│ undefined: railMid") {
		t.Fatalf("the failure's detail is not on screen without a click:\n%s", body)
	}

	// A SUCCESS does not, and that asymmetry is the point.
	ok := toolApp(t, tokens.ANSI256, call("bash", `{"command":"go build ./..."}`, "ok"))
	if ok.entries[firstTool(t, ok)].open {
		t.Fatal("a successful call opened itself")
	}
}

// The elapsed time is the call's OWN duration, at the line's right end, once it
// is done — and a call too fast to have one draws nothing, for the reason there
// is no success glyph.
func TestAFinishedCallCarriesItsElapsedTime(t *testing.T) {
	a := toolApp(t, tokens.ANSI256, call("read", `{"path":"a.go"}`, "one\ntwo"))
	at := firstTool(t, a)

	if got := elapsedWord(&a.entries[at]); got != "" {
		t.Fatalf("an instant call drew %q, want nothing", got)
	}

	// A call that took real time says how much, and says it dim, at the end.
	a.entries[at].began = time.Now().Add(-1200 * time.Millisecond)
	a.entries[at].ended = time.Now()
	a.touch()
	if got := elapsedWord(&a.entries[at]); got != "1.2s" {
		t.Fatalf("elapsed is %q, want 1.2s", got)
	}
	line := plain(toolRowText(t, a))
	if !strings.HasSuffix(strings.TrimRight(line, " "), "1.2s") {
		t.Fatalf("the elapsed time is not at the line's right end: %q", line)
	}
	if !strings.Contains(toolRowText(t, a), a.pal.dim("1.2s")) {
		t.Fatalf("the elapsed time is not dim: %q", toolRowText(t, a))
	}

	// A running call has no elapsed time — a number that changes under a
	// spinner is a number nobody can read.
	a.entries[at].status = toolRunning
	if got := elapsedWord(&a.entries[at]); got != "" {
		t.Fatalf("a running call drew an elapsed time: %q", got)
	}
}

func TestElapsedWordSpellsEveryScale(t *testing.T) {
	for _, c := range []struct {
		took time.Duration
		want string
	}{
		{40 * time.Millisecond, ""},
		{450 * time.Millisecond, "0.5s"},
		{9500 * time.Millisecond, "9.5s"},
		{42 * time.Second, "42s"},
		{93 * time.Second, "1m33s"},
		{125 * time.Second, "2m05s"},
	} {
		e := entry{status: toolOK, began: time.Now(), ended: time.Now().Add(c.took)}
		if got := elapsedWord(&e); got != c.want {
			t.Fatalf("%v renders as %q, want %q", c.took, got, c.want)
		}
	}
}

// ── 2. the consent question, unmissable ─────────────────────────────────────

// THE VIOLET. The question is the one thing on this surface painted in the
// fifth colour, and it is painted in it everywhere at once: the row, the
// choices, and the word in the status line. Anything less was the defect — a
// person who could not tell the agent had stopped and was waiting for them.
func TestTheConsentQuestionIsVioletEverywhereAtOnce(t *testing.T) {
	agent, a := wired([]session.Event{
		toolBegin("bash", "bash rm -rf build"),
		consentEvent(9, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
	})
	_ = agent
	a.pal = newPalette(tokens.TrueColor, false)
	typeLine(t, a, "clean it")

	// The authored question hue, on the wire, as truecolor.
	violet := "\x1b[38;2;192;143;232m"
	painted, _, _, _ := a.chrome(a.width)
	block := strings.Join(painted, "\n")
	if !strings.Contains(block, violet) {
		t.Fatalf("the question block is not violet:\n%q", block)
	}
	offer := painted[consentOfferRow+1] // the rule sits above the block: [rule, call, offer, …]
	if !strings.Contains(offer, "allow?") {
		// The rule row is only drawn when the frame is roomy; find the offer.
		for _, line := range painted {
			if strings.Contains(plain(line), "allow?") {
				offer = line
			}
		}
	}
	if !strings.Contains(offer, violet) {
		t.Fatalf("THE CHOICES ARE NOT VIOLET — they were dim, which is the defect: %q", offer)
	}
	if strings.Contains(offer, a.pal.dim("allow? ")) {
		t.Fatalf("the offer is still painted dim: %q", offer)
	}

	// The row the question is about carries the marker and the hue.
	row := toolRowText(t, a)
	if !strings.Contains(plain(row), glyphAsk) || !strings.Contains(row, violet) {
		t.Fatalf("the asked-about row is not marked: %q", row)
	}

	// And the status line says so in words as well as in colour.
	status := a.status(a.width)
	if !strings.Contains(plain(status), waitingWord) {
		t.Fatalf("the status line does not say the surface is waiting: %q", plain(status))
	}
	if !strings.Contains(status, violet) {
		t.Fatalf("the status line's state is not violet: %q", status)
	}
	if strings.Contains(plain(status), "working") {
		t.Fatalf("a blocked turn still calls itself working: %q", plain(status))
	}
}

// Answered, the violet goes: a question hue left on screen for a question
// nobody is being asked is the hue meaning two things.
func TestAnsweringTheQuestionEndsTheViolet(t *testing.T) {
	_, a := wired([]session.Event{
		toolBegin("bash", "bash rm -rf build"),
		consentEvent(9, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
	})
	a.pal = newPalette(tokens.TrueColor, false)
	typeLine(t, a, "clean it")
	drive(t, a, key("a"))

	violet := "\x1b[38;2;192;143;232m"
	if got := frame(a); strings.Contains(got, violet) {
		t.Fatalf("the question hue outlived the question:\n%q", got)
	}
	if plain(a.status(a.width)) == waitingWord {
		t.Fatal("the status line is still waiting")
	}
}

// ── 3. the live preview ─────────────────────────────────────────────────────

// THE DIFF BEFORE THE EDIT. The change is on screen from the moment the model
// finishes asking for it — the only moment at which reading it is worth
// anything — under a header that says which of the two things is happening.
func TestTheEditPreviewAppearsOnAnnouncementAndCollapsesIntoTheStat(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		announced("edit", "edit internal/session/loop.go", editPayload),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "bump the limit")

	body := strings.Join(plainRows(a), "\n")
	for _, want := range []string{"│ pending", "│ -const argsLimit = 400", "│ +const argsLimit = 8192"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the pending preview is missing %q:\n%s", want, body)
		}
	}
	// It is a PENDING preview, not an applied one: no stat yet, because the
	// stat is what the preview collapses into.
	if strings.Contains(plain(toolRowText(t, a)), "+1") {
		t.Fatalf("a queued edit drew its stat: %q", plain(toolRowText(t, a)))
	}
	// The header is the question hue's dimmer half, never plain dim ink.
	for _, r := range rows(a) {
		if strings.Contains(plain(r.text), "pending") && !strings.Contains(r.text, a.pal.ask("pending")) {
			t.Fatalf("the pending header is not in the question hue: %q", r.text)
		}
	}

	// EXECUTION STARTS: the same diff, one different word.
	drive(t, a, streamEventMsg{gen: a.gen, ev: beginWith("edit", "edit internal/session/loop.go", editPayload)})
	body = strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "│ applying") {
		t.Fatalf("the preview did not become 'applying':\n%s", body)
	}
	if strings.Contains(body, "│ pending") {
		t.Fatalf("the preview is still pending after the begin:\n%s", body)
	}
	if !strings.Contains(body, "│ +const argsLimit = 8192") {
		t.Fatalf("the diff was redrawn or dropped on begin:\n%s", body)
	}

	// AND IT LANDS: the preview collapses into the stat, and the whole diff is
	// where it always was — one click away.
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolEnd, Tool: "edit", Args: editPayload,
		Output: "Successfully replaced 1 block(s) in internal/session/loop.go.",
	}})
	body = strings.Join(plainRows(a), "\n")
	if strings.Contains(body, "applying") || strings.Contains(body, "@@") {
		t.Fatalf("the preview did not collapse:\n%s", body)
	}
	if line := plain(toolRowText(t, a)); !strings.Contains(line, "+1") || !strings.Contains(line, "−1") {
		t.Fatalf("the stat did not replace the preview: %q", line)
	}
	expanded := strings.Join(openFirst(t, a), "\n")
	if !strings.Contains(expanded, "+const argsLimit = 8192") {
		t.Fatalf("the expansion lost the diff:\n%s", expanded)
	}
}

// A write previews its content the same way, and every preview row carries the
// rail: it belongs to the call above it.
func TestAWritePreviewsItsContentUnderTheRail(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		announced("write", "write out.txt", `{"path":"out.txt","content":"package main\n\nfunc main() {}\n"}`),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "write it")

	var preview []string
	for _, line := range plainRows(a) {
		line = strings.TrimLeft(line, " ")
		if strings.HasPrefix(line, railCont) {
			preview = append(preview, line)
		}
	}
	if len(preview) < 2 {
		t.Fatalf("the write preview is missing:\n%s", strings.Join(plainRows(a), "\n"))
	}
	if preview[0] != railCont+"pending" {
		t.Fatalf("the preview's first row is %q, want the pending header", preview[0])
	}
	if !strings.Contains(strings.Join(preview, "\n"), "func main() {}") {
		t.Fatalf("the write preview has no content:\n%s", strings.Join(preview, "\n"))
	}
}

// A bash call previews nothing — its command is already on its own line, whole
// — and says what it is doing instead of showing a preview it does not have.
func TestAToolWithNothingToPreviewShowsNoPreview(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		announced("bash", "bash go test ./...", `{"command":"go test ./..."}`),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "test it")

	for _, line := range plainRows(a) {
		if strings.HasPrefix(line, railCont) {
			t.Fatalf("a bash call drew a preview: %q", line)
		}
	}
}

// ── 4. the parameter hierarchy ──────────────────────────────────────────────

// In a successful chain, the prefix is CONTEXT and the last command is the
// substance, so repeated setup recedes without a builtin-specific rule.
func TestACommandChainLetsItsLastActionLead(t *testing.T) {
	a := toolApp(t, tokens.TrueColor,
		call("bash", `{"command":"cd internal/session && go test ./..."}`, "ok"))

	line := toolRowText(t, a)
	if !strings.Contains(line, a.pal.dim("cd internal/session && ")) {
		t.Fatalf("the cd prefix is not dim: %q", line)
	}
	// The substance after the prefix is no longer ONE ink run: the shell lexer
	// paints it in its own tiers (shellx.go), and the hierarchy this test is
	// about is unchanged — the prefix recedes whole, the work does not.
	if !strings.Contains(line, a.pal.shell("go test ./...")) {
		t.Fatalf("the command is not highlighted: %q", line)
	}
	if !strings.Contains(line, a.pal.ink("go")) {
		t.Fatalf("the command's verb is not ink: %q", line)
	}

	// A single command stays whole. Any successful chain applies the same rule,
	// so repeated setup is not special-cased to one shell builtin.
	for _, command := range []string{"cd internal/session", "go test ./..."} {
		context, rest, found := cutCommandContext(command)
		if found {
			t.Fatalf("%q was split into %q + %q", command, context, rest)
		}
	}
	if context, rest, found := cutCommandContext("prepare && cd a/b && make check"); !found ||
		context != "prepare && cd a/b && " || rest != "make check" {
		t.Fatalf("the prefix split is %q + %q (found=%v)", context, rest, found)
	}
}

// A grep's pattern is what is being looked for and its path is where: accent
// and dim, not one colour for both.
func TestAGrepPatternLeadsAndItsPathRecedes(t *testing.T) {
	a := toolApp(t, tokens.TrueColor, []session.Event{
		{Kind: session.EventToolBegin, Tool: "grep", Hint: "grep argsLimit internal/session"},
		{Kind: session.EventToolEnd, Tool: "grep", Output: "internal/session/loop.go:12: argsLimit"},
	})

	line := toolRowText(t, a)
	if !strings.Contains(line, a.pal.accent("argsLimit")) {
		t.Fatalf("the pattern is not the accent: %q", line)
	}
	if !strings.Contains(line, a.pal.dim(" internal/session")) {
		t.Fatalf("the path beside a pattern is not dim: %q", line)
	}
}

// A read's path leads and the range after it recedes.
func TestAReadPathLeadsAndItsRangeRecedes(t *testing.T) {
	a := toolApp(t, tokens.TrueColor, []session.Event{
		{Kind: session.EventToolBegin, Tool: "read", Hint: "read internal/session/loop.go 120-240"},
		{Kind: session.EventToolEnd, Tool: "read", Output: "one\ntwo"},
	})

	line := toolRowText(t, a)
	if !strings.Contains(line, a.pal.ink("internal/session/loop.go")) {
		t.Fatalf("the path is not ink: %q", line)
	}
	if !strings.Contains(line, a.pal.dim(" 120-240")) {
		t.Fatalf("the range is not dim: %q", line)
	}
}

// ── 5. hover ────────────────────────────────────────────────────────────────

// motionAt is a pointer moving to one screen row.
func motionAt(y int) tea.MouseMotionMsg { return tea.MouseMotionMsg{Y: y} }

// screenRowOf finds the screen line one visible conversation row was drawn on.
func screenRowOf(t *testing.T, a *app, want func(row) bool) int {
	t.Helper()
	body, _ := a.window(a.width, a.viewHeight())
	for i, r := range body {
		if want(r) {
			return a.bodyTop() + i
		}
	}
	t.Fatalf("no visible row matches:\n%s", strings.Join(plainRows(a), "\n"))
	return -1
}

// THE POINTER GETS AN ANSWER, AND ONLY WHERE THERE IS ONE. A tool row reacts
// because a click on it does something; the person's own message does not,
// because a click on it does not.
func TestHoverAnswersOnInteractiveRowsAndNowhereElse(t *testing.T) {
	a := toolApp(t, tokens.ANSI256, call("read", `{"path":"a.go"}`, "one\ntwo"))
	background := "\x1b[48;5;" + itoa(int(hueCursor.idx)) + "m"

	toolY := screenRowOf(t, a, func(r row) bool { return r.hit == hitTool })
	drive(t, a, motionAt(toolY))
	if a.hot.kind != hoverEntry {
		t.Fatalf("the pointer over a tool row recorded %v", a.hot)
	}
	if !strings.Contains(toolRowText(t, a), background) {
		t.Fatalf("the hovered row has no hover background: %q", toolRowText(t, a))
	}
	if !strings.Contains(toolRowText(t, a), a.pal.accent(railLast)) {
		t.Fatalf("the hovered row's marker did not brighten: %q", toolRowText(t, a))
	}

	// The person's own message is not a button.
	userY := screenRowOf(t, a, func(r row) bool {
		return r.entry >= 0 && a.entries[r.entry].kind == entryUser
	})
	drive(t, a, motionAt(userY))
	if a.hot.kind != hoverNothing {
		t.Fatalf("a user message answered the pointer: %v", a.hot)
	}
	for _, r := range rows(a) {
		if strings.Contains(r.text, background) {
			t.Fatalf("a non-interactive row is wearing the hover style: %q", r.text)
		}
	}
}

// The fold line and the thinking block are clickable, so they are hoverable —
// the same map, read the same way.
func TestHoverFollowsTheFoldLineAndTheThinkingBlock(t *testing.T) {
	var events []session.Event
	for i := 0; i < toolWindow+2; i++ {
		events = append(events, call("read", `{"path":"a.go"}`, "one")...)
	}
	a := toolApp(t, tokens.ANSI256, events)
	a.toggleLatestWorkfold()

	foldY := screenRowOf(t, a, func(r row) bool { return r.hit == hitFold })
	drive(t, a, motionAt(foldY))
	if a.hot.kind != hoverFold {
		t.Fatalf("the fold line did not answer the pointer: %v", a.hot)
	}
	background := "\x1b[48;5;" + itoa(int(hueCursor.idx)) + "m"
	var fold string
	for _, r := range rows(a) {
		if r.hit == hitFold {
			fold = r.text
		}
	}
	if !strings.Contains(fold, background) || !strings.Contains(fold, a.pal.accent(glyphTool)) {
		t.Fatalf("the hovered fold line did not react: %q", fold)
	}

	// A thought, collapsed, is one clickable row.
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventReasoning, "the file is probably under internal/"),
		text(session.EventTextDelta, "found it"),
		{Kind: session.EventTurnDone},
	}}}
	think := newTestApp(agent)
	think.pal = newPalette(tokens.ANSI256, false)
	runTurn(t, think, agent, "where is it")
	think.toggleLatestWorkfold()

	at := thoughtAt(t, think)
	thoughtY := screenRowOf(t, think, func(r row) bool { return r.entry == at })
	drive(t, think, motionAt(thoughtY))
	if !think.hoveringEntry(at) {
		t.Fatalf("the thinking block did not answer the pointer: %v", think.hot)
	}
	for _, r := range rows(think) {
		if r.entry == at && !strings.Contains(r.text, think.pal.accent(glyphThought)) {
			t.Fatalf("the hovered thought's marker did not brighten: %q", r.text)
		}
	}
}

// The chrome answers too — the consent choices and the open list are the two
// things below the conversation a person can point at.
func TestHoverReachesTheChoicesAndThePickerRows(t *testing.T) {
	_, a := wired([]session.Event{
		toolBegin("bash", "bash rm -rf build"),
		consentEvent(3, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
	})
	a.pal = newPalette(tokens.ANSI256, false)
	typeLine(t, a, "clean it")

	_, marks, _, _ := a.chrome(a.width)
	_, height := a.size()
	offerY := -1
	for i, mark := range marks {
		if mark.kind == chromeChoices {
			offerY = height - len(marks) + i
		}
	}
	if offerY < 0 {
		t.Fatal("the offer line has no chrome mark")
	}
	drive(t, a, motionAt(offerY))
	if !a.hoveringChoices() {
		t.Fatalf("the choices did not answer the pointer: %v", a.hot)
	}
	background := "\x1b[48;5;" + itoa(int(hueCursor.idx)) + "m"
	if !strings.Contains(a.consentOffer(a.width), background) {
		t.Fatalf("the hovered choices have no hover background: %q", a.consentOffer(a.width))
	}

	// And the model picker.
	picked := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	picked.pal = newPalette(tokens.ANSI256, false)
	typeLine(t, picked, "/model")
	_, marks, _, _ = picked.chrome(picked.width)
	_, height = picked.size()
	rowY := -1
	for i, mark := range marks {
		if mark.kind == chromeOverlay && mark.index == 2 {
			rowY = height - len(marks) + i
		}
	}
	if rowY < 0 {
		t.Fatal("the picker rows have no chrome marks")
	}
	drive(t, picked, motionAt(rowY))
	if !picked.hoveringOverlay(2) {
		t.Fatalf("the picker row did not answer the pointer: %v", picked.hot)
	}
	list := picked.overlayRows(picked.width, picked.overlayHeight())
	if !strings.Contains(list[2], background) {
		t.Fatalf("the hovered picker row has no hover background: %q", list[2])
	}
	for i, line := range list {
		if i != 2 && strings.Contains(line, background) {
			t.Fatalf("row %d is hovered too: %q", i, line)
		}
	}
}

// Moving along one expansion is ONE repaint, not one per row: the hover is
// tracked as an identity, and an identity that did not change asks for nothing.
func TestHoverRepaintsOnlyWhenTheAnswerChanges(t *testing.T) {
	a := toolApp(t, tokens.ANSI256, failedCall("bash", `{"command":"go build"}`,
		"first line", "first line\nsecond line\nthird line\n\nCommand exited with code 1"))

	var ys []int
	body, _ := a.window(a.width, a.viewHeight())
	for i, r := range body {
		if r.hit == hitTool && r.entry >= 0 {
			ys = append(ys, a.bodyTop()+i)
		}
	}
	if len(ys) < 3 {
		t.Fatalf("the failed call did not draw an expansion:\n%s", strings.Join(plainRows(a), "\n"))
	}
	drive(t, a, motionAt(ys[0]))
	_ = a.visible(a.width)
	before := a.builds
	for _, y := range ys[1:] {
		drive(t, a, motionAt(y))
	}
	_ = a.visible(a.width)
	if a.builds != before {
		t.Fatalf("moving within one call rebuilt the layout %d times", a.builds-before)
	}
}

// ── 6. the frame ────────────────────────────────────────────────────────────

// THE STATUS LINE IS THE LAST ROW, and it carries every segment. It used to be
// the first row, two feet from everything that moves.
func TestTheStatusLineIsTheLastRowAndCarriesEverySegment(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-4.1-mini", usage: session.Usage{CostUSD: 0.14}}
	a := newApp(t.Context(), Options{
		Agent: agent, Workspace: "/tmp/lab", ContextWindow: 10_000,
		History: nil,
	})
	a.width, a.height = 90, 20
	a.pal = newPalette(tokens.ANSI256, false)
	// This conversation is empty, so the welcome box is up (welcome.go) and it
	// names the model on purpose. It is not what this test is about, and the
	// "nothing above the conversation says this" check below is about the top
	// bar that used to be there — so the box is dismissed the way a keystroke
	// would dismiss it.
	a.dismissWelcome()
	a.title = "porting the parser"
	a.cost = 0.14
	a.ctxTokens = 1000
	a.touch()

	lines := strings.Split(plain(frame(a)), "\n")
	last := lines[len(lines)-1]
	// THE TWO CLUSTERS, on the one row a ninety-column frame keeps them on:
	// identity left (the name and the model's BASENAME — the vendor is a routing
	// address, and it stays in the picker), telemetry right, state word last.
	// The product name is no longer on this line at all.
	for _, want := range []string{"porting the parser", "gpt-4.1-mini", "$0.14", "1k/10k · 10%", "idle"} {
		if !strings.Contains(last, want) {
			t.Fatalf("the status line is missing %q:\n%q", want, last)
		}
	}
	if strings.Contains(last, product) {
		t.Fatalf("the product name is still on the status line: %q", last)
	}
	if strings.Contains(last, "openai/") {
		t.Fatalf("the vendor prefix is still on the status line: %q", last)
	}
	// NO TOP BAR. Nothing above the conversation says any of this.
	for _, line := range lines[:len(lines)-1] {
		if strings.Contains(line, "openai/gpt-4.1-mini") && !strings.Contains(line, "model ·") {
			t.Fatalf("the model is still drawn above the conversation: %q", line)
		}
	}
	if strings.Contains(lines[0], "$0.14") {
		t.Fatalf("the top row is still a status bar: %q", lines[0])
	}

	// The four states, painted where the state is.
	if !strings.Contains(a.status(a.width), a.pal.dim("idle")) {
		t.Fatal("idle is not dim")
	}
	a.state = stateWorking
	if !strings.Contains(a.status(a.width), a.pal.accent("working")) {
		t.Fatal("working is not the accent")
	}
	a.state = stateInterrupted
	if !strings.Contains(a.status(a.width), a.pal.bad("interrupted")) {
		t.Fatal("interrupted is not the soft red")
	}
}

// The rule sits between the conversation and the input, and the draft is inset
// one cell under it with one blank row above.
func TestTheInputAreaSitsUnderARuleWithItsOwnBreathingRoom(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	// Named, because the border's label is the conversation's name and an
	// unnamed session in a directory with no repository has nothing to put in it
	// — which is the emptiness law, and is its own test (bundle_test.go).
	a.title = "trimming the parser"
	typeInto(t, a, "half a sentence")

	lines := strings.Split(plain(frame(a)), "\n")
	draft := -1
	for i, line := range lines {
		if strings.Contains(line, "half a sentence") {
			draft = i
		}
	}
	if draft < 0 {
		t.Fatalf("the draft is not on screen:\n%s", strings.Join(lines, "\n"))
	}
	if got := lines[draft]; !strings.HasPrefix(got, inputPad+prompt) {
		t.Fatalf("the draft is not inset one cell behind its prompt: %q", got)
	}
	if strings.TrimSpace(lines[draft-1]) != "" {
		t.Fatalf("the row above the draft is not blank: %q", lines[draft-1])
	}
	// The rule above the box is the LEGEND (render.go): the same one line, with
	// whatever that border has to say written into it.
	//
	// IT IS ASKED FOR BY IDENTITY RATHER THAN BY SHAPE. The legend's left end is
	// the branch and the machine now, not the conversation's name — the status
	// line below owns that — so in a lab with neither, at a width under
	// [hudTight], the border is honestly a bare rule and a test that demanded
	// `─ ` would be demanding a label the emptiness law forbids.
	if rule := lines[draft-2]; rule != plain(a.legend(a.width)) {
		t.Fatalf("the row above that is not the input's legend border: %q", rule)
	}
	if draft != len(lines)-2 {
		t.Fatalf("the draft is %d rows from the bottom, want 1 (the status line)", len(lines)-1-draft)
	}
	// The caret is in the box, one cell right of where it used to be.
	_, caretX, caretY := a.frame()
	if caretY != draft {
		t.Fatalf("the caret is on row %d, want the draft's row %d", caretY, draft)
	}
	if caretX != len(inputPad)+ansi.StringWidth(prompt)+len("half a sentence") {
		t.Fatalf("the caret is at column %d", caretX)
	}
}
