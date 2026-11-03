package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

type savingQuitAgent struct {
	quietAgent
	save func() error
}

func (a *savingQuitAgent) Close() error { return a.save() }

// Contract E2 follows the actual quit, launcher teardown and bare receiving launch.
// L4-H4b preserves publication and release when the final upload succeeds.
func TestL4HostedSurfaceQuitPublishesReleasesAndBareLaunchMovesTheChat(t *testing.T) {
	runHostedQuitContract(t, false)
}

// L4-H4b reports a successful local save when the final upload cannot finish.
func TestL4HostedSurfaceQuitWithRelayDownKeepsTheLocalTurns(t *testing.T) {
	runHostedQuitContract(t, true)
}

func runHostedQuitContract(t *testing.T, relayDown bool) {
	t.Helper()
	t.Cleanup(stopFinalSaveProgress)
	cfg, c := doorChat(t)
	short, err := os.MkdirTemp("", "ce-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(short)
	t.Setenv(home.EnvVar, short)
	id, err := identity.Ensure(short)
	if err != nil {
		t.Fatal(err)
	}
	service := relayserve.New(relayserve.Config{Store: t.TempDir()})
	defer service.Close()
	var releases atomic.Int32
	var down atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/release") {
			releases.Add(1)
		}
		service.Handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	t.Setenv(syncsetup.URLVar, server.URL)
	a, ok, err := syncsetup.Open(short)
	if err != nil || !ok {
		t.Fatalf("open sender: %v", err)
	}
	bHome := t.TempDir()
	if _, err := identity.Adopt(bHome, id, false); err != nil {
		t.Fatal(err)
	}
	b, ok, err := syncsetup.Open(bHome)
	if err != nil || !ok {
		t.Fatalf("open receiver: %v", err)
	}
	engine := cellstore.EngineFor(cfg.Place.Workspace)
	engine.DataRoot = t.TempDir()
	t.Cleanup(cellstore.StopSpawned)
	if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID, Workspace: cfg.Place.Workspace, Title: "quit handoff"}); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(c.Root, cell.TranscriptPath)
	write := func(words string) {
		t.Helper()
		if err := os.WriteFile(transcript, []byte(words), 0600); err != nil {
			t.Fatal(err)
		}
	}
	first := `{"type":"session","version":3,"id":"quit"}` + "\n" + `{"type":"message","role":"user","content":"first turn"}` + "\n"
	write(first)
	drive, err := a.Drive(t.Context(), engine, c, syncsetup.DriveOptions{DeviceName: "sender", OnNotice: func(line string) { t.Log(line) }})
	if err != nil {
		t.Fatal(err)
	}
	defer drive.Close(context.Background())
	if _, err := drive.Store(engine).Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	drive.Idle()
	deadline := time.Now().Add(cellstore.SealWithin)
	for {
		_, fence, e := a.SendLease(c.ID)
		if e == nil && fence != 0 {
			break
		}
		if time.Now().After(deadline) {
			view, relayErr := a.Dir.Cell(t.Context(), c.ID)
			closing, stop := context.WithTimeout(t.Context(), time.Second)
			closeErr := drive.Close(closing)
			stop()
			t.Fatalf("first turn did not publish: relay=%+v relay error=%v close error=%v", view, relayErr, closeErr)
		}
		time.Sleep(time.Millisecond)
	}
	write(first + `{"type":"message","role":"assistant","content":"final words"}` + "\n")
	saving, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	far := &savingQuitAgent{save: func() error {
		close(saving)
		<-release
		_, err := drive.Store(engine).Seal(context.Background(), c, cellstore.TurnInfo{})
		return err
	}}
	hostDone := make(chan error, 1)
	go func() {
		hostDone <- enginehost.Run(cfg.Place.Workspace, enginehost.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: far, Workspace: cfg.Place.Workspace, SessionFile: transcript, ReleaseOnLeave: func(remote.WrappedAgent) bool { return true }, FinishSaving: func(remote.WrappedAgent) error {
				ctx, cancel := context.WithTimeout(context.Background(), cellstore.SealWithin)
				defer cancel()
				return session.SavingError(transcript, drive.Close(ctx))
			}}, nil
		}})
	}()
	var conn net.Conn
	deadline = time.Now().Add(cellstore.SealWithin)
	for time.Now().Before(deadline) {
		conn, err = enginehost.Dial(cfg.Place.Workspace)
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if conn == nil {
		t.Fatal("host never listened")
	}
	client, err := remote.Dial(conn, "", remote.Hello{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		_ = enginehost.Retire(cfg.Place.Workspace, true)
		<-hostDone
	}()
	oldRun := runSurfaceProgram
	defer func() { runSurfaceProgram = oldRun }()
	input, typed := io.Pipe()
	defer input.Close()
	defer typed.Close()
	screen := &surfaceFrameWriter{ready: make(chan struct{})}
	restored := make(chan struct{})
	runSurfaceProgram = func(ctx context.Context, o tui3.Options) error {
		o.Input = input
		o.Output = screen
		o.Width = 100
		o.Height = 30
		err := tui3.Run(ctx, o)
		close(restored)
		return err
	}
	ctx, cancel := context.WithTimeout(t.Context(), cellstore.SealWithin)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runSurface(ctx, tui3.Options{Agent: client.Agent(), Workspace: cfg.Place.Workspace, SessionFile: transcript, ProfileDir: short})
	}()
	<-screen.ready
	began := time.Now()
	down.Store(relayDown)
	if _, err := io.WriteString(typed, "/quit\r"); err != nil {
		t.Fatal(err)
	}
	<-saving
	<-restored
	select {
	case err := <-done:
		t.Fatalf("launcher returned before saving: %v", err)
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if relayDown {
		if time.Since(began) > cellstore.SealWithin+time.Second {
			t.Fatal("offline quit exceeded the save ceiling")
		}
		if releases.Load() != 0 {
			t.Fatal("offline quit advertised a release")
		}
		if _, fence, err := a.SendLease(c.ID); err != nil || fence == 0 {
			t.Fatalf("offline quit lost the retained hold: %v", err)
		}
		raw, err := os.ReadFile(transcript)
		if err != nil || !strings.Contains(string(raw), "final words") {
			t.Fatal("offline quit lost its final words")
		}
		oldFlush, oldOut := flushFinalSaves, os.Stdout
		out, err := os.CreateTemp(t.TempDir(), "saved-line")
		if err != nil {
			t.Fatal(err)
		}
		defer out.Close()
		defer func() { flushFinalSaves, os.Stdout = oldFlush, oldOut }()
		flushFinalSaves = func(context.Context) error { return nil }
		os.Stdout = out
		if !finishFinalSaving(t.Context()) {
			t.Fatal("a confirmed local save was reported as a failure")
		}
		if _, err := out.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		line, err := io.ReadAll(out)
		if err != nil || !strings.Contains(string(line), "saved here; it reaches your other devices when the sync service at ") || strings.Contains(string(line), transcript) || strings.Contains(string(line), "could not save") {
			t.Fatalf("offline quit report=%q err=%v", line, err)
		}
		return
	}
	if !finishFinalSaving(t.Context()) {
		t.Fatal("the completed upload was reported as a failure")
	}
	if releases.Load() != 1 {
		t.Fatalf("relay releases=%d", releases.Load())
	}
	if _, _, err := a.SendLease(c.ID); !os.IsNotExist(err) {
		t.Fatalf("sender hold survived quit: %v", err)
	}
	view, err := b.Dir.Cell(t.Context(), c.ID)
	if err != nil || view.Cell.Lease.ReleasedAt == 0 {
		t.Fatalf("relay has no released chat: %+v %v", view, err)
	}
	head, err := cellstore.Head(c)
	if err != nil || head == nil || view.Cell.Head != head.Turn.ID {
		t.Fatalf("final head was not published: %v", err)
	}
	engineB := cellstore.EngineFor("")
	engineB.DataRoot = t.TempDir()
	copies := t.TempDir()
	taker := takeover{b.Continuer(engineB, syncsetup.TakeOptions{DeviceName: "receiver", RootFor: func(id string) string { return filepath.Join(copies, id) }})}
	landInput, landTyped := io.Pipe()
	defer landInput.Close()
	defer landTyped.Close()
	landScreen := &surfaceFrameWriter{ready: make(chan struct{})}
	landCtx, landCancel := context.WithCancel(t.Context())
	defer landCancel()
	landDone := make(chan error, 1)
	go func() {
		landDone <- tui3.Run(landCtx, tui3.Options{Agent: &quietAgent{}, Workspace: t.TempDir(), SessionFile: "/new/transcript.jsonl", Landing: true, Machines: surfaceMachines{b}, Takeover: taker, Input: landInput, Output: landScreen, Width: 100, Height: 30, Open: func(where, file string) (tui3.Conversation, error) {
			return tui3.Conversation{Agent: &quietAgent{}, Workspace: where, SessionFile: file}, nil
		}})
	}()
	deadline = time.Now().Add(20 * time.Second)
	for !strings.Contains(landScreen.String(), "Moved from sender") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	landCancel()
	<-landDone
	if !strings.Contains(landScreen.String(), "Moved from sender") {
		t.Fatalf("bare launch did not move the chat: %s", landScreen.String())
	}
}
