package tui3

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	machine "github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
)

type busyProbeOp struct{}

func (busyProbeOp) Verb() string   { return "want" }
func (busyProbeOp) Args() []string { return []string{"want"} }

func TestHoldStoreWriterHelperProcess(t *testing.T) {
	storeDir := os.Getenv("CODEAF_TEST_HOLD_STORE_DIR")
	if storeDir == "" {
		return
	}
	release, err := cellstore.HoldStoreWriter(storeDir, "codeaf chat")
	if err != nil {
		os.Exit(2)
	}
	_, _ = os.Stdout.WriteString("LOCKED\n")
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
	_ = release()
	os.Exit(0)
}

func startForeignStoreWriter(t *testing.T, storeDir string) func() {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestHoldStoreWriterHelperProcess$") //codeaf:plumbing spawns this test binary to hold the engine store writer lock in another process
	cmd.Env = append(os.Environ(), "CODEAF_TEST_HOLD_STORE_DIR="+storeDir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "LOCKED" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("helper failed to hold store %s: line=%q err=%v", storeDir, line, err)
	}
	var stopped bool
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_, _ = stdin.Write([]byte("release\n"))
		_ = stdin.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
	t.Cleanup(stop)
	return stop
}

type scriptedRemoteSource struct {
	rows []chatlist.Row
}

func (s *scriptedRemoteSource) Rows(context.Context) ([]chatlist.Row, error) {
	out := make([]chatlist.Row, len(s.rows))
	copy(out, s.rows)
	return out, nil
}

func stepKeyNoDrain(t *testing.T, a *app, k string) tea.Cmd {
	t.Helper()
	_, cmd := a.Update(key(k))
	return cmd
}

func collectBatchMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, sub := range batch {
			out = append(out, collectBatchMsgs(sub)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func countHomeRowsForID(a *app, id string) int {
	n := 0
	for _, line := range a.home.lines {
		switch {
		case line.kind == homeMachineRow && line.remote != nil && (line.remote.Cell == id || line.remote.Parent == id):
			n++
		case line.kind == homeSession && homeSessionID(line.row) == id:
			n++
		}
	}
	return n
}

// Contract 1: In one window, continuing three different chats last written on
// another computer, one after another, each shows `continuing here…` and opens
// that chat with its full history.
func TestContinueThreeRemoteChatsInOneWindowShowsContinuingAndFullHistory(t *testing.T) {
	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	initial := lab.session("project", "0000000000000000", "Local start", workspace, time.Now().Add(-time.Hour))

	type remoteChat struct {
		id      string
		title   string
		device  string
		history []session.DisplayEntry
		file    string
	}
	remotes := []remoteChat{
		{
			id:     "0000000000000101",
			title:  "Alpha migration",
			device: "tower",
			history: []session.DisplayEntry{
				{Role: "user", Text: "alpha user question"},
				{Role: "assistant", Text: "alpha assistant answer", Answer: true},
			},
		},
		{
			id:     "0000000000000102",
			title:  "Beta indexing",
			device: "laptop",
			history: []session.DisplayEntry{
				{Role: "user", Text: "beta first turn"},
				{Role: "assistant", Text: "beta full response", Answer: true},
			},
		},
		{
			id:     "0000000000000103",
			title:  "Gamma release",
			device: "studio",
			history: []session.DisplayEntry{
				{Role: "user", Text: "gamma ship checklist"},
				{Role: "assistant", Text: "gamma complete verification", Answer: true},
			},
		},
	}

	src := &scriptedRemoteSource{}
	byID := map[string]*remoteChat{}
	for i := range remotes {
		rc := &remotes[i]
		byID[rc.id] = rc
		src.rows = append(src.rows, chatlist.Row{
			Cell:       rc.id,
			Title:      rc.title,
			Device:     rc.device,
			DeviceID:   "dev_" + rc.device,
			Status:     chatlist.Off,
			DurableAgo: time.Duration(i+1) * time.Minute,
		})
	}

	a := lab.app(initial)
	a.width, a.height = 160, 48
	byFile := map[string][]session.DisplayEntry{}
	a.open = func(where, transcript string) (Conversation, error) {
		return Conversation{
			Agent:       &switchAgent{fakeAgent: &fakeAgent{model: "m", past: byFile[transcript]}},
			SessionFile: transcript,
			Workspace:   where,
			Resumed:     true,
		}, nil
	}

	taker := &fakeTaker{}
	taker.on = func(cellID string) {
		rc := byID[cellID]
		if rc == nil {
			t.Fatalf("unexpected cell %q", cellID)
		}
		rc.file = lab.session("project", rc.id, rc.title, workspace, time.Now())
		byFile[rc.file] = rc.history
		taker.taken = Taken{
			Fence:      uint64(len(taker.cells)),
			Transcript: rc.file,
			Resume:     machine.Resume{From: rc.device, Now: workspace},
		}
		for i := range src.rows {
			if src.rows[i].Cell == cellID {
				src.rows[i].Status = chatlist.Here
				src.rows[i].Mine = true
			}
		}
	}
	a.machines = src
	a.taker = taker

	for i := range remotes {
		rc := &remotes[i]
		drain(t, a, a.openHome())
		a.machinesAsking = false
		drain(t, a, a.askMachines())

		standOn(t, a, rc.id)
		_ = stepKeyNoDrain(t, a, "1")
		takeCmd := stepKeyNoDrain(t, a, "enter")

		if a.homeTaking != rc.id {
			t.Fatalf("%s: homeTaking = %q immediately after confirm, want %q", rc.id, a.homeTaking, rc.id)
		}
		if text := homeText(a); !strings.Contains(text, continuing) {
			t.Fatalf("%s: Home did not show %q while take ran:\n%s", rc.id, continuing, text)
		}

		drain(t, a, takeCmd)

		if a.homeTaking != "" {
			t.Fatalf("%s: homeTaking = %q after chat opened, want empty", rc.id, a.homeTaking)
		}
		if a.at(pageHome) {
			t.Fatalf("%s: stayed on Home instead of opening arrived chat:\n%s", rc.id, homeText(a))
		}
		if a.file != rc.file {
			t.Fatalf("%s: opened %q, want %q", rc.id, a.file, rc.file)
		}
		chatLines, _, _ := a.chatFrameLines(a.width, a.height)
		rendered := plain(strings.Join(chatLines, "\n"))
		for _, entry := range rc.history {
			if !strings.Contains(rendered, entry.Text) {
				t.Fatalf("%s: opened chat missing history line %q:\n%s", rc.id, entry.Text, rendered)
			}
		}
	}
}

// Contract 1: homeTaking is cleared on EVERY ending of a take (homeOpenLater,
// bringForward, failure, cancel), a press while a take is already running says
// so in one line on the card instead of doing nothing, and a take's failure is
// shown where the person is looking (the card on Home or notes in the chat).
func TestHomeTakingClearedOnEveryEndingAndBusyPressExplainsOnCard(t *testing.T) {
	t.Run("homeOpenLater and bringForward and cancel", func(t *testing.T) {
		lab := newHomeLab(t)
		workspace := lab.workspace("project")
		first := lab.session("project", "0000000000000201", "First", workspace, time.Now().Add(-2*time.Minute))
		second := lab.session("project", "0000000000000202", "Second", workspace, time.Now().Add(-time.Minute))

		src := &scriptedRemoteSource{
			rows: []chatlist.Row{
				{Cell: "0000000000000202", Title: "Second", Device: "desk", Status: chatlist.Off, DurableAgo: time.Minute},
				{Cell: "0000000000000201", Title: "First", Device: "desk", Status: chatlist.Off, DurableAgo: 2 * time.Minute},
			},
		}
		a := lab.app(first)
		a.width, a.height = 160, 48
		a.machines = src
		taker := &fakeTaker{taken: Taken{Fence: 1, Transcript: second, Resume: machine.Resume{From: "desk", Now: workspace}}}
		a.taker = taker

		// 1. Success through homeOpenLater parks `first` in a.behind and opens `second`.
		drain(t, a, a.openHome())
		drain(t, a, a.askMachines())
		standOn(t, a, "0000000000000202")
		confirm(t, a)
		if a.homeTaking != "" || a.file != second {
			t.Fatalf("after homeOpenLater: homeTaking=%q file=%q", a.homeTaking, a.file)
		}

		// 2. Success through bringForward (`first` is held in a.behind and AlreadyHere is true).
		taker.taken = Taken{Fence: 2, AlreadyHere: true, Transcript: first, Resume: machine.Resume{Now: workspace}}
		drain(t, a, a.openHome())
		a.machinesAsking = false
		drain(t, a, a.askMachines())
		standOn(t, a, "0000000000000201")
		confirm(t, a)
		if a.homeTaking != "" || a.file != first {
			t.Fatalf("after bringForward: homeTaking=%q file=%q", a.homeTaking, a.file)
		}

		// 3. While a take is already running, pressing continue on a card says so on the card,
		// and cancelling clears homeTaking and ignores the late message.
		src.rows = []chatlist.Row{
			{Cell: "0000000000000203", Title: "Third", Device: "desk", Status: chatlist.Off, DurableAgo: time.Minute},
			{Cell: "0000000000000204", Title: "Fourth", Device: "desk", Status: chatlist.Off, DurableAgo: 2 * time.Minute},
		}
		drain(t, a, a.openHome())
		a.machinesAsking = false
		drain(t, a, a.askMachines())

		standOn(t, a, "0000000000000203")
		_ = stepKeyNoDrain(t, a, "1")
		inFlight := stepKeyNoDrain(t, a, "enter")
		if a.homeTaking != "0000000000000203" {
			t.Fatalf("expected in-flight take, got %q", a.homeTaking)
		}

		// Press enter on another row while the first take is still running.
		standOn(t, a, "0000000000000204")
		card := strings.Join(cardRows(a), "\n")
		if !strings.Contains(card, continueAlreadyRunning) {
			t.Fatalf("busy press did not explain on the card:\n%s\nfull Home:\n%s", card, homeText(a))
		}

		// First esc closes the card; second esc cancels the in-flight take.
		drive(t, a, key("esc"))
		drive(t, a, key("esc"))
		if a.homeTaking != "" {
			t.Fatalf("esc did not clear homeTaking: %q", a.homeTaking)
		}
		// Late arrival from cancelled command is ignored.
		for _, msg := range collectBatchMsgs(inFlight) {
			_, _ = a.Update(msg)
		}
		if a.homeTaking != "" {
			t.Fatalf("late cancelled message dirtied homeTaking: %q", a.homeTaking)
		}

		// 4. Cancel during Phase 2 (conversationOpening) also clears homeTaking.
		thirdFile := lab.session("project", "0000000000000203", "Third", workspace, time.Now())
		taker.taken = Taken{Fence: 3, Transcript: thirdFile, Resume: machine.Resume{From: "desk", Now: workspace}}
		standOn(t, a, "0000000000000203")
		_ = stepKeyNoDrain(t, a, "1")
		phase1 := stepKeyNoDrain(t, a, "enter")
		takenMsg := phase1().(homeTakenMsg)
		_ = a.tookTakeover(takenMsg)
		if !a.conversationOpening {
			t.Fatal("expected Phase 2 conversationOpening")
		}
		a.cancelConversationOpening()
		if a.homeTaking != "" {
			t.Fatalf("cancelConversationOpening left homeTaking=%q", a.homeTaking)
		}
	})

	t.Run("failure shown on card on Home and in notes in chat", func(t *testing.T) {
		taker := &fakeTaker{err: errors.New("disk write failed")}
		a := continueHome(t, taker, continueRows, 160)
		standOn(t, a, "c-off")
		confirm(t, a)
		if a.homeTaking != "" {
			t.Fatalf("failed take left homeTaking=%q", a.homeTaking)
		}
		// The card itself must remain open with the failure reason and retry.
		card := strings.Join(cardRows(a), "\n")
		if !strings.Contains(card, continueFails) || !strings.Contains(card, continueRetry) {
			t.Fatalf("failure not shown on card with retry:\n%s", card)
		}

		// Failure while looking at the chat lands in chat notes and clears homeTaking.
		a.closeHome()
		cmd := a.takeRemote(chatlist.Row{Cell: "c-off", Title: "Port the picker", Device: "host-a", Status: chatlist.Off})
		drain(t, a, cmd)
		if a.homeTaking != "" {
			t.Fatalf("chat-page failed take left homeTaking=%q", a.homeTaking)
		}
		if notes := strings.Join(noteTexts(a), "\n"); !strings.Contains(notes, continueFails) {
			t.Fatalf("chat-page failed take missing from chat notes: %q", notes)
		}
	})
}

// Contract 2: When the engine store's writer is busy, continuing says within a
// few seconds that the store is busy and which program holds it (pid and
// command), offers retry, and never shows the generic "could not be put in this
// folder" sentence.
func TestBusyStoreNamedWithHolderAndOffersRetry(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), "v3", "stores", "shared")
	stopWriter := startForeignStoreWriter(t, storeDir)
	defer stopWriter()

	transport := cellstore.Spawn{
		Binary: "/bin/echo",
		Run: func(context.Context, string, []string, ...string) ([]byte, error) {
			return nil, errors.New("furrow: exit status 1: store writer is still busy after waiting 3 seconds; retry the operation")
		},
	}
	_, storeErr := transport.Do(context.Background(), cellstore.Target{Tree: t.TempDir(), DataDir: storeDir}, busyProbeOp{})
	if !errors.Is(storeErr, cellstore.ErrStoreBusy) {
		t.Fatalf("transport.Call err = %v, want errors.Is(_, cellstore.ErrStoreBusy)", storeErr)
	}

	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	initial := lab.session("project", "0000000000000300", "Start", workspace, time.Now().Add(-time.Hour))
	arrived := lab.session("project", "c-off", "Port the picker", workspace, time.Now())

	a := lab.app(initial)
	a.width, a.height = 160, 48
	a.machines = continueRows
	taker := &fakeTaker{err: fmt.Errorf("handoff: %w", storeErr)}
	a.taker = taker

	start := time.Now()
	drain(t, a, a.openHome())
	drain(t, a, a.askMachines())
	standOn(t, a, "c-off")
	confirm(t, a)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("busy store refusal took %s, want within a few seconds", elapsed)
	}

	card := strings.Join(strings.Fields(strings.Join(cardRows(a), " ")), " ")
	screen := homeText(a)
	if strings.Contains(screen, "could not be put in this folder") {
		t.Fatalf("generic folder error shown for busy store:\n%s", screen)
	}
	exeBase := filepath.Base(os.Args[0])
	for _, want := range []string{
		"could not continue this chat here because the engine store is busy (held by " + exeBase + ", pid ",
		continueRetry,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("busy store card missing %q:\n%s", want, card)
		}
	}

	// Release the foreign store lock and choose retry on the card.
	stopWriter()
	taker.err = nil
	taker.taken = Taken{Fence: 1, Transcript: arrived, Resume: machine.Resume{From: "host-a", Now: workspace}}

	confirm(t, a)
	if a.homeTaking != "" || a.at(pageHome) || a.file != arrived {
		t.Fatalf("retry after releasing busy store did not open chat: homeTaking=%q atHome=%v file=%q", a.homeTaking, a.at(pageHome), a.file)
	}
}

// Contract 3: Under `--host`, `continue here` is not offered and a chat the
// remote host already holds never shows as held elsewhere or duplicated.
func TestHostedSurfaceNeverOffersContinueOrShowsHeldElsewhere(t *testing.T) {
	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	bChat := lab.session("project", heldCell, "Host B Chat", workspace, time.Now())

	a := lab.app(bChat)
	a.host = "host-b"
	a.world = func() (session.World, bool) { return session.ReadWorld(a.placesRoot()), true }
	a.width, a.height = 160, 48
	taker := &fakeTaker{}
	src := &flipping{status: chatlist.Running}
	a.machines = src
	a.taker = taker
	// Even if stale machine rows exist, hosted Home ignores them consistently.
	a.machineRead.rows = []chatlist.Row{
		{Cell: heldCell, Title: "Host B Chat", Device: "host-b", Status: chatlist.Running, DurableAgo: time.Minute},
		{Cell: "other-remote", Title: "Other Remote", Device: "laptop", Status: chatlist.Off, DurableAgo: 2 * time.Minute},
	}

	drain(t, a, a.openHome())
	if cmd := a.pollMachines(a.now()); cmd != nil {
		t.Fatalf("hosted pollMachines returned non-nil command")
	}
	if cmd := a.askMachines(); cmd != nil {
		drain(t, a, cmd)
	}
	if _, elsewhere := a.heldElsewhere(); elsewhere {
		t.Fatal("hosted chat reported as heldElsewhere")
	}
	if a.leftOff.row != nil || a.devicesWanted() || a.addMachineWanted() {
		t.Fatal("hosted Home offered local device/resume bands")
	}

	screen := homeText(a)
	if strings.Contains(screen, "running on host-b") || strings.Contains(screen, chatlist.OfferContinue) || strings.Contains(screen, "Other Remote") {
		t.Fatalf("hosted Home showed held-elsewhere or continue offer:\n%s", screen)
	}
	if got := countHomeRowsForID(a, heldCell); got != 1 {
		t.Fatalf("hosted Home showed %d rows for %s, want 1:\n%s", got, heldCell, screen)
	}
}

// Contract 4: A chat known both locally and through sync appears once on Home,
// rows are keyed by chat id rather than transcript path, and branch copies of
// one chat collapse into the main row with a count and loss explanation.
func TestOneChatOneRowKeyedByIDAndCollapsedBranchCopies(t *testing.T) {
	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	now := time.Now()

	// Same chat ID stored at two different transcript paths (original and .moving copy).
	const sharedID = "0000000000000401"
	orig := lab.session("project", sharedID, "Shared Architecture", workspace, now.Add(-10*time.Minute))
	moving := lab.session(filepath.Join("project", ".moving"), sharedID, "Shared Architecture", workspace, now.Add(-2*time.Minute))
	_ = orig

	src := &scriptedRemoteSource{
		rows: []chatlist.Row{
			{
				Cell:       sharedID,
				Title:      "Shared Architecture",
				Device:     "tower",
				Status:     chatlist.Running,
				Head:       "turn-5",
				DurableAgo: time.Minute,
			},
			{
				Cell:        "branch-redundant",
				Title:       "Shared Architecture",
				Device:      "laptop",
				Parent:      sharedID,
				Status:      chatlist.Branch,
				Head:        "turn-5",
				OrphanTurns: 2,
				DurableAgo:  3 * time.Minute,
			},
		},
	}

	a := lab.app(moving)
	a.width, a.height = 180, 48
	a.machines = src
	a.taker = &fakeTaker{}

	drain(t, a, a.openHome())
	drain(t, a, a.askMachines())

	if got := countHomeRowsForID(a, sharedID); got != 1 {
		t.Fatalf("rows for %s when held elsewhere = %d, want 1:\n%s", sharedID, got, homeText(a))
	}
	if got := countHomeRowsForID(a, "branch-redundant"); got != 0 {
		t.Fatalf("branch copy rendered as separate row (%d):\n%s", got, homeText(a))
	}
	screen := homeText(a)
	flat := strings.Join(strings.Fields(screen), " ")
	for _, want := range []string{
		"1 copy",
		"unsent turns set aside",
		"discarding loses nothing",
	} {
		if !strings.Contains(flat, want) {
			t.Fatalf("Home missing collapsed copy explanation %q:\n%s", want, screen)
		}
	}

	// When held locally (Status == Here) and a second non-redundant branch copy exists,
	// there is still exactly one row for sharedID, now saying discarding archives turns not in this chat.
	src.rows[0].Status = chatlist.Here
	src.rows[0].Mine = true
	src.rows = append(src.rows, chatlist.Row{
		Cell:        "branch-extra",
		Title:       "Shared Architecture",
		Device:      "desk",
		Parent:      sharedID,
		Status:      chatlist.Branch,
		Head:        "turn-diverged",
		OrphanTurns: 3,
		DurableAgo:  4 * time.Minute,
	})
	a.machinesAsking = false
	drain(t, a, a.askMachines())

	if got := countHomeRowsForID(a, sharedID); got != 1 {
		t.Fatalf("rows for %s when held here = %d, want 1:\n%s", sharedID, got, homeText(a))
	}
	screen = homeText(a)
	for _, want := range []string{
		"2 copies",
		"unsent turns set aside",
		"discarding archives",
		"3 turns not in this chat",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("Home missing non-redundant copy explanation %q:\n%s", want, screen)
		}
	}
}
