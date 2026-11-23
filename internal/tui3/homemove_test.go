package tui3

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	machine "github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// The continue's line says what it is doing and how much has come through.
func TestMoveSaid(t *testing.T) {
	for _, c := range []struct {
		p    MoveProgress
		want string
	}{
		{MoveProgress{Step: "preparing"}, "opening conversation…"},
		{MoveProgress{Step: "copying", Done: 24 << 10}, "opening conversation… 24 KB"},
		{MoveProgress{Step: "copying", Done: 3 << 20}, "opening conversation… 3 MB"},
		{MoveProgress{Step: "opening", Done: 48 << 10}, "opening conversation… 48 KB"},
	} {
		if got := moveSaid(c.p); got != c.want {
			t.Errorf("default moveSaid(%+v) = %q, want %q", c.p, got, c.want)
		}
	}

	t.Setenv("CODEAF_MOVE_FILES_FIRST", "1")
	for _, c := range []struct {
		p    MoveProgress
		want string
	}{
		{MoveProgress{Step: "preparing"}, movingHere},
		{MoveProgress{Step: "copying", Done: 412 << 20, Total: 1433 << 20}, "copying to this computer: 412 of 1,433 MB…"},
		{MoveProgress{Step: "copying", Done: 3 << 20}, "copying to this computer: 3 MB so far…"},
		{MoveProgress{Step: "copying", Done: 900 << 10, Total: 1000 << 10}, "copying to this computer: 900 of 1,000 KB…"},
		{MoveProgress{Step: "opening", Done: 5}, "opening it here…"},
	} {
		if got := moveSaid(c.p); got != c.want {
			t.Errorf("files-first moveSaid(%+v) = %q, want %q", c.p, got, c.want)
		}
	}
}

// movingTaker is a fakeTaker that says how far its continue has got.
type movingTaker struct {
	fakeTaker
	at        MoveProgress
	active    bool
	retried   string
	safeClean bool
	safeCalls int
}

func (m *movingTaker) SafeToContinue(context.Context, string, string, bool) (bool, error) {
	m.safeCalls++
	return m.safeClean, nil
}

func (m *movingTaker) TakeSeen(ctx context.Context, cell string, _ *directory.LeaseExpectation) (Taken, error) {
	return m.Take(ctx, cell)
}

func (m *movingTaker) MoveProgress(string) (MoveProgress, bool) {
	if !m.active && m.at == (MoveProgress{}) {
		return MoveProgress{}, false
	}
	return m.at, m.active || m.at != (MoveProgress{})
}

func (m *movingTaker) RetryMove(cell string) bool {
	m.retried = cell
	m.at = MoveProgress{Step: "arriving", Done: 40 << 20, Total: 112 << 20}
	m.active = true
	return true
}

// Home's line follows a running continue, and only while home still shows it.
func TestHomeFollowsAMove(t *testing.T) {
	taker := &movingTaker{at: MoveProgress{Step: "copying", Done: 24 << 10}}
	a := continueHome(t, taker, continueRows, 200)
	a.homeTaking, a.homeTakeSeq = "c-off", 7
	a.home.say("opening conversation…", "")
	if next := a.followMove(homeMoveTickMsg{seq: 7}); next == nil {
		t.Fatal("a running continue stopped being followed")
	}
	if a.home.msg != "opening conversation… 24 KB" {
		t.Fatalf("home says %q", a.home.msg)
	}
	a.home.say("something else", "")
	_ = a.followMove(homeMoveTickMsg{seq: 7})
	if a.home.msg != "something else" {
		t.Fatalf("progress wrote over another line: %q", a.home.msg)
	}
	if next := a.followMove(homeMoveTickMsg{seq: 6}); next != nil {
		t.Fatal("an older continue's tick kept running")
	}
}

// A continue that stalled says so, and where the chat still is.
func TestAStalledContinueSaysWhy(t *testing.T) {
	a := continueHome(t, &fakeTaker{}, continueRows, 200)
	got := a.continueFailure(homeTakenMsg{row: continueRows[0], err: fmt.Errorf("%w: handoff: fetch", ErrMoveStalled)})
	if !strings.Contains(got, "stopped: nothing came through for 2 minutes; the chat is still on host-a") {
		t.Fatalf("a stalled continue says %q", got)
	}
}

// A chat whose computer is not running codeaf cannot be copied from it. The
// failure names that computer (C5). Whether it also says the words typed here
// are kept depends on whether there are any: see the three tests below.
func TestContinueNamesTheComputerThatIsNotOn(t *testing.T) {
	a := continueHome(t, &fakeTaker{}, continueRows, 200)
	for _, err := range []error{
		fmt.Errorf("cellsync: fetch object x: %w", &syncwords.HolderOfflineError{Name: "tower"}),
		fmt.Errorf("handoff: %w", syncwords.ErrHolderOffline),
	} {
		row := continueRows[0]
		row.Device = "tower"
		got := a.continueFailure(homeTakenMsg{row: row, err: err})
		if !strings.Contains(got, "tower has this chat and is not on right now; open codeaf there, then move here") {
			t.Fatalf("a continue whose computer is off says %q", got)
		}
	}
}

// F2: an automatic continue at launch has no typed draft, so the holder-off
// sentence must not claim that words are still in the box.
func TestHolderOffSentenceWithoutADraftDoesNotMentionTheBox(t *testing.T) {
	a := continueHome(t, &fakeTaker{}, continueRows, 200)
	row := continueRows[0]
	row.Device = "tower"
	err := fmt.Errorf("handoff: %w", &syncwords.HolderOfflineError{Name: "tower"})
	want := "tower has this chat and is not on right now; open codeaf there, then move here"
	for name, msg := range map[string]homeTakenMsg{
		"automatic continue, no draft": {row: row, err: err, automatic: true},
		"a draft holding nothing":      {row: row, err: err, draft: &composerState{}},
	} {
		if got := a.continueFailure(msg); got != want {
			t.Errorf("%s: the holder-off sentence is %q; want exactly %q", name, got, want)
		}
	}
}

// F2, the other half: with words typed in a chat window's composer, or in
// Home's own box, the sentence still says they were kept.
func TestHolderOffSentenceWithAnUnsentDraftSaysTheWordsAreKept(t *testing.T) {
	row := continueRows[0]
	row.Device = "tower"
	err := fmt.Errorf("handoff: %w", &syncwords.HolderOfflineError{Name: "tower"})
	const tail = "; your words are still in the box"

	a := continueHome(t, &fakeTaker{}, continueRows, 200)
	var box editor
	box.setText("carry on with the migration")
	got := a.continueFailure(homeTakenMsg{row: row, err: err, draft: &composerState{box: box}})
	if !strings.HasSuffix(got, tail) || !strings.HasPrefix(got, "tower has this chat and is not on right now") {
		t.Errorf("with a draft captured from a chat window the sentence is %q", got)
	}

	b := continueHome(t, &fakeTaker{}, continueRows, 200)
	b.home.box.setText("typed on home while it tried")
	if !b.at(pageHome) {
		t.Fatal("the home page is not showing")
	}
	got = b.continueFailure(homeTakenMsg{row: row, err: err, automatic: true})
	if !strings.HasSuffix(got, tail) {
		t.Errorf("with words typed in Home's box the sentence is %q", got)
	}
}

// A computer with live transfer switched off says why a chat only a live
// computer holds cannot be copied, and how to fix it.
func TestContinueSaysLiveTransferIsOff(t *testing.T) {
	a := continueHome(t, &fakeTaker{}, continueRows, 200)
	got := a.continueFailure(homeTakenMsg{row: continueRows[0], err: fmt.Errorf("cellsync: fetch object x: %w", syncwords.ErrLiveTransferOff)})
	if !strings.Contains(got, "blind pass-through is switched off here") || !strings.Contains(got, "CODEAF_PASSTHROUGH") {
		t.Fatalf("a continue with live transfer off says %q", got)
	}
}

// Before a big folder the continue card says what moving it copies.
func TestContinueCardWarnsBeforeABigFolder(t *testing.T) {
	big := chatlist.Row{Cell: "c-big", Title: "Index", Device: "host-a", Status: chatlist.Off, DurableAgo: time.Hour, Folder: "CodeAF", Size: 1500 << 20}
	if got := continueReason(big); !strings.Contains(got, "this chat works in the CodeAF folder on host-a; moving it copies that folder too: 1.5 GB, which can take several minutes") {
		t.Fatalf("the card says %q", got)
	}
	small := big
	small.Size = 20 << 20
	if got := continueReason(small); strings.Contains(got, "copies that folder") {
		t.Fatalf("a small chat warned: %q", got)
	}
	unnamed := big
	unnamed.Folder = ""
	if got := continueReason(unnamed); !strings.Contains(got, "this chat works in a folder on host-a") {
		t.Fatalf("a chat with no folder name says %q", got)
	}
}

// Contract C2 and C4: the open chat's status line shows arriving workspace
// bytes (never 0 of 0), names a failed background fetch in plain words with a
// retry door, and clears when the files finish arriving.
func TestC2FilesArrivingStatusLineAppearsShowsBytesAndClears(t *testing.T) {
	for _, c := range []struct {
		p    MoveProgress
		want string
	}{
		{MoveProgress{Step: "arriving", Done: 0, Total: 0}, "files arriving"},
		{MoveProgress{Step: "arriving", Done: 0, Total: 112 << 20}, "files arriving"},
		{MoveProgress{Step: "arriving", Done: 100, Total: 200}, "files arriving"},
		{MoveProgress{Step: "arriving", Done: 40 << 20, Total: 112 << 20}, "files arriving · 40 of 112 MB"},
		{MoveProgress{Step: "arriving", Done: 40 << 20, Total: 0}, "files arriving · 40 MB"},
		{MoveProgress{Step: "failed", Err: "sync could not be reached"}, "files couldn't arrive: sync could not be reached · retry"},
		{MoveProgress{Step: "done", Done: 112 << 20, Total: 112 << 20}, ""},
	} {
		got := filesArrivingSaid(c.p)
		if got != c.want {
			t.Errorf("filesArrivingSaid(%+v) = %q, want %q", c.p, got, c.want)
		}
		if strings.Contains(got, "0 of 0") {
			t.Errorf("filesArrivingSaid(%+v) violated emptiness law: %q", c.p, got)
		}
	}

	taker := &movingTaker{active: true, at: MoveProgress{Step: "arriving", Done: 40 << 20, Total: 112 << 20}}
	a := continueHome(t, taker, continueRows, 200)
	a.closeHome()
	a.file = "/tmp/c-off/.cell/transcript.jsonl"
	a.homeTakeSeq = 9

	// Appears on the open chat's status line and quotes arriving bytes (C2).
	_, plain := a.seamTelemetryLabel(lineParts(a.seamParts(200)))
	if !strings.Contains(plain, "files arriving · 40 of 112 MB") {
		t.Fatalf("status line while files arrive = %q, want files arriving · 40 of 112 MB", plain)
	}
	if cmd := a.followMove(homeMoveTickMsg{seq: 9}); cmd == nil {
		t.Fatal("followMove stopped ticking while files are still arriving")
	}

	// Failure names the problem in plain words with a retry door (C4).
	taker.at = MoveProgress{Step: "failed", Done: 40 << 20, Total: 112 << 20, Err: "sync could not be reached"}
	ledger, alive := lineParts(a.seamParts(200))
	_, plain = a.seamTelemetryLabel(ledger, alive)
	if !strings.Contains(plain, "files couldn't arrive: sync could not be reached · retry") {
		t.Fatalf("status line on failed background fetch = %q", plain)
	}
	a.doors = nil
	a.markDoors(alive, 0, legendDoorRow)
	var retryDoor *statusDoor
	for i := range a.doors {
		if a.doors[i].kind == segFiles {
			retryDoor = &a.doors[i]
			break
		}
	}
	if retryDoor == nil {
		t.Fatal("failed files segment did not register a retry door")
	}
	cmd, handled := a.doorPress(*retryDoor)
	if !handled || cmd == nil || taker.retried != "c-off" {
		t.Fatalf("pressing retry door: handled=%v cmd=%v retried=%q", handled, cmd != nil, taker.retried)
	}

	// Clears when the files finish arriving (C2).
	taker.at = MoveProgress{Step: "done", Done: 112 << 20, Total: 112 << 20}
	taker.active = false
	_, plain = a.seamTelemetryLabel(lineParts(a.seamParts(200)))
	if strings.Contains(plain, "files arriving") || strings.Contains(plain, "files couldn't arrive") {
		t.Fatalf("status line did not clear after files arrived: %q", plain)
	}
	if cmd := a.followMove(homeMoveTickMsg{seq: 9}); cmd != nil {
		t.Fatal("followMove kept ticking after files finished arriving")
	}
}

// Contract R5: from the keypress until shown, Home's bottom line is never a
// blank wait; it shows "opening conversation… N KB" updated at least every
// 500 ms, and brings back the "copying to this computer" counter for
// CODEAF_MOVE_FILES_FIRST=1.
func TestR5HomeBottomLineNeverBlankShowsOpeningConversationBytesAndFilesFirstCounter(t *testing.T) {
	if moveTickEvery > 500*time.Millisecond {
		t.Fatalf("moveTickEvery = %s, want <= 500ms", moveTickEvery)
	}

	taker := &movingTaker{active: true, safeClean: true, at: MoveProgress{Step: "preparing"}}
	a := continueHome(t, taker, continueRows, 200)
	standOn(t, a, "c-off")
	_ = stepKeyNoDrain(t, a, "1")
	_ = stepKeyNoDrain(t, a, "enter")

	if a.home.msg == "" || !strings.Contains(homeText(a), "opening conversation…") {
		t.Fatalf("immediately after keypress Home bottom line = %q, frame:\n%s", a.home.msg, homeText(a))
	}

	// Progress updates to opening conversation… N KB on tick, and stays visible across keystrokes.
	taker.at = MoveProgress{Step: "copying", Done: 24 << 10}
	_ = a.followMove(homeMoveTickMsg{seq: a.homeTakeSeq})
	if a.home.msg != "opening conversation… 24 KB" {
		t.Fatalf("after first progress tick Home bottom line = %q, want %q", a.home.msg, "opening conversation… 24 KB")
	}
	_ = stepKeyNoDrain(t, a, "down")
	if a.home.msg != "opening conversation… 24 KB" {
		t.Fatalf("after navigation key Home bottom line went blank or changed: %q", a.home.msg)
	}
	taker.at = MoveProgress{Step: "copying", Done: 68 << 10}
	_ = a.followMove(homeMoveTickMsg{seq: a.homeTakeSeq})
	if a.home.msg != "opening conversation… 68 KB" {
		t.Fatalf("after second progress tick Home bottom line = %q, want %q", a.home.msg, "opening conversation… 68 KB")
	}

	// CODEAF_MOVE_FILES_FIRST=1 restores the "copying to this computer" counter.
	t.Setenv("CODEAF_MOVE_FILES_FIRST", "1")
	taker.at = MoveProgress{Step: "copying", Done: 412 << 20, Total: 1433 << 20}
	_ = a.followMove(homeMoveTickMsg{seq: a.homeTakeSeq})
	if a.home.msg != "copying to this computer: 412 of 1,433 MB…" {
		t.Fatalf("with CODEAF_MOVE_FILES_FIRST=1 Home bottom line = %q, want %q", a.home.msg, "copying to this computer: 412 of 1,433 MB…")
	}
}

// Contract R6: while a continue started from Home is running, text typed on
// Home is held as the draft of the chat being moved and lands in its box when
// it opens; it is never sent, and Enter never starts a new conversation from it.
func TestR6TypingDuringHomeMoveLandsAsDraftInArrivingChatAndEnterNeverStartsNew(t *testing.T) {
	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	initial := lab.session("project", "0000000000000601", "Initial", workspace, time.Now().Add(-time.Hour))
	arrived := lab.session("project", "c-off", "Port the picker", workspace, time.Now())

	var startedNew int
	openedFake := &fakeAgent{model: "m"}
	a := lab.app(initial)
	a.width, a.height = 180, 48
	a.start = func(where string) (Conversation, error) {
		startedNew++
		return Conversation{}, fmt.Errorf("unexpected new conversation in %s", where)
	}
	a.open = func(where, file string) (Conversation, error) {
		return Conversation{
			Agent:       &switchAgent{fakeAgent: openedFake},
			SessionFile: file,
			Workspace:   where,
			Resumed:     true,
		}, nil
	}

	src := &scriptedRemoteSource{
		rows: []chatlist.Row{
			{Cell: "c-off", Title: "Port the picker", Device: "host-a", Status: chatlist.Off, DurableAgo: time.Minute},
		},
	}
	taker := &movingTaker{
		active: true,
		at:     MoveProgress{Step: "copying", Done: 16 << 10},
		fakeTaker: fakeTaker{
			taken: Taken{
				Fence:      1,
				Transcript: arrived,
				Resume:     machine.Resume{From: "host-a", Now: workspace},
			},
			on: func(cellID string) {
				for i := range src.rows {
					if src.rows[i].Cell == cellID {
						src.rows[i].Status = chatlist.Here
						src.rows[i].Mine = true
					}
				}
			},
		},
	}
	a.machines = src
	a.taker = taker

	drain(t, a, a.openHome())
	drain(t, a, a.askMachines())
	standOn(t, a, "c-off")
	_ = stepKeyNoDrain(t, a, "1")
	takeCmd := stepKeyNoDrain(t, a, "enter")
	if a.homeTaking != "c-off" {
		t.Fatalf("expected c-off take in flight, got %q", a.homeTaking)
	}

	// Type a draft on Home while the continue is running and press Enter.
	for _, r := range "check the parser edge cases" {
		_ = stepKeyNoDrain(t, a, string(r))
	}
	if got := a.home.box.String(); got != "check the parser edge cases" {
		t.Fatalf("Home box while typing = %q, want %q", got, "check the parser edge cases")
	}
	if cmd := stepKeyNoDrain(t, a, "enter"); cmd != nil {
		drain(t, a, cmd)
	}
	if cmd := stepKeyNoDrain(t, a, "ctrl+enter"); cmd != nil {
		drain(t, a, cmd)
	}
	if cmd := stepKeyNoDrain(t, a, "alt+enter"); cmd != nil {
		drain(t, a, cmd)
	}
	if startedNew != 0 {
		t.Fatalf("Enter during move started %d new conversations, want 0", startedNew)
	}
	if !a.at(pageHome) || a.homeTaking != "c-off" {
		t.Fatalf("Enter during move left Home or cancelled take: atHome=%v homeTaking=%q", a.at(pageHome), a.homeTaking)
	}

	// Now let the move finish and open the arrived chat.
	drain(t, a, takeCmd)
	if a.at(pageHome) || a.file != arrived {
		t.Fatalf("move did not open arrived chat: atHome=%v file=%q want=%q", a.at(pageHome), a.file, arrived)
	}
	if got := a.input.String(); got != "check the parser edge cases" {
		t.Fatalf("arrived chat composer draft = %q, want %q", got, "check the parser edge cases")
	}
	if len(openedFake.sent) != 0 {
		t.Fatalf("typed draft was sent automatically (%v), want unsent in composer", openedFake.sent)
	}
}

// Contract R3 (alt+c path): pressing alt+c on "Continue <device>'s latest chat
// here?" shows "opening conversation…" immediately and opens the arrived chat
// while workspace files continue arriving in the background.
func TestR3ResumeBandAltCOpensEarly(t *testing.T) {
	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	initial := lab.session("project", "0000000000000602", "Initial", workspace, time.Now().Add(-time.Hour))
	arrived := lab.session("project", "c-off", "Port the picker", workspace, time.Now())

	a := lab.app(initial)
	a.width, a.height = 180, 48
	src := &scriptedRemoteSource{
		rows: []chatlist.Row{
			{Cell: "c-off", Title: "Port the picker", Device: "tower", DeviceID: "dev-tower", Status: chatlist.Off, DurableAgo: time.Minute},
		},
	}
	taker := &movingTaker{
		active:    true,
		safeClean: true,
		at:        MoveProgress{Step: "arriving", Done: 18 << 20, Total: 112 << 20},
		fakeTaker: fakeTaker{
			taken: Taken{
				Fence:      1,
				Transcript: arrived,
				Resume:     machine.Resume{From: "tower", Now: workspace},
			},
			on: func(cellID string) {
				for i := range src.rows {
					if src.rows[i].Cell == cellID {
						src.rows[i].Status = chatlist.Here
						src.rows[i].Mine = true
					}
				}
			},
		},
	}
	a.machines = src
	a.taker = taker

	drain(t, a, a.openHome())
	drain(t, a, a.askMachines())
	if !strings.Contains(homeText(a), "Move tower's latest chat here?") {
		t.Fatalf("expected resume band on Home:\n%s", homeText(a))
	}

	safeCmd := stepKeyNoDrain(t, a, "alt+c")
	if a.home.msg != "opening conversation…" {
		t.Fatalf("immediately after alt+c Home bottom line = %q, want %q", a.home.msg, "opening conversation…")
	}
	drain(t, a, safeCmd)
	if a.at(pageHome) || a.file != arrived {
		t.Fatalf("alt+c did not open arrived chat: atHome=%v file=%q", a.at(pageHome), a.file)
	}
	_, statusPlain := a.seamTelemetryLabel(lineParts(a.seamParts(200)))
	if !strings.Contains(statusPlain, "files arriving · 18 of 112 MB") {
		t.Fatalf("open chat status line after early open = %q, want files arriving · 18 of 112 MB", statusPlain)
	}
}

// Contract R5d: when a draft typed on Home during a move (R6) arrives in a chat
// that already has an older saved draft, neither text is lost: the box shows the
// saved draft, then a newline, then the carried text; the saved draft counts as
// restored; quitting saves the box as usual; and "saved draft has not been
// restored" never appears.
func TestR5dCarriedHomeDraftMergesWithSavedDraftAndSavesOnQuit(t *testing.T) {
	for _, sameWindowFile := range []bool{false, true} {
		name := "older_window_draft"
		if sameWindowFile {
			name = "same_draft_file"
		}
		t.Run(name, func(t *testing.T) {
			lab := newHomeLab(t)
			workspace := lab.workspace("project")
			initial := lab.session("project", "0000000000000601", "Initial", workspace, time.Now().Add(-time.Hour))
			arrived := lab.session("project", heldCell, "Port the picker", workspace, time.Now())
			drafts := t.TempDir()

			const savedWords = "older saved draft from before the move"
			const carriedWords = "carried text typed on home during move"
			const wantCombined = savedWords + "\n" + carriedWords

			var priorDraft string
			arrivedDraft := DraftFile(drafts, workspace)
			if sameWindowFile {
				previous := lab.app(arrived)
				previous.draftFile = arrivedDraft
				previous.input.setText(savedWords)
				if err := previous.draftSaveOf(previous.mainDraftText()).commit(); err != nil {
					t.Fatal(err)
				}
			} else {
				priorDraft = asWindow(t, deadPid(t), func() string { return DraftFile(drafts, workspace) })
				previous := lab.app(arrived)
				previous.draftFile = priorDraft
				previous.input.setText(savedWords)
				if err := previous.draftSaveOf(previous.mainDraftText()).commit(); err != nil {
					t.Fatal(err)
				}
			}

			openedFake := &fakeAgent{model: "m"}
			a := lab.app(initial)
			a.width, a.height = 180, 48
			a.draftFile = DraftFile(drafts, workspace)
			a.open = func(where, file string) (Conversation, error) {
				return Conversation{
					Agent:       &switchAgent{fakeAgent: openedFake},
					SessionFile: file,
					Workspace:   where,
					DraftFile:   arrivedDraft,
					Resumed:     true,
				}, nil
			}

			src := &scriptedRemoteSource{
				rows: []chatlist.Row{
					{Cell: heldCell, Title: "Port the picker", Device: "host-a", Status: chatlist.Off, DurableAgo: time.Minute},
				},
			}
			taker := &movingTaker{
				active: true,
				at:     MoveProgress{Step: "copying", Done: 16 << 10},
				fakeTaker: fakeTaker{
					taken: Taken{
						Fence:      1,
						Transcript: arrived,
						Resume:     machine.Resume{From: "host-a", Now: workspace},
					},
					on: func(cellID string) {
						for i := range src.rows {
							if src.rows[i].Cell == cellID {
								src.rows[i].Status = chatlist.Here
								src.rows[i].Mine = true
							}
						}
					},
				},
			}
			a.machines = src
			a.taker = taker

			drain(t, a, a.openHome())
			drain(t, a, a.askMachines())
			standOn(t, a, heldCell)
			_ = stepKeyNoDrain(t, a, "1")
			takeCmd := stepKeyNoDrain(t, a, "enter")
			if a.homeTaking != heldCell {
				t.Fatalf("expected %s take in flight, got %q", heldCell, a.homeTaking)
			}

			for _, r := range carriedWords {
				_ = stepKeyNoDrain(t, a, string(r))
			}
			if got := a.home.box.String(); got != carriedWords {
				t.Fatalf("Home box while typing = %q, want %q", got, carriedWords)
			}

			drain(t, a, takeCmd)
			if a.at(pageHome) || a.file != arrived {
				t.Fatalf("move did not open arrived chat: atHome=%v file=%q want=%q", a.at(pageHome), a.file, arrived)
			}
			if got := a.input.String(); got != wantCombined {
				t.Fatalf("arrived chat composer draft = %q, want %q", got, wantCombined)
			}
			for _, e := range a.entries {
				if strings.Contains(e.text, "saved draft has not been restored") || strings.Contains(e.text, draftKeepFailWord) {
					t.Fatalf("unexpected draft failure notice in entries: %q", e.text)
				}
			}
			if note := plain(lastNote(t, a)); strings.Contains(note, "saved draft has not been restored") || strings.Contains(note, draftKeepFailWord) {
				t.Fatalf("unexpected draft failure notice in lastNote: %q", note)
			}
			if got := readDraft(arrivedDraft); got != wantCombined {
				t.Fatalf("arrived draft file right after open = %q, want %q", got, wantCombined)
			}

			// Simulate quitting and verify the combined draft is saved as usual.
			a.writeDraftsNow(a.leavingDraft())
			for _, e := range a.entries {
				if strings.Contains(e.text, "saved draft has not been restored") || strings.Contains(e.text, draftKeepFailWord) {
					t.Fatalf("unexpected draft failure notice on quit: %q", e.text)
				}
			}
			if got := readDraft(arrivedDraft); got != wantCombined {
				t.Fatalf("arrived draft file after quit = %q, want %q", got, wantCombined)
			}
			if priorDraft != "" {
				if got := readDraft(priorDraft); got != "" {
					t.Fatalf("old window draft file still has %q after reunite and quit, want empty", got)
				}
			}

			// Reopening the chat in a later window restores the combined draft.
			reopened := lab.app(arrived)
			reopened.draftFile = arrivedDraft
			reopened.restoreDraft()
			if got := reopened.input.String(); got != wantCombined {
				t.Fatalf("reopened composer = %q, want %q", got, wantCombined)
			}
		})
	}
}
