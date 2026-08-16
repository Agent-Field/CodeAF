package chat

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// The dressing is tested through the frame, like everything else in this
// package: 13.1 item 2 is a claim about what a reader sees, and a claim about
// what a reader sees is only checkable where the rows come out.
//
// The law under all of it is 13.1 item 3's: what renders must remain a faithful
// PRESENTATION of what the journal says. So every test below asserts that the
// journal's own words survive alongside whatever dressing was added — a test
// that only checked for the glyph would pass on a renderer that had thrown the
// sentence away.

// fixedNow is the one instant every test in this package renders at, so a
// frame is a function of the journal and the width alone.
func fixedNow() time.Time { return time.Unix(1_700_000_000, 0) }

// dressedThread journals one of every row kind this surface knows how to dress,
// in the order a real session produces them.
func dressedThread(backend *fakeBackend) {
	backend.add(store.Message{
		SessionID: testSession, Role: store.RoleAgent,
		Body: "Three tasks settled while you were away.",
		Brief: &store.Brief{
			Done: 3, Failed: 1, Questions: 2, CostUSD: 8.65,
			Items: []store.BriefItem{
				{Body: "wisp-parity settled"},
				{Body: "navctx failed on its third worker"},
			},
		},
	})
	backend.add(store.Message{
		SessionID: testSession, Role: store.RoleUser,
		Body: "what is **the** plan for `navctx`?",
	})
	backend.add(store.Message{
		SessionID: testSession, Role: store.RoleSystem,
		Body: "· reflected — 3 facts, 1 skill\nfact: the parser is the bottleneck\nskill: bisecting a flaky test",
	})
	backend.add(store.Message{
		SessionID: testSession, Role: store.RoleSystem, CommandSeq: 4,
		Body: "rework NavCtx after the worker died",
	})
	backend.add(store.Message{
		SessionID: testSession, Role: store.RoleAgent,
		NodeID: "task/wisp-parity", Model: "anthropic/claude-k3",
		Body: "reworking NavCtx after the worker died",
	})
	backend.add(store.Message{
		SessionID: testSession, Role: store.RoleAgent, Model: "anthropic/claude-k3",
		Body: "Here is the **plan**:\n\n" +
			"- read `navctx.rs` end to end\n" +
			"- write the *failing* test first\n\n" +
			"```rust\nlet ctx = NavCtx::new();\n```\n\n" +
			"> and a note about the plan",
		Parts: []store.MessagePart{
			store.ArtifactRef(store.ArtifactPart{Path: "workspace/wisp/plan.md", Bytes: 1611}),
		},
	})
}

func dressedApp(t *testing.T) *App {
	t.Helper()
	backend := &fakeBackend{}
	dressedThread(backend)
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	return app
}

// whole is every row the transcript holds, not just the screenful: the tests
// below are about the dressing, and scrolling is a different law with its own
// tests.
func whole(t *testing.T, app *App, width int) string {
	t.Helper()
	var out strings.Builder
	for i := 0; i < app.transcript.Len(); i++ {
		for _, row := range app.transcript.Block(i).Rows(width) {
			out.WriteString(ansi.Strip(row))
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// 5.13's voice hierarchy, after §3b took the speaker rows out: the two parties
// of the conversation are told apart by tier and gutter rather than by name, and
// the guest that is neither of them keeps its own. The three voices have their
// own file (voices_test.go); what is checked here is that the words and the
// provenance rules around them survived the re-dress.
func TestRoleVoicesAreDistinctAndCalm(t *testing.T) {
	app := dressedApp(t)
	out := whole(t, app, 80)

	if !strings.Contains(out, tokens.GlyphPromptChat+" what is the plan") {
		t.Fatalf("the reader's own turn is not attributed:\n%s", out)
	}
	if !strings.Contains(out, "Here is the plan:") {
		t.Fatalf("the answer's own words are missing:\n%s", out)
	}
	// Nobody is named in a two-party conversation (§15).
	for _, label := range []string{tokens.GlyphPromptChat + " you", "aforge"} {
		if strings.Contains(out, label) {
			t.Fatalf("the speaker row %q came back:\n%s", label, out)
		}
	}
	// The vendor prefix is provenance, not identity, and never reaches a row.
	if strings.Contains(out, "anthropic/claude-k3") {
		t.Fatalf("the model's vendor prefix reached the transcript:\n%s", out)
	}
}

// 5.13's spacing rhythm. A wall of text with no air is what 13.1 opened on.
func TestEveryTurnIsSeparatedByABlankRow(t *testing.T) {
	app := dressedApp(t)
	for i := 0; i < app.transcript.Len(); i++ {
		rows := app.transcript.Block(i).Rows(80)
		if len(rows) == 0 || rows[len(rows)-1] != "" {
			t.Fatalf("block %d does not close with a blank row: %q", i, rows)
		}
	}
}

// Every voice's words start at the same left edge, whatever hangs in the gutter
// in front of them (§16's PADDING RHYTHM, and §3b's "indented to the shared
// edge").
func TestSpeechBodiesSitAtADepth(t *testing.T) {
	app := dressedApp(t)
	out := whole(t, app, 80)
	if !strings.Contains(out, "\n"+strings.Repeat(" ", bodyIndent)+"Here is the plan:") {
		t.Fatalf("the reply's body is not indented under its speaker:\n%s", out)
	}
}

// Markdown reaches the transcript, not just the renderer's own tests.
func TestMarkdownReachesTheTranscript(t *testing.T) {
	app := dressedApp(t)
	out := whole(t, app, 80)
	if strings.Contains(out, "**") {
		t.Fatalf("literal bold delimiters reached the transcript:\n%s", out)
	}
	if !strings.Contains(out, "• read navctx.rs end to end") {
		t.Fatalf("the bullet list did not render as a list:\n%s", out)
	}
}

// 13.1 item 2: a system receipt is a dim collapsed row, never a naked line —
// and it is expandable, or the fold would be a way of hiding the record.
func TestReceiptsCollapseAndExpand(t *testing.T) {
	app := dressedApp(t)

	collapsed := whole(t, app, 80)
	if !strings.Contains(collapsed, tokens.GlyphCollapsed+" · reflected — 3 facts, 1 skill") {
		t.Fatalf("the receipt did not render as a collapsed row:\n%s", collapsed)
	}
	if !strings.Contains(collapsed, "2 lines") {
		t.Fatalf("the fold does not say how much it is holding:\n%s", collapsed)
	}
	if strings.Contains(collapsed, "the parser is the bottleneck") {
		t.Fatalf("the receipt's detail was not folded:\n%s", collapsed)
	}

	app.key(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	expanded := whole(t, app, 80)
	if !strings.Contains(expanded, "the parser is the bottleneck") {
		t.Fatalf("the fold would not open:\n%s", expanded)
	}
	if !strings.Contains(expanded, tokens.GlyphExpanded) {
		t.Fatalf("an open fold still shows the collapsed affordance:\n%s", expanded)
	}

	app.key(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if reclosed := whole(t, app, 80); strings.Contains(reclosed, "the parser is the bottleneck") {
		t.Fatalf("the fold would not close again:\n%s", reclosed)
	}
}

// A fold is a change to bytes the cache has already committed, so it must move
// the block's version — 8.1.1's rule, and the thing Transcript.Strict catches.
func TestOpeningAFoldBumpsTheBlockVersion(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem, Body: "· reflected\ndetail"})
	app := newTestApp(backend, nil, nil)
	poll(t, app)

	block, ok := app.transcript.Block(0).(*messageBlock)
	if !ok {
		t.Fatalf("the receipt is not a message block: %T", app.transcript.Block(0))
	}
	before := block.Version()
	app.toggleReceipts()
	if block.Version() == before {
		t.Fatal("the fold opened without bumping the version the cache keys on")
	}
}

// A block with nothing folded must not offer a fold: the affordance never lies
// (5.20), and that includes lying by presence.
func TestAOneLineReceiptOffersNoFold(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem, Body: "· let go — one worker"})
	app := newTestApp(backend, nil, nil)
	poll(t, app)
	if out := whole(t, app, 80); strings.Contains(out, "lines") {
		t.Fatalf("a receipt with nothing hidden advertised a fold:\n%s", out)
	}
}

// 5.20 rule 1: the chat-or-work fork is THE ambiguity, and it must always be
// inked — as a row that cannot be mistaken for either speech or a receipt.
func TestCommissioningRowsAreDistinct(t *testing.T) {
	app := dressedApp(t)
	out := whole(t, app, 80)
	// A COMMISSIONING IS A CARD FROM THE MOMENT IT IS JOURNALED (user-amended
	// 2026-08-11). It used to be a dim `↦ commissioned: …` row until the graph
	// caught up, which left the thread showing nothing for the seconds a reader
	// is watching hardest — "once it decides we are creating task … there is
	// like a few seconds where nothing happens". The skeleton is the same block,
	// the same id and the same dress the named card wears, so what changes when
	// the board answers is the title and nothing else
	// ([messageBlock.dressCommitting]).
	if !strings.Contains(out, "rework NavCtx") {
		t.Fatalf("the dispatch lost the head's own reading:\n%s", out)
	}
	if !strings.Contains(out, creatingWord) {
		t.Fatalf("the dispatch does not say what is happening to it:\n%s", out)
	}
	// It is not speech and not a receipt: §14 keeps machinery vocabulary off
	// every surface, and the fork 5.20 rule 1 is about is inked by the card's
	// own shape rather than by a word naming a mechanism.
	for _, banned := range []string{"commissioned", tokens.GlyphCollapsed + " commissioned"} {
		if strings.Contains(out, banned) {
			t.Fatalf("machinery vocabulary reached the frame (%q):\n%s", banned, out)
		}
	}
}

// THREAD-UX: stream = conversation, card = work. A row anchored to a graph node
// is a job reporting and is drawn with 5.9's anatomy.
func TestWorkRowsBecomeCards(t *testing.T) {
	app := dressedApp(t)
	out := whole(t, app, 80)
	if !strings.Contains(out, tokens.GlyphWorking+" wisp-parity") {
		t.Fatalf("the node row did not become a card:\n%s", out)
	}
	// §20: the name is the title ROW and the status is the line under it at the
	// content edge — see TestAJobCardWearsItsNameAsATitleRow for why the chip
	// form could not hold a card.
	// The card stands inside its own gutter (§20 lets an accent edge live
	// there), so its content edge is the room's shifted by [cardLane] — the same
	// one depth in that blocks.CardBlock's own anatomy draws.
	if !strings.Contains(out, "\n"+strings.Repeat(" ", blocks.ContentEdge+cardLane)+"reworking NavCtx") {
		t.Fatalf("the card has no status line under its title:\n%s", out)
	}
	// 5.9: money is always visible, and 8.2.20: missing data renders —.
	if !strings.Contains(out, "$"+tokens.GlyphMissing) {
		t.Fatalf("the card's telemetry hides the money cell:\n%s", out)
	}
	// 5.14's never-shown tier: node ids and seqs are not information.
	if strings.Contains(out, "task/wisp-parity") {
		t.Fatalf("a raw node id reached a card:\n%s", out)
	}
}

// Two tasks must not share one accent, or the identity hue answers nothing.
func TestCardsWearTheirOwnIdentityAccent(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, NodeID: "task/alpha", Body: "one"})
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, NodeID: "task/beta", Body: "two"})
	app := New(Options{
		Backend: backend, Session: testSession, Profile: tokens.TrueColor,
		Now: fixedNow, PollEvery: 1, Root: "/tmp/room", Home: "/tmp",
	})
	poll(t, app)

	// The title row, not row zero: a card opens with one blank row of its own
	// ground (§16's padding rhythm), and two cards' grounds are identical by
	// design — it is the GLYPH that carries the identity.
	alpha, _ := app.transcript.Block(0).(*messageBlock)
	beta, _ := app.transcript.Block(1).(*messageBlock)
	first := alpha.Rows(60)[alpha.cardPad()]
	second := beta.Rows(60)[beta.cardPad()]
	if first == second {
		t.Fatalf("two tasks drew the same header: %q", first)
	}
	if ansi.Strip(first) == first {
		t.Fatalf("the card glyph carries no accent at all: %q", first)
	}
}

// 5.24: the arrival brief is the FIRST transcript block and is styled as one.
func TestArrivalBriefIsTheFirstBlockAndStyledAsOne(t *testing.T) {
	app := dressedApp(t)
	rows := app.transcript.Block(0).Rows(80)
	head := ansi.Strip(rows[0])
	if !strings.Contains(head, "while you were away") {
		t.Fatalf("the first block is not the arrival brief: %q", head)
	}
	for _, want := range []string{
		"[" + tokens.GlyphSettled + "3]",
		"[" + tokens.GlyphFailed + "1]",
		"[" + tokens.GlyphNeedsHuman + "2]",
		"$8.65",
	} {
		if !strings.Contains(head, want) {
			t.Fatalf("the brief's header is missing %q: %q", want, head)
		}
	}
	body := strings.Join(rows, "\n")
	if !strings.Contains(ansi.Strip(body), "wisp-parity settled") {
		t.Fatalf("the brief's items did not render:\n%s", ansi.Strip(body))
	}
	// The one hairline 5.13 permits, at the one boundary that earns it.
	if !strings.Contains(body, strings.Repeat("─", 10)) {
		t.Fatalf("the arrival is not closed by a rule:\n%s", ansi.Strip(body))
	}
}

// 12.5.1, restated after the re-dress: a deliverable is a row of its own, with
// its own glyph, and is never flattened into the sentence around it.
func TestArtifactsStaySeparateRowsAfterDressing(t *testing.T) {
	app := dressedApp(t)
	out := whole(t, app, 80)
	line := ""
	for _, row := range strings.Split(out, "\n") {
		if strings.Contains(row, "plan.md") {
			line = row
			break
		}
	}
	if line == "" {
		t.Fatalf("the deliverable lost its row:\n%s", out)
	}
	if !strings.Contains(line, tokens.GlyphCollapsed) {
		t.Fatalf("the deliverable is not drawn as a reference: %q", line)
	}
	if strings.Contains(line, "note about the plan") {
		t.Fatalf("the deliverable was flattened into the prose around it: %q", line)
	}
}

// 12.5.2 survives the re-dress, at every width, in both halves of the law: the
// header badge and the cut rule under the body.
func TestTruncationMarksSurviveTheDressing(t *testing.T) {
	cases := []struct {
		how  store.EndKind
		mark string
	}{
		{store.EndLength, "cut off — output cap"},
		{store.EndStreamDrop, "cut off — stream dropped"},
		{store.EndInterrupted, "stopped by you"},
	}
	for _, testCase := range cases {
		t.Run(testCase.mark, func(t *testing.T) {
			backend := &fakeBackend{}
			backend.add(store.Message{
				SessionID: testSession, Role: store.RoleAgent,
				Body:  "half an **answer**",
				Parts: []store.MessagePart{store.EndedMark(store.EndedPart{How: testCase.how})},
			})
			app := newTestApp(backend, nil, nil)
			poll(t, app)

			for _, width := range []int{40, 60, 80, 120} {
				out := whole(t, app, width)
				if !strings.Contains(out, "half an answer") {
					t.Fatalf("the words that did arrive are missing at width %d:\n%s", width, out)
				}
				if strings.Count(out, testCase.mark) < 2 {
					t.Fatalf("the cut is not marked in both halves of the law at width %d:\n%s",
						width, out)
				}
				rule := ansi.Strip(blocks.CutRule(endStateFor(testCase.how), width, blocks.Plain))
				if rule == "" || !strings.Contains(out, rule) {
					t.Fatalf("the visible cut rule is missing at width %d:\n%s", width, out)
				}
			}
		})
	}
}

// 10.5.22: the footer shortens, never wraps, and drops lowest-priority-first.
func TestStatusRowShortensAndNeverWraps(t *testing.T) {
	app := dressedApp(t)
	wide := app.status.Render(120, 1)
	narrow := app.status.Render(24, 1)

	if strings.ContainsAny(wide+narrow, "\n") {
		t.Fatal("the status row wrapped")
	}
	if blocks.Width(narrow) > 24 || blocks.Width(wide) > 120 {
		t.Fatalf("the status row overran its width: %d, %d", blocks.Width(narrow), blocks.Width(wide))
	}
	if blocks.Width(narrow) >= blocks.Width(wide) {
		t.Fatal("the status row did not shorten under width pressure")
	}
	// THE LAST THING STANDING IS WHERE YOU ARE (§7). The permanent `? help`
	// door this used to assert is not on the row any more — standing legends
	// moved to the `?` sheet — so what a narrowing terminal must never drop is
	// the place word.
	if !strings.Contains(ansi.Strip(narrow), "chat") {
		t.Fatalf("where-you-are was dropped before everything else: %q", narrow)
	}
}

// §7 ENDED THE TWO HOMES. 10.5.23 kept this-turn money off the bar row and put
// it on a strip of the composer's own; §7 dissolved that strip and the money
// came here — into the MIDDLE zone, where it stands beside `interrupt esc` for
// exactly as long as the turn does.
//
// The distinction the old split was protecting is still protected, and by a
// stronger mechanism than two rows: the day's total is a standing fact on the
// right and says the word `today`; the turn's cost is a live one in the middle
// and says nothing, because it is gone the moment the turn is.
func TestTurnMoneyIsLiveAndDayMoneyIsStanding(t *testing.T) {
	app := dressedApp(t)
	app.status.live = false
	app.status.cost, app.status.haveCost = 0.03, true
	app.status.spend, app.status.haveSpend = 1.42, true
	settled := ansi.Strip(app.status.Render(120, 1))
	if strings.Contains(settled, "$0.03") {
		t.Fatalf("a settled room still shows the turn's cost: %q", settled)
	}
	if !strings.Contains(settled, "$1.42 today") {
		t.Fatalf("the day total is not a standing fact: %q", settled)
	}

	app.status.live = true
	app.status.elapsed = 12 * time.Second
	live := ansi.Strip(app.status.Render(120, 1))
	if !strings.Contains(live, "$0.03") || !strings.Contains(live, "12s") {
		t.Fatalf("a live turn does not carry its own receipts: %q", live)
	}
	// And the two figures are never confused for one another: the day's carries
	// its word, the turn's does not.
	if strings.Contains(live, "$0.03 today") {
		t.Fatalf("the turn's cost is wearing the day's word: %q", live)
	}
}

// 5.9: money is the one cell that never leaves. Everything else on the bar's
// right zone goes before the day's total does.
func TestTheDayTotalIsTheLastFactToLeaveTheBar(t *testing.T) {
	app := dressedApp(t)
	app.status.spend, app.status.haveSpend = 1.42, true
	app.status.dir = "~/a/aforge-v2"
	app.status.used, app.status.window, app.status.haveUsage = 8000, 262144, true
	for width := 1; width <= 60; width++ {
		row := ansi.Strip(app.status.Render(width, 1))
		if blocks.Width(row) > width {
			t.Fatalf("the bar row overran width %d: %q", width, row)
		}
		// Once anything at all from the right zone survives, money is in it.
		for _, other := range []string{"aforge-v2", "of 262K", "%"} {
			if strings.Contains(row, other) && !strings.Contains(row, "$") {
				t.Fatalf("width %d kept %q and dropped the money: %q", width, other, row)
			}
		}
	}
}

// 5.20 rule 6 and 8.2.21, now on the footer as well as the awaiting line: the
// hint appears only when esc would in fact interrupt.
func TestFooterAdvertisesEscOnlyWhileItWouldInterrupt(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	// The chip is VERB FIRST, key after (§16's verb·key grammar), so the words
	// on the row are `interrupt esc` and not the other way round.
	const chip = "interrupt esc"
	if row := ansi.Strip(app.status.Render(120, 1)); strings.Contains(row, chip) {
		t.Fatalf("an idle footer advertised an interrupt: %q", row)
	}
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	if row := ansi.Strip(app.status.Render(120, 1)); !strings.Contains(row, chip) {
		t.Fatalf("a live turn's footer does not advertise the interrupt: %q", row)
	}
	stream(app, StreamEvent{Kind: StreamFinished, Session: testSession})
	if row := ansi.Strip(app.status.Render(120, 1)); strings.Contains(row, chip) {
		t.Fatalf("a settled turn's footer still advertised an interrupt: %q", row)
	}
}

// §7's middle zone holds only what is LIVE RIGHT NOW, so the standing bindings
// this test used to demand are deliberately absent: quit, newline and the
// receipts fold moved to the `?` sheet and the palette, which read the registry
// directly. What survives here is the rule that made the old assertion worth
// making — whatever the row DOES offer comes from the registry and is really
// bound on this surface, so a chip can never teach a chord that does nothing.
func TestFooterVerbsComeFromTheRegistryAndAreBound(t *testing.T) {
	app := dressedApp(t)
	row := ansi.Strip(app.status.Render(160, 1))
	for _, entry := range app.verbs {
		if entry.Key == "" {
			t.Fatalf("an unbound entry reached the row: %+v", entry)
		}
		if !strings.Contains(row, entry.Verb+" "+entry.Key) {
			t.Fatalf("the registry row %q is not on the row: %q", entry.ID, row)
		}
	}
	// v1's ctrl+j is not what this surface binds, so nothing may advertise it.
	if strings.Contains(row, "ctrl+j") {
		t.Fatalf("the row advertised a chord this surface does not bind: %q", row)
	}
}

// A store this surface cannot read is the one state 5.16 hands the whole row
// to: a coloured sentence means something is wrong.
func TestAFailedReadTakesTheWholeStatusRow(t *testing.T) {
	app := dressedApp(t)
	app.status.err = "database is locked"
	row := ansi.Strip(app.status.Render(120, 1))
	if !strings.Contains(row, "database is locked") {
		t.Fatalf("the failure is not on the row: %q", row)
	}
	if strings.Contains(row, "help") {
		t.Fatalf("the footer kept offering doors beside a dead store: %q", row)
	}
}

// 5.19: the composer's top edge names the ground this work lands on.
// §7 dissolved the place line as a ROW of this surface: the ground it named now
// rides the bar's right zone, in the same abbreviated form placeline renders,
// beside the model word and the day's total. The question 5.19 asked — where
// does this work land on disk — is still answered on screen; it is answered on
// one row instead of two.
func TestTheGroundIsNamedOnTheBarRow(t *testing.T) {
	app := New(Options{
		Backend: &fakeBackend{}, Session: testSession, Profile: tokens.NoColor,
		Now: fixedNow, PollEvery: 1, Root: "/home/someone/aforge-v2", Home: "/home/someone",
	})
	app.refresh()
	row := ansi.Strip(app.status.Render(120, 1))
	if !strings.Contains(row, "aforge-v2") {
		t.Fatalf("the bar row does not name the ground: %q", row)
	}
	// It is the ABBREVIATED form, tilde-folded — the place line's own grammar,
	// asked of the place line rather than respelled here.
	if !strings.Contains(row, "~/") {
		t.Fatalf("the ground is not in its abbreviated form: %q", row)
	}
	// And the composer above it is the draft and nothing else.
	region := strings.Split(app.composer.Render(60, 1), "\n")
	if len(region) != 1 {
		t.Fatalf("the composer region drew %d rows, want 1: %q", len(region), region)
	}
	if strings.Contains(region[0], "aforge-v2") {
		t.Fatalf("the place line is still a row of the composer: %q", region[0])
	}
	if !strings.Contains(region[0], tokens.GlyphPromptChat) {
		t.Fatalf("the draft row lost its prompt: %q", region[0])
	}
}

// The composer region degrades by dropping its edges, never its draft.
func TestTheComposerRegionKeepsItsDraftLast(t *testing.T) {
	app := New(Options{
		Backend: &fakeBackend{}, Session: testSession, Profile: tokens.NoColor,
		Now: fixedNow, PollEvery: 1, Root: "/home/someone/aforge-v2", Home: "/home/someone",
	})
	for height := 1; height <= 6; height++ {
		region := app.composer.Render(40, height)
		rows := strings.Split(region, "\n")
		if len(rows) > height {
			t.Fatalf("the region drew %d rows into %d: %q", len(rows), height, rows)
		}
		if !strings.Contains(region, tokens.GlyphPromptChat) {
			t.Fatalf("the draft was dropped at height %d: %q", height, region)
		}
	}
}

// The amber attention column is 5.16's most expensive word and must only ever
// mean a human is actually needed — so it appears with an open question and
// goes away the moment the reader speaks again.
func TestAttentionCountsOnlyOpenQuestions(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, nil, nil)
	if app.openQuestions() != 0 {
		t.Fatal("an empty room reports an open question")
	}

	backend.add(store.Message{
		SessionID: testSession, Role: store.RoleAgent, Body: "which one?",
		Parts: []store.MessagePart{store.QuestionRef(7)},
	})
	poll(t, app)
	if got := app.openQuestions(); got != 1 {
		t.Fatalf("an asked question is not counted as open: %d", got)
	}
	// The amber BADGE is gone (§7): the answer chip IS the statement that a
	// human is needed, and the attention count TINTS that chip rather than
	// adding a second mark for one meaning. This fixture's question carries no
	// numbered options, so there is no digit range to offer and the honest row
	// is a silent one — what must never come back is the badge.
	if row := ansi.Strip(app.status.Render(120, 1)); strings.Contains(row, tokens.GlyphNeedsHuman+"1") {
		t.Fatalf("the old attention badge came back: %q", row)
	}

	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "that one"})
	poll(t, app)
	if got := app.openQuestions(); got != 0 {
		t.Fatalf("the question stayed open after the reader answered: %d", got)
	}
}

// The renderer presents the journal; it never edits it. 13.1 item 3 is explicit
// that a chatty reply is the head's to fix, and a renderer that quietly deleted
// a repeat would be lying about the record.
func TestTheDressingNeverEditsWhatTheJournalSaid(t *testing.T) {
	const repeated = "I will read the file. I will read the file. I will read the file."
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: repeated})
	app := newTestApp(backend, nil, nil)
	poll(t, app)

	out := whole(t, app, 200)
	if strings.Count(out, "I will read the file.") != 3 {
		t.Fatalf("the renderer edited the journal's own words:\n%s", out)
	}
}

// -- residency ---------------------------------------------------------------

// fakeResidents hands out a scripted sequence of roles, one per probe, and then
// holds the last one. It is the shape the entry point's own adapter has: a
// state, and a replacement engine offered exactly once.
type fakeResidents struct {
	states []Residency
	adopt  Commander
	handed bool
	asked  int
}

func (f *fakeResidents) Residency() (Residency, Commander) {
	state := f.states[len(f.states)-1]
	if f.asked < len(f.states) {
		state = f.states[f.asked]
	}
	f.asked++
	if !state.Visitor && f.adopt != nil && !f.handed {
		f.handed = true
		return state, f.adopt
	}
	return state, nil
}

// probe drives one residency cycle the way the poll chain does.
func probe(app *App) {
	cmd := app.probeResidency()
	if cmd == nil {
		return
	}
	if msg, ok := cmd().(residencyMsg); ok {
		app.applyResidency(msg)
	}
}

// 5.20 rule 3: a window that is not running the head says so, for as long as it
// is true — and then stops saying it the moment it is not.
func TestAVisitorSaysSoAndThenGetsPromoted(t *testing.T) {
	promoted := &fakeCommander{model: "anthropic/claude-k3"}
	residents := &fakeResidents{
		states: []Residency{
			{Visitor: true, PID: 4242},
			{Visitor: true, PID: 4242, Note: "taking over as resident"},
			{},
		},
		adopt: promoted,
	}
	app := New(Options{
		Backend: &fakeBackend{}, Session: testSession, Profile: tokens.NoColor,
		Now: fixedNow, PollEvery: 1, Root: "/home/someone/aforge-v2", Home: "/home/someone",
		Residents: residents,
	})

	// The role is asked before the first frame, so the opening frame is honest.
	if !app.residency.Visitor {
		t.Fatal("a window that opened beside a resident drew itself as the resident")
	}
	if row := ansi.Strip(app.status.Render(120, 1)); !strings.Contains(row, "visitor") ||
		!strings.Contains(row, "pid 4242") {
		t.Fatalf("the footer does not say what this window is: %q", row)
	}

	probe(app)
	if row := ansi.Strip(app.status.Render(120, 1)); !strings.Contains(row, "taking over as resident") {
		t.Fatalf("the promotion under way is not on the row: %q", row)
	}

	probe(app)
	if app.residency.Visitor {
		t.Fatal("the window stayed a visitor after the lease came free")
	}
	if app.commander != Commander(promoted) {
		t.Fatal("the promoted window did not adopt the engine it was handed")
	}
	if got := app.status.model; got != "anthropic/claude-k3" {
		t.Fatalf("the promoted window kept the old model word: %q", got)
	}
	// A resident is the ordinary case, and the ordinary case earns no ink.
	if row := ansi.Strip(app.status.Render(120, 1)); strings.Contains(row, "visitor") {
		t.Fatalf("a promoted window is still calling itself a visitor: %q", row)
	}
}

// The engine is handed over exactly once: adopting the same commander twice
// would rebuild the window's chrome for nothing.
func TestTheReplacementEngineIsHandedOverOnce(t *testing.T) {
	residents := &fakeResidents{states: []Residency{{}}, adopt: &fakeCommander{}}
	app := New(Options{
		Backend: &fakeBackend{}, Session: testSession, Profile: tokens.NoColor,
		Now: fixedNow, PollEvery: 1, Residents: residents,
	})
	first := app.commander
	probe(app)
	if app.commander != first {
		t.Fatal("the window adopted a second engine it was never handed")
	}
}

// Capability honesty, in the place it costs the most: a visitor must never
// advertise an interrupt for a turn running in another process.
func TestAVisitorNeverAdvertisesAnInterrupt(t *testing.T) {
	residents := &fakeResidents{states: []Residency{{Visitor: true, PID: 7}}}
	app := New(Options{
		Backend: &fakeBackend{}, Commander: &fakeCommander{}, Session: testSession,
		Profile: tokens.NoColor, Now: fixedNow, PollEvery: 1, Residents: residents,
	})
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})

	if app.canInterrupt() {
		t.Fatal("a visitor believes it can stop a turn it is not running")
	}
	out := app.Frame(80, 20)
	if strings.Contains(out, "esc interrupt") {
		t.Fatalf("a visitor advertised an interrupt it cannot perform:\n%s", out)
	}
	if !strings.Contains(out, "visitor") {
		t.Fatalf("a visitor window does not say what it is:\n%s", out)
	}

	// And esc must not reach the interrupt door either — the hint and the key
	// have to agree, in both directions (8.2.21).
	app.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if commander, ok := app.commander.(*fakeCommander); ok && len(commander.interrupted) != 0 {
		t.Fatal("esc reached the interrupt door from a visitor window")
	}
}

// A window with no residency door wired is the ordinary single-window journey
// and must render exactly as it did before the seam existed.
func TestNoResidencyDoorMeansTheOrdinaryWindow(t *testing.T) {
	app := dressedApp(t)
	if app.residency.Visitor {
		t.Fatal("a window with no residency door called itself a visitor")
	}
	if row := ansi.Strip(app.status.Render(120, 1)); strings.Contains(row, "visitor") {
		t.Fatalf("the ordinary window is advertising a role it does not have: %q", row)
	}
	if app.probeResidency() != nil {
		t.Fatal("a window with no residency door armed a probe")
	}
}
