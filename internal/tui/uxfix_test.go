package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ── defects #3/#4 · BUG C: escape-blind truncation ──────────────────────────

// Styled text must truncate by display cells, cutting between escape
// sequences: the visible text is exactly the plain-text truncation, styles
// keep their boundaries, and no dangling escape can eat the next line.
func TestTruncateIsAnsiSafeAcrossStyleBoundaries(t *testing.T) {
	styled := "\x1b[1mcontracts\x1b[0m\x1b[2m · 5/5 · 12s\x1b[0m"
	plain := "contracts · 5/5 · 12s"
	if got := ansi.Strip(styled); got != plain {
		t.Fatalf("test fixture is wrong: %q", got)
	}
	for width := 1; width <= lipgloss.Width(plain)+2; width++ {
		out := truncate(styled, width)
		if lipgloss.Width(out) > width {
			t.Fatalf("width %d: truncated width %d exceeds budget: %q", width, lipgloss.Width(out), out)
		}
		if got, want := ansi.Strip(out), truncate(plain, width); got != want {
			t.Fatalf("width %d: styled truncation shows %q, plain shows %q", width, got, want)
		}
	}
}

// An OSC8 hyperlink cut mid-URL must keep its terminator: a dangling link
// sequence would swallow the leading characters of every following row.
func TestTruncateNeverLeavesDanglingOSC8Link(t *testing.T) {
	link := pathLink("/repo/internal/very/long/path/to/the/file.go", 60)
	for width := 2; width <= lipgloss.Width(ansi.Strip(link)); width++ {
		out := truncate(link, width)
		if lipgloss.Width(out) > width {
			t.Fatalf("width %d: link truncated to %d cells: %q", width, lipgloss.Width(out), out)
		}
		if opens := strings.Count(out, "\x1b]8;;"); opens%2 != 0 {
			t.Fatalf("width %d: unterminated OSC8 sequence (%d markers): %q", width, opens, out)
		}
		if stripped := ansi.Strip(out); strings.ContainsRune(stripped, 0x1b) {
			t.Fatalf("width %d: escape bytes leaked into visible text: %q", width, stripped)
		}
	}
}

// ── BUG B: pane blocks must never outgrow the pane ──────────────────────────

func TestChatPaneClampsPathologicalLinesToPaneSize(t *testing.T) {
	model := New(&fakeBackend{}, "clamp")
	model.setSize(60, 16)
	// A pathological line: an OSC8-linked path far wider than the pane, the
	// kind of content that previously soft-wrapped inside the Width style and
	// grew the frame taller than the terminal.
	long := pathLink("/x/"+strings.Repeat("segment/", 60)+"file.go", 400)
	model.chat.SetContent(long + "\n" + strings.Repeat("y", 500))

	pane := model.renderChatPane()
	if got := lipgloss.Height(pane); got != model.chatHeight {
		t.Fatalf("chat pane is %d rows, want exactly %d", got, model.chatHeight)
	}
	for index, line := range strings.Split(pane, "\n") {
		if width := lipgloss.Width(line); width > model.chatWidth {
			t.Fatalf("pane line %d is %d cells, pane is %d:\n%q", index, width, model.chatWidth, line)
		}
	}
	if view := model.View(); lipgloss.Height(view) != model.height {
		t.Fatalf("frame is %d rows, want %d", lipgloss.Height(view), model.height)
	}
}

// ── defect #1 · plan progress never stacks in the thread ────────────────────

func progressPoll(command store.Command, pending bool, messages []store.Message) pollResultMsg {
	result := pollResultMsg{
		messages: messages,
		snapshot: store.Snapshot{Nodes: []store.Node{{ID: store.RootID}}},
		cardSnapshot: store.Snapshot{
			Nodes: []store.Node{{ID: store.RootID}},
		},
		commands: []store.Command{command},
	}
	if pending {
		result.pending = []store.Command{command}
	}
	return result
}

func TestPlanProgressMutatesTheCardAndNeverTheStream(t *testing.T) {
	model := New(&fakeBackend{}, "cards")
	model.setSize(90, 24)
	command := store.Command{
		Seq: 9, SessionID: "cards", Kind: store.CommandSplice,
		Instruction: "build the plan", Status: store.CommandPending, Time: time.Now(),
	}
	progress := []store.Message{
		{Seq: 11, SessionID: "cards", Role: store.RoleSystem, NodeID: "task-9", CommandSeq: 9,
			Body: "grounding: settling what to look at", Time: time.Now()},
		{Seq: 12, SessionID: "cards", Role: store.RoleSystem, NodeID: "task-9", CommandSeq: 9,
			Body: "spine: sample 1/3", Time: time.Now()},
		{Seq: 13, SessionID: "cards", Role: store.RoleSystem, NodeID: "task-9", CommandSeq: 9,
			Body: "spine: sample 2/3", Time: time.Now()},
	}
	model.applyPoll(progressPoll(command, true, progress))

	thread := model.renderMessages()
	// Superseded lines and node chips never appear; the CURRENT line may
	// appear exactly once — as the shimmer, not as a stacked bubble.
	for _, hidden := range []string{"grounding:", "spine: sample 1/3", "↳ task-9"} {
		if strings.Contains(thread, hidden) {
			t.Fatalf("plan progress stacked into the thread:\n%s", thread)
		}
	}
	if count := strings.Count(thread, "exploring approaches · 2 of 3"); count > 1 {
		t.Fatalf("current planning line rendered %d times, want at most the shimmer:\n%s", count, thread)
	}
	if model.newMessages != 0 {
		t.Fatalf("plan progress claimed attention: %d new messages", model.newMessages)
	}
	card := model.cardByID("command:9")
	if card == nil || card.State != cardCompiling {
		t.Fatalf("compiling card missing: %#v", model.cards)
	}
	if card.Latest != "exploring approaches · 2 of 3" || len(card.Narration) != 0 {
		t.Fatalf("card did not carry progress as live status: latest=%q narration=%v", card.Latest, card.Narration)
	}
	if dock := model.renderActivityBar(); !strings.Contains(dock, "compiling") ||
		!strings.Contains(dock, "exploring approaches 2 of 3") {
		t.Fatalf("compiling card does not show the live planning line: %s", dock)
	}
	if shimmer := model.renderShimmerLines(90); !strings.Contains(shimmer, "exploring approaches · 2 of 3") {
		t.Fatalf("hidden-rail shimmer does not carry the current line:\n%s", shimmer)
	}

	// The poll race window: the command already resolved, the subtree not yet
	// visible. Progress still never becomes a stream block.
	command.Status = store.CommandApplied
	raceMessage := store.Message{
		Seq: 14, SessionID: "cards", Role: store.RoleSystem, NodeID: "task-9", CommandSeq: 9,
		Body: "contracts 5/5", Time: time.Now(),
	}
	model.applyPoll(progressPoll(command, false, []store.Message{raceMessage}))
	if thread := model.renderMessages(); strings.Contains(thread, "↳ task-9") ||
		strings.Count(thread, "contracts 5/5") > 1 {
		t.Fatalf("race-window progress leaked into the thread as a block:\n%s", thread)
	}

	// After the splice lands and the job settles, progress stays card state:
	// the settled card shows the deliverable, never the planning lines.
	settled := progressPoll(command, false, []store.Message{{
		Seq: 20, SessionID: "cards", Role: store.RoleSystem, NodeID: "task-9",
		Body: "The plan ran to completion.", Time: time.Now().Add(time.Minute),
	}})
	settled.cardSnapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{
			ID: "task-9", Parent: store.RootID, Status: store.Done, CreatedSeq: 15,
			FinishedAt: time.Now(), Summary: "The plan ran to completion.",
			Provenance: store.Provenance{SessionID: "cards", Intent: "build the plan"},
		},
	}}
	model.applyPoll(settled)
	card = model.cardByID("task-9")
	if card == nil || card.State != cardSettled {
		t.Fatalf("job did not settle into its card: %#v", model.cards)
	}
	thread = model.renderMessages()
	if !strings.Contains(thread, "The plan ran to completion.") {
		t.Fatalf("settled card lost its deliverable:\n%s", thread)
	}
	for _, hidden := range []string{"grounding:", "spine:", "contracts 5/5"} {
		if strings.Contains(thread, hidden) {
			t.Fatalf("settled thread still shows planning lines:\n%s", thread)
		}
	}
}

func TestCompilingCardMaterializesThreeLatestTitlesInPlace(t *testing.T) {
	model := New(&fakeBackend{}, "cards")
	model.setSize(100, 28)
	command := store.Command{
		Seq: 44, SessionID: "cards", Kind: store.CommandSplice,
		Instruction: "materialize the plan", Status: store.CommandPending, Time: time.Now(),
	}
	titles := []string{"Read source material", "Compare approaches", "Write recommendation", "Check citations"}
	messages := make([]store.Message, 0, len(titles))
	for index, title := range titles {
		messages = append(messages, store.Message{
			Seq: int64(45 + index), SessionID: "cards", Role: store.RoleSystem,
			NodeID: "task-44", CommandSeq: 44, Time: time.Now(),
			Body: fmt.Sprintf("writing the plan · %d of 4", index+1),
			Progress: &store.MessageProgress{
				Phase: "writing the plan", Done: index + 1, Total: 4, Latest: title,
			},
		})
	}
	model.applyPoll(progressPoll(command, true, messages))
	card := model.cardByID("command:44")
	if card == nil {
		t.Fatal("compiling card missing")
	}
	want := titles[1:]
	if !reflect.DeepEqual(card.StepTitles, want) {
		t.Fatalf("materialized titles = %#v, want %#v", card.StepTitles, want)
	}
	if len(model.cards) != 1 {
		t.Fatalf("progress created %d cards instead of updating one in place", len(model.cards))
	}
	dock := ansi.Strip(model.renderActivityBar())
	if !strings.Contains(dock, "writing the plan · 4 of 4") {
		t.Fatalf("compiling card missing its primary count line:\n%s", dock)
	}
	if strings.Contains(dock, titles[0]) {
		t.Fatalf("oldest title did not roll away:\n%s", dock)
	}
	for _, title := range want {
		if !strings.Contains(dock, title) {
			t.Fatalf("materialized card missing %q:\n%s", title, dock)
		}
	}
	if thread := ansi.Strip(model.renderMessages()); strings.Contains(thread, titles[0]) || len(model.cards) != 1 {
		t.Fatalf("progress grew a thread stack:\n%s", thread)
	}
}

func TestCompilingCardNeverRendersInternalPlannerVocabulary(t *testing.T) {
	model := New(&fakeBackend{}, "guard")
	model.setSize(100, 24)
	command := store.Command{
		Seq: 51, SessionID: "guard", Kind: store.CommandSplice,
		Instruction: "guard the card", Status: store.CommandPending, Time: time.Now(),
	}
	internal := []string{
		"grounding: settling what to look at",
		"spine: sample 1/3",
		"fan-out: 11 nodes",
		"ensemble: deciding whether to split",
	}
	messages := make([]store.Message, 0, len(internal))
	for index, body := range internal {
		messages = append(messages, store.Message{
			Seq: int64(52 + index), SessionID: "guard", Role: store.RoleSystem,
			NodeID: "task-51", CommandSeq: 51, Body: body, Time: time.Now(),
		})
	}
	model.applyPoll(progressPoll(command, true, messages))
	rendered := strings.ToLower(ansi.Strip(model.renderActivityBar() + "\n" + model.renderMessages()))
	for _, forbidden := range []string{"spine", "fan-out", "ensemble", "grounding"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("internal word %q reached the rendered card:\n%s", forbidden, rendered)
		}
	}
}

func TestCompilingCardHeaderRendersUserCount(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	card := jobCard{
		State: cardCompiling, CompilePhase: "setting working standards",
		CompileDone: 3, CompileTotal: 18, StartedAt: now.Add(-time.Minute - 7*time.Second),
	}
	if got, want := (&Model{}).cardMeta(card, now), "compiling · setting standards 3 of 18 · 1m 07s"; got != want {
		t.Fatalf("compile header = %q, want %q", got, want)
	}
}

// ── defect #2 · the settle is calm: no compaction jump ──────────────────────

func TestSettleWithManyProgressMessagesHoldsTheScrollAnchor(t *testing.T) {
	model := New(&fakeBackend{}, "cards")
	model.setSize(80, 14)
	base := time.Now().Add(-2 * time.Hour)
	filler := make([]store.Message, 0, 24)
	for index := 1; index <= 24; index++ {
		filler = append(filler, store.Message{
			Seq: int64(index), Time: base.Add(time.Duration(index) * 4 * time.Minute),
			SessionID: "cards", Role: store.RoleAgent, Body: fmt.Sprintf("update number %02d", index),
		})
	}
	command := store.Command{
		Seq: 30, SessionID: "cards", Kind: store.CommandSplice,
		Instruction: "narrate the podcast", Status: store.CommandPending, Time: time.Now(),
	}
	poll := progressPoll(command, true, filler)
	model.applyPoll(poll)
	progress := make([]store.Message, 0, 12)
	for index := 0; index < 12; index++ {
		progress = append(progress, store.Message{
			Seq: int64(31 + index), SessionID: "cards", Role: store.RoleSystem,
			NodeID: "task-30", CommandSeq: 30, Time: time.Now(),
			Body: fmt.Sprintf("briefs %d/12", index+1),
		})
	}
	model.applyPoll(progressPoll(command, true, progress))
	if thread := model.renderMessages(); strings.Contains(thread, "briefs 5/12") {
		t.Fatalf("progress stacked before the settle:\n%s", thread)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if model.autoScroll {
		t.Fatal("paging up should release auto-follow")
	}
	topBefore := strings.Split(model.chat.View(), "\n")[0]

	command.Status = store.CommandApplied
	settle := progressPoll(command, false, []store.Message{{
		Seq: 43, SessionID: "cards", Role: store.RoleSystem, NodeID: "task-30",
		Body: "The narration landed.", Time: time.Now().Add(time.Minute),
	}})
	settle.cardSnapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{
			ID: "task-30", Parent: store.RootID, Status: store.Done, CreatedSeq: 40,
			FinishedAt: time.Now(), Summary: "The narration landed.",
			Provenance: store.Provenance{SessionID: "cards", Intent: "narrate the podcast"},
		},
	}}
	model.applyPoll(settle)

	if card := model.cardByID("task-30"); card == nil || card.State != cardSettled {
		t.Fatalf("job did not settle: %#v", model.cards)
	}
	if model.autoScroll {
		t.Fatal("the settle re-pinned a reader who had scrolled up")
	}
	topAfter := strings.Split(model.chat.View(), "\n")[0]
	if topAfter != topBefore {
		t.Fatalf("the settle moved the reader's anchor:\nbefore: %q\nafter:  %q", topBefore, topAfter)
	}
}

// ── defect #5 · the shimmer sweeps left→right, briskly, with a soft edge ────

func TestShimmerSweepMovesLeftToRightAndCompletesInBudget(t *testing.T) {
	const cells = 60
	previous := sweepCenter(0, cells)
	lastWrap := 0
	for frame := 1; frame <= 45; frame++ {
		next := sweepCenter(frame, cells)
		if next < previous {
			period := frame - lastWrap
			// 120ms ticks: 10–15 frames is a 1.2–1.8s full sweep.
			if lastWrap > 0 && (period < 10 || period > 15) {
				t.Fatalf("full sweep took %d frames, want 10–15", period)
			}
			lastWrap = frame
		} else if next == previous {
			t.Fatalf("sweep stalled at frame %d (center %d)", frame, next)
		}
		previous = next
	}
	if lastWrap == 0 {
		t.Fatal("sweep never completed a pass")
	}
	// The band eases: shoulders wider than the core, distinct styles.
	if sweepSoftRadius <= sweepCoreRadius {
		t.Fatal("soft shoulder must extend beyond the core for a soft edge")
	}
	if line := (&Model{shimmerFrame: 3}).matteSweep("compiling the plan"); line == "" {
		t.Fatal("sweep rendered nothing")
	}
}

// ── defect #6 · two voices, one accent ──────────────────────────────────────

func TestSpeakerVoicesAreDifferentiatedWithinThePalette(t *testing.T) {
	agent := store.Message{Role: store.RoleAgent, Body: "Here is the answer."}
	user := store.Message{Role: store.RoleUser, Body: "Please look into it."}
	if accent, label := messagePresentation(agent); accent != lavender || label != "aforge" {
		t.Fatalf("aforge presentation = %v %q, want lavender accent", accent, label)
	}
	if accent, label := messagePresentation(user); accent != muted || label != "you" {
		t.Fatalf("you presentation = %v %q, want muted", accent, label)
	}
	if aforgeLabelStyle.GetForeground() != lavender {
		t.Fatal("aforge label must carry the lavender accent")
	}
	if youLabelStyle.GetForeground() != muted || !youLabelStyle.GetFaint() {
		t.Fatal("you label must be muted and faint")
	}
	if youTextStyle.GetForeground() != ink || !youTextStyle.GetFaint() {
		t.Fatal("you body must be softened ink")
	}
}

// ── defect #7 · tool-call block anatomy in the flight recorder ──────────────

func TestToolCallBlockSeparatesCallLineFromGutteredOutput(t *testing.T) {
	call := toolCallBlock(`sh {"cmd":"go test ./..."}`, 80)
	if len(call.brief) < 2 || call.brief[0] != "" {
		t.Fatalf("call block must lead with a breathing line: %#v", call.brief)
	}
	line := strings.Join(call.brief, "\n")
	if !strings.Contains(line, "$ sh") || !strings.Contains(line, "go test ./...") {
		t.Fatalf("call line lost its glyph+name+command anatomy: %q", line)
	}

	ok := toolResultBlock("1438B: total 3984⏎drwx------", 80)
	okText := strings.Join(ok.brief, "\n")
	if !strings.Contains(okText, "│ ") || !strings.Contains(okText, "1.4KB") {
		t.Fatalf("output is not guttered under the call: %q", okText)
	}
	if strings.Contains(okText, "✗") {
		t.Fatalf("success grew a failure marker: %q", okText)
	}

	failed := toolResultBlock("902B ERROR: exit status 2⏎go: build failed", 80)
	failedText := strings.Join(failed.brief, "\n")
	if !strings.Contains(failedText, "✗") || !strings.Contains(failedText, "exit status 2") {
		t.Fatalf("failure lost its ✗ marker or detail: %q", failedText)
	}

	// A collapsed long output ends in the truncation glyph and expands whole.
	long := toolResultBlock("9000B: "+strings.Repeat("word ", 200), 40)
	if !long.expandable() || !strings.Contains(strings.Join(long.brief, "\n"), "⋯") {
		t.Fatalf("long output did not collapse behind ⋯: %#v", long.brief)
	}
}

func TestToolCallLineTruncatesAnsiSafely(t *testing.T) {
	command := strings.Repeat("go test ./internal/tui -run Everything ", 8)
	block := toolCallBlock(`sh {"cmd":"`+command+`"}`, 40)
	for _, line := range block.brief {
		if width := lipgloss.Width(line); width > 40 {
			t.Fatalf("call line is %d cells, budget 40: %q", width, line)
		}
		if stripped := ansi.Strip(line); strings.ContainsRune(stripped, 0x1b) {
			t.Fatalf("escape bytes leaked into visible text: %q", stripped)
		}
	}
}

// ── defect #8 · one affordance grammar, no legacy hint strings ──────────────

func TestAffordanceGrammarReplacesLegacyHints(t *testing.T) {
	model := New(&fakeBackend{}, "grammar")
	model.setSize(90, 40)
	delivery := store.Message{Seq: 8, Role: store.RoleSystem, Body: "Landed.\nDetail."}
	model.messages = []store.Message{
		{Seq: 1, Time: time.Now(), Role: store.RoleSystem, CommandSeq: 42,
			Body: "Read the request.\nAssumed: defaults hold."},
		{Seq: 2, Time: time.Now(), Role: store.RoleAgent,
			Body: strings.Repeat("A line of the long answer.\n\n", 24)},
	}
	model.cards = []jobCard{
		{ID: "active", RootID: "active", State: cardWorking, Title: "Docked work", Total: 2},
		{ID: "landed", RootID: "landed", State: cardSettled, Title: "Settled work",
			BirthSeq: 5, Outcome: "Landed.", Deliverable: &delivery},
	}
	model.refreshChat()

	surfaces := model.View() + "\n" + model.renderMessages() + "\n" + model.renderActivityBar()
	for _, legacy := range []string{"v to expand", "v to collapse", "click to expand", "click for details", "tab or click"} {
		if strings.Contains(surfaces, legacy) {
			t.Fatalf("legacy affordance hint %q survived:\n%s", legacy, surfaces)
		}
	}
	for _, expected := range []string{"▸ Read the request. · 1 assumption", "▸ 35 more lines", "▸ details", " ▸"} {
		if !strings.Contains(surfaces, expected) {
			t.Fatalf("grammar affordance %q missing:\n%s", expected, surfaces)
		}
	}

	// Glyphs flip with state.
	model.receiptsExpanded = true
	model.expandedMessages[2] = true
	model.cardExpanded["landed"] = true
	flipped := model.renderMessages()
	for _, expected := range []string{"▾ Read the request. · 1 assumption", "▾ collapse", "⟨×⟩ close · ▸ job graph"} {
		if !strings.Contains(flipped, expected) {
			t.Fatalf("expanded state did not flip its glyph to %q:\n%s", expected, flipped)
		}
	}
}

func TestRailHistoryUsesGrammarGlyphs(t *testing.T) {
	model := New(&fakeBackend{}, "history")
	nodes := []store.Node{{ID: store.RootID}}
	for index := 1; index <= 7; index++ {
		nodes = append(nodes, store.Node{
			ID: fmt.Sprintf("job-%d", index), Parent: store.RootID,
			Title: fmt.Sprintf("job %d", index), Status: store.Done, CreatedSeq: int64(index),
		})
	}
	model.snapshot = store.Snapshot{Nodes: nodes}
	if tree := model.renderTree(60, 0); !strings.Contains(tree, "▸ history (2)") {
		t.Fatalf("collapsed history is missing its ▸ affordance:\n%s", tree)
	}
	model.historyExpanded = true
	if tree := model.renderTree(60, 0); !strings.Contains(tree, "▾ history (2)") {
		t.Fatalf("expanded history did not flip to ▾:\n%s", tree)
	}
}

// ── defect #9 · every journey has key, click, and traversal paths ───────────

func TestHeaderTasksButtonTogglesTheRail(t *testing.T) {
	model := New(&fakeBackend{}, "header")
	model.setSize(100, 30)
	view := model.View()
	if !strings.Contains(view, "⟨tasks ▸⟩") {
		t.Fatalf("header button missing:\n%s", view)
	}
	if model.headerTasksBounds.width == 0 {
		t.Fatal("header button registered no click bounds")
	}
	_, _ = model.Update(tea.MouseMsg{
		X: model.headerTasksBounds.x + 1, Y: model.headerTasksBounds.y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if !model.graphOpen {
		t.Fatal("clicking the header button did not open the rail")
	}
	if view := model.View(); !strings.Contains(view, "⟨tasks ▾⟩") {
		t.Fatalf("open rail did not flip the button glyph:\n%s", view)
	}
	_, _ = model.Update(tea.MouseMsg{
		X: model.headerTasksBounds.x + 1, Y: model.headerTasksBounds.y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if model.graphOpen {
		t.Fatal("clicking the header button again did not close the rail")
	}
}

func TestDockRendersItsThreeStatesWithExactFrameHeight(t *testing.T) {
	model := New(&fakeBackend{}, "dock")
	model.setSize(90, 24)

	// Zero active: no dock at all.
	if height := model.cardDockHeight(); height != 0 {
		t.Fatalf("idle dock is %d rows, want 0", height)
	}
	if view := model.View(); lipgloss.Height(view) != 24 || strings.Contains(view, "no tasks in flight") {
		t.Fatalf("idle frame is %d rows or shows dock chrome", lipgloss.Height(view))
	}

	// Small count: cards render directly, no summary line.
	model.cards = []jobCard{
		{ID: "a", State: cardWorking, Title: "First job"},
		{ID: "b", State: cardWorking, Title: "Second job"},
	}
	model.setSize(90, 24)
	dock := model.renderActivityBar()
	if strings.Contains(dock, "running") || !strings.Contains(dock, "First job") {
		t.Fatalf("small dock should show cards directly: %s", dock)
	}
	if view := model.View(); lipgloss.Height(view) != 24 {
		t.Fatalf("small-dock frame is %d rows, want 24", lipgloss.Height(view))
	}

	// Overflow: a one-line ▸ summary; click expands into a capped list with a
	// ▾ header; esc collapses back and returns to the input.
	for index := 0; index < 4; index++ {
		model.cards = append(model.cards, jobCard{
			ID: fmt.Sprintf("extra-%d", index), State: cardWorking, Title: fmt.Sprintf("Extra %d", index),
		})
	}
	model.cards = append(model.cards, jobCard{ID: "q", State: cardQuestion, Title: "Needs an answer", Question: "Which?"})
	model.setSize(90, 24)
	_ = model.View()
	dock = model.renderActivityBar()
	if !strings.Contains(dock, "▸ 6 running · 1 waiting ⚑") || lipgloss.Height(dock) != 1 {
		t.Fatalf("overflow dock should be a one-line summary: %q", dock)
	}
	if view := model.View(); lipgloss.Height(view) != 24 {
		t.Fatalf("summary frame is %d rows, want 24", lipgloss.Height(view))
	}

	_, _ = model.Update(tea.MouseMsg{
		X: model.activityBarBounds.x + 1, Y: model.activityBarBounds.y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if !model.dockExpanded || model.focus != focusCards {
		t.Fatalf("clicking the summary did not expand the dock: expanded=%v focus=%v", model.dockExpanded, model.focus)
	}
	expanded := model.renderActivityBar()
	if !strings.Contains(expanded, "▾ 6 running") || !strings.Contains(expanded, "Extra 2") {
		t.Fatalf("expanded dock is missing its header or cards:\n%s", expanded)
	}
	if height := lipgloss.Height(expanded); height > max(4, 24*2/5) {
		t.Fatalf("expanded overflow dock is %d rows, cap is %d", height, max(4, 24*2/5))
	}
	if view := model.View(); lipgloss.Height(view) != 24 {
		t.Fatalf("expanded frame is %d rows, want 24", lipgloss.Height(view))
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.dockExpanded || model.focus != focusInput {
		t.Fatalf("esc did not collapse the dock back to input: expanded=%v focus=%v", model.dockExpanded, model.focus)
	}
	if dock := model.renderActivityBar(); !strings.Contains(dock, "▸ 6 running") {
		t.Fatalf("collapsed dock lost its summary: %q", dock)
	}
}

func TestFocusTraversalWalksThreadElementsAndEnterEqualsClick(t *testing.T) {
	model := New(&fakeBackend{}, "traversal")
	model.setSize(90, 40)
	model.messages = []store.Message{
		{Seq: 1, Time: time.Now(), Role: store.RoleSystem, CommandSeq: 7,
			Body: "Read the request.\nAssumed: defaults hold."},
		{Seq: 2, Time: time.Now(), Role: store.RoleAgent,
			Body: strings.Repeat("A long answer line.\n\n", 24)},
	}
	model.cards = []jobCard{{ID: "job", State: cardWorking, Title: "Working job"}}
	model.setSize(90, 40)

	// Zone order: input → dock → thread.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if model.focus != focusCards {
		t.Fatalf("first tab focused %v, want the dock", model.focus)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if model.focus != focusChat {
		t.Fatalf("second tab focused %v, want the thread", model.focus)
	}

	_ = model.renderMessages()
	targets := model.chatFocusLines()
	if len(targets) < 2 {
		t.Fatalf("thread should expose at least the receipt and the fold: %v", targets)
	}
	if model.chatFocusIndex < len(targets)-1 {
		model.chatFocusIndex = len(targets) - 1
	}
	// Walk to the first target: the receipt line.
	for index := 0; index < len(targets)+2; index++ {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	if model.chatFocusIndex != 0 {
		t.Fatalf("up-arrow walk did not reach the first element: index %d", model.chatFocusIndex)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.receiptsExpanded {
		t.Fatal("enter on the focused receipt did not expand receipts (enter != click)")
	}

	// Down to the fold and open it.
	_ = model.renderMessages()
	targets = model.chatFocusLines()
	foldAt := -1
	for index, line := range targets {
		for _, row := range model.chatExpandRows {
			if row.line == line && row.action == chatExpandMessage {
				foldAt = index
			}
		}
	}
	if foldAt < 0 {
		t.Fatalf("fold target disappeared: %v", targets)
	}
	for model.chatFocusIndex < foldAt {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.expandedMessages[2] {
		t.Fatal("enter on the focused fold did not expand the answer")
	}

	// Esc backs the zone out to the input.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.focus != focusInput || !model.inputFocused {
		t.Fatalf("esc did not return the thread zone to input: focus=%v", model.focus)
	}
}

// A long outcome used to end at a truncation marker inside a fixed-height
// header, and the rest of it existed only in chat. The feed scrolls, so the
// whole result lands there — reachable from the surface that produced it.
func TestALongOutcomeIsReadableInTheActivityFeed(t *testing.T) {
	model, _ := inspectedWorkerModel(t)
	model.setSize(90, 26)
	model.inspectedNode = store.Node{
		ID: "worker", Parent: store.RootID, Status: store.Done,
		Brief:   "Inspect this worker",
		Summary: strings.TrimSpace(strings.Repeat("a finding worth reading in full. ", 60)),
	}
	model.sizeNodeViewports()
	if !model.nodeDetailsClipped {
		t.Fatal("a 60-sentence outcome did not clip the header")
	}
	if !strings.Contains(ansi.Strip(model.nodeDetailsText), "in full at the end of the feed") {
		t.Fatalf("clipped header points nowhere:\n%s", ansi.Strip(model.nodeDetailsText))
	}
	feed := ansi.Strip(model.renderActivityFeed(88))
	if !strings.Contains(feed, "── outcome ──") ||
		strings.Count(feed, "a finding worth reading in full.") < 10 {
		t.Fatalf("feed does not carry the whole outcome:\n%s", feed)
	}
}
