package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

type tailPublishGate struct {
	armed         atomic.Bool
	committed     chan struct{}
	reply         chan struct{}
	canceled      atomic.Int32
	conflicts     atomic.Int32
	down          atomic.Bool
	titleRelease  chan struct{}
	costRelease   chan struct{}
	costRequested chan struct{}
	costOnce      atomic.Bool
}

func (g *tailPublishGate) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.down.Load() && r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/store/") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/publish") {
			next.ServeHTTP(w, r)
			return
		}
		reply := httptest.NewRecorder()
		next.ServeHTTP(reply, r)
		if reply.Code == http.StatusConflict {
			g.conflicts.Add(1)
		}
		if reply.Code == http.StatusOK && g.armed.CompareAndSwap(true, false) {
			close(g.committed)
			select {
			case <-g.reply:
			case <-r.Context().Done():
				g.canceled.Add(1)
				return
			}
		}
		for key, values := range reply.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(reply.Code)
		_, _ = w.Write(reply.Body.Bytes())
	})
}

func releaseTailGate(gate chan struct{}) {
	if gate == nil {
		return
	}
	select {
	case <-gate:
	default:
		close(gate)
	}
}

type hostedTailChat struct {
	*hostedMoveRig
	client    *remote.Client
	closing   chan struct{}
	closeOnce sync.Once
	seat      executor.Seat
}

func newHostedTailChat(t *testing.T, g *tailPublishGate) *hostedTailChat {
	t.Helper()
	h := newHostedMoveRig(t, g.wrap)
	h.ea.DataRoot = ""
	if err := os.Remove(session.Place{Dir: h.c.Root}.Transcript()); err != nil {
		t.Fatal(err)
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/generation") && g.costRelease != nil {
			close(g.costRequested)
			select {
			case <-g.costRelease:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"tokens_prompt":924,"tokens_completion":604,"total_cost":0.000345232}}`)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var request struct {
			Stream   bool              `json:"stream"`
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		title := false
		for _, message := range request.Messages {
			title = title || strings.Contains(string(message), "You name conversations.")
		}
		if title && g.titleRelease != nil {
			select {
			case <-g.titleRelease:
			case <-r.Context().Done():
				return
			}
		}
		if title && g.costRelease != nil && g.costOnce.CompareAndSwap(false, true) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"id\":\"late-title-cost\",\"choices\":[{\"delta\":{\"content\":\"partial title\"}}]}\n\ndata: malformed\n\n")
			return
		}
		words := "saved answer"
		if title {
			words = "a saved conversation title"
		}
		if !request.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"`+words+`"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":2}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\""+words+"\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(model.Close)
	chat := &hostedTailChat{hostedMoveRig: h, closing: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- enginehost.Run(h.work, enginehost.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			cfg := v3Seated(session.Config{Workspace: h.work, Place: session.Place{Dir: h.c.Root, Workspace: h.work}, SessionFile: session.Place{Dir: h.c.Root}.Transcript(), Model: "stub/chat", APIKey: "stub", BaseURL: model.URL})
			chat.mu.Lock()
			chat.seat = cfg.Seat
			chat.mu.Unlock()
			agent, err := session.New(cfg)
			if err != nil {
				return nil, err
			}
			return &remote.Engine{Agent: agent, Workspace: h.work, SessionFile: agent.SessionPath(), Place: cfg.Place, ReleaseOnLeave: func(remote.WrappedAgent) bool { return true }, FinishSaving: func(remote.WrappedAgent) error {
				chat.closeOnce.Do(func() { close(chat.closing) })
				return syncDrives.closeCell(h.c.ID)
			}}, nil
		}})
	}()
	t.Cleanup(func() { _ = enginehost.Retire(h.work, true); <-done })
	chat.connect()
	t.Cleanup(func() { releaseTailGate(g.reply); releaseTailGate(g.titleRelease); releaseTailGate(g.costRelease) })
	return chat
}

func (h *hostedTailChat) connect() {
	h.t.Helper()
	var conn net.Conn
	h.wait("host socket", func() bool { var err error; conn, err = enginehost.Dial(h.work); return err == nil })
	client, err := remote.Dial(conn, "", remote.Hello{})
	if err != nil {
		h.t.Fatal(err)
	}
	h.client = client
	h.t.Cleanup(func() { _ = client.Close() })
}

func (h *hostedTailChat) say(words string) {
	h.t.Helper()
	events, err := h.client.Agent().Submit(h.t.Context(), words)
	if err != nil {
		h.t.Fatal(err)
	}
	for event := range events {
		if event.Kind == session.EventError {
			h.t.Fatal(event.Text)
		}
	}
}

func (h *hostedTailChat) saved() bool {
	head, err := cellstore.Head(h.c)
	if err != nil || head == nil {
		return false
	}
	view, err := h.a.Dir.Cell(h.t.Context(), h.c.ID)
	return err == nil && head.Turn.ID == view.Cell.Head
}

func (h *hostedTailChat) quit(g *tailPublishGate) string {
	return h.quitAfter(g, 3*time.Second)
}

func (h *hostedTailChat) quitAfter(g *tailPublishGate, delay time.Duration) string {
	h.t.Helper()
	oldRun, oldOut, oldFlush := runSurfaceProgram, os.Stdout, flushFinalSaves
	out, err := os.CreateTemp(h.t.TempDir(), "quit-output")
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { out.Close() })
	os.Stdout = out
	flushFinalSaves = cellstore.Flush
	defer func() { runSurfaceProgram, os.Stdout, flushFinalSaves = oldRun, oldOut, oldFlush }()
	finalSaving.Lock()
	finalSaving.failure, finalSaving.pending, finalSaving.forced = nil, nil, nil
	finalSaving.Unlock()
	input, typed := io.Pipe()
	defer input.Close()
	defer typed.Close()
	screen := &surfaceFrameWriter{ready: make(chan struct{})}
	runSurfaceProgram = func(ctx context.Context, opt tui3.Options) error {
		opt.Input, opt.Output, opt.Width, opt.Height = input, screen, 100, 30
		return tui3.Run(ctx, opt)
	}
	finished := make(chan error, 1)
	ctx, cancel := context.WithTimeout(h.t.Context(), cellstore.SealWithin)
	defer cancel()
	go func() {
		finished <- runSurface(ctx, tui3.Options{Agent: h.client.Agent(), Workspace: h.work, SessionFile: session.Place{Dir: h.c.Root}.Transcript()})
	}()
	<-screen.ready
	if _, err := io.WriteString(typed, "/quit\r"); err != nil {
		h.t.Fatal(err)
	}
	select {
	case <-h.closing:
	case <-ctx.Done():
		h.t.Fatal("quit did not reach its final save")
	}
	// The committed response arrives while quit is joining the writer.
	time.Sleep(delay)
	releaseTailGate(g.reply)
	if err := <-finished; err != nil {
		h.t.Fatal(err)
	}
	finishFinalSaving(ctx)
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		h.t.Fatal(err)
	}
	raw, err := io.ReadAll(out)
	if err != nil {
		h.t.Fatal(err)
	}
	return string(raw)
}

func quittingTailChat(t *testing.T) (*hostedTailChat, *tailPublishGate, string) {
	t.Helper()
	g := &tailPublishGate{committed: make(chan struct{}), reply: make(chan struct{})}
	h := newHostedTailChat(t, g)
	h.say("Remember this question.")
	h.wait("first saved answer", h.saved)
	g.armed.Store(true)
	h.say("Finish this turn before I quit.")
	select {
	case <-g.committed:
	case <-time.After(cellstore.SealWithin):
		t.Fatal("next publish never committed")
	}
	output := h.quit(g)
	return h, g, output
}

// Contract 1 requires quitting after work to save the latest head quietly and release its hold.
func TestHostedQuitAfterWorkSavesNewestHeadQuietly(t *testing.T) {
	h, _, output := quittingTailChat(t)
	if output != "" {
		t.Errorf("successful quit printed %q", output)
	}
	if !h.saved() {
		t.Error("quit left the newest sealed head local")
	}
	if _, _, err := h.a.SendLease(h.c.ID); !os.IsNotExist(err) {
		t.Errorf("quit left its hold: %v", err)
	}
	view, err := h.a.Dir.Cell(t.Context(), h.c.ID)
	if err != nil || view.Cell.Lease.ReleasedAt == 0 {
		t.Errorf("quit did not release the relay hold: %+v, %v", view, err)
	}
}

// Contract 2 requires a closing publish to join a committed request without conflicting with itself.
func TestHostedQuitJoinsCommittedPublishWithoutSelfConflict(t *testing.T) {
	_, gate, _ := quittingTailChat(t)
	if got := gate.conflicts.Load(); got != 0 {
		t.Fatalf("one computer conflicted with itself %d times", got)
	}
	if got := gate.canceled.Load(); got != 0 {
		t.Fatalf("quit discarded %d committed publish replies", got)
	}
}

// Contract 3 requires the publication record to name the head accepted by the relay.
func TestHostedQuitRecordsTheHeadAcceptedByRelay(t *testing.T) {
	h, _, _ := quittingTailChat(t)
	raw, err := os.ReadFile(filepath.Join(h.c.Root, cell.StateDir, "published.json"))
	if err != nil {
		t.Fatal(err)
	}
	var published struct {
		Head string `json:"head"`
	}
	if err := json.Unmarshal(raw, &published); err != nil {
		t.Fatal(err)
	}
	view, err := h.a.Dir.Cell(t.Context(), h.c.ID)
	if err != nil || published.Head != view.Cell.Head {
		t.Fatalf("publication record=%s, relay=%s: %v", published.Head, view.Cell.Head, err)
	}
}

// Contract 6 requires the first take back to restore the newest history without a kept branch.
func TestHostedQuitThenTakeBackRestoresNewestHistoryFirstTry(t *testing.T) {
	h, _, _ := quittingTailChat(t)
	rootB := func(id string) string { return filepath.Join(h.bRoots, id) }
	got, err := h.taker(h.b, h.eb, rootB, nil).Take(t.Context(), h.c.ID)
	if err != nil {
		t.Fatal(err)
	}
	at, err := cell.OpenAt(session.DirOf(got.Transcript), h.c.ID)
	if err != nil {
		t.Fatal(err)
	}
	other := h.publish(h.b, h.eb, at, "other computer")
	if err := other.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	back, err := h.a.Continuer(h.ea, syncsetup.TakeOptions{RootFor: func(string) string { return h.c.Root }}).Take(t.Context(), h.c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Taken.Kept != "" || back.KeptTurns != 0 {
		t.Fatalf("take back created a branch: %+v", back)
	}
	if !h.saved() {
		t.Fatal("take back did not restore the newest history")
	}
}

// Contract 1b requires a successful saving reply to remain awaited past the ordinary request deadline.
func TestHostedQuitWaitsForSavingPastOrdinaryCallDeadline(t *testing.T) {
	g := &tailPublishGate{committed: make(chan struct{}), reply: make(chan struct{})}
	h := newHostedTailChat(t, g)
	h.say("Remember this answer.")
	h.wait("first saved answer", h.saved)
	g.armed.Store(true)
	h.say("Save the last answer before leaving.")
	select {
	case <-g.committed:
	case <-time.After(10 * time.Second):
		t.Fatal("publish did not commit")
	}
	output := h.quitAfter(g, 11*time.Second)
	if strings.Contains(output, "could not save") {
		t.Errorf("successful save was reported as failed: %q", output)
	}
	h.wait("completed release", func() bool {
		v, err := h.a.Dir.Cell(t.Context(), h.c.ID)
		return err == nil && v.Cell.Lease.ReleasedAt != 0
	})
	if !h.saved() {
		t.Fatal("long final save lost the newest head")
	}
}

// Contract 5 requires an unchanged launch to leave the turn chain alone, including when another computer holds it.
func TestHostedLaunchNeverSealsAnEmptyTurn(t *testing.T) {
	t.Run("new chat", func(t *testing.T) {
		g := &tailPublishGate{committed: make(chan struct{}), reply: make(chan struct{})}
		h := newHostedTailChat(t, g)
		if err := h.client.Agent().DetachAndWait(); err != nil {
			t.Fatal(err)
		}
		turns, err := cellstore.Turns(h.c)
		if err != nil || len(turns) != 0 {
			t.Fatalf("new launch sealed %d empty turns: %v", len(turns), err)
		}
	})
	for _, moved := range []bool{false, true} {
		t.Run(map[bool]string{false: "held here", true: "held elsewhere"}[moved], func(t *testing.T) {
			g := &tailPublishGate{committed: make(chan struct{}), reply: make(chan struct{})}
			h := newHostedTailChat(t, g)
			h.say("Keep the saved answer.")
			h.wait("saved answer", h.saved)
			if err := h.client.Agent().DetachAndWait(); err != nil {
				t.Fatal(err)
			}
			if moved {
				if _, err := h.taker(h.b, h.eb, func(id string) string { return filepath.Join(h.bRoots, id) }, nil).Take(t.Context(), h.c.ID); err != nil {
					t.Fatal(err)
				}
			}
			before, err := cellstore.Turns(h.c)
			if err != nil {
				t.Fatal(err)
			}
			journal, err := os.ReadFile(session.Place{Dir: h.c.Root}.Transcript())
			if err != nil {
				t.Fatal(err)
			}
			h.connect()
			if err := h.client.Agent().DetachAndWait(); err != nil {
				t.Fatal(err)
			}
			after, err := cellstore.Turns(h.c)
			if err != nil {
				t.Fatal(err)
			}
			if len(after) != len(before) {
				t.Fatalf("unchanged launch added %d empty turns", len(after)-len(before))
			}
			raw, err := os.ReadFile(session.Place{Dir: h.c.Root}.Transcript())
			if err != nil || string(raw) != string(journal) {
				t.Fatalf("viewer changed transcript: %v", err)
			}
		})
	}
}

// Contract 7 requires an auxiliary-only local tail to adopt the remote history without making unsaved edits.
func TestHostedTakeBackNeverBranchesBookkeepingOnlyTurns(t *testing.T) {
	for _, kind := range []string{"auxiliary usage", "reconciled cost"} {
		t.Run(kind, func(t *testing.T) {
			g := &tailPublishGate{committed: make(chan struct{}), reply: make(chan struct{})}
			if kind == "auxiliary usage" {
				g.titleRelease = make(chan struct{})
			} else {
				g.costRelease = make(chan struct{})
				g.costRequested = make(chan struct{})
			}
			h := newHostedTailChat(t, g)
			h.say("Keep this answer across computers.")
			h.wait("shared answer", h.saved)
			g.down.Store(true)
			needle := `"title":"a saved conversation title"`
			if g.titleRelease != nil {
				close(g.titleRelease)
			} else {
				select {
				case <-g.costRequested:
				case <-time.After(10 * time.Second):
					t.Fatal("late receipt was not requested")
				}
				close(g.costRelease)
				needle = `"arm":"reconciled"`
			}
			h.wait("late accounting record", func() bool {
				raw, err := os.ReadFile(session.Place{Dir: h.c.Root}.Transcript())
				return err == nil && strings.Contains(string(raw), needle)
			})
			assertBookkeepingTakeBack(t, h, g)
		})
	}
}

func assertBookkeepingTakeBack(t *testing.T, h *hostedTailChat, g *tailPublishGate) {
	t.Helper()
	output := h.quit(g)
	if !strings.Contains(output, "could not save") {
		raw, _ := os.ReadFile(session.Place{Dir: h.c.Root}.Transcript())
		t.Fatalf("offline fixture did not retain local work: %q\n%s", output, raw)
	}
	if !session.FlushUsage() {
		t.Fatal("usage did not settle before the move")
	}
	ledger, err := os.ReadFile(session.UsageLedgerPath())
	if err != nil || len(ledger) == 0 {
		t.Fatalf("late usage was not kept in the ledger: %v", err)
	}
	g.down.Store(false)
	if _, err := h.taker(h.b, h.eb, func(id string) string { return filepath.Join(h.bRoots, id) }, nil).Take(t.Context(), h.c.ID); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.requests = nil
	h.mu.Unlock()
	got, err := h.a.Continuer(h.ea, syncsetup.TakeOptions{RootFor: func(string) string { return h.c.Root }}).Take(t.Context(), h.c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Taken.Kept != "" || got.KeptTurns != 0 {
		t.Errorf("bookkeeping became unsaved edits: %s (%d turns)", got.Taken.Kept, got.KeptTurns)
	}
	after, err := os.ReadFile(session.UsageLedgerPath())
	if err != nil || string(after) != string(ledger) {
		t.Fatalf("take changed the local usage ledger: %v", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, request := range h.requests {
		path := strings.TrimPrefix(request, "POST /v1/dir/cells/")
		if path != request && !strings.Contains(path, "/") {
			t.Error("bookkeeping created a branch chat")
		}
	}
}

// Contract 4 requires a failed quit to retain its hold and retry on restart while it still owns the chat.
func TestHostedFailedQuitRetriesOnNextStartWhileStillHolding(t *testing.T) {
	g := &tailPublishGate{committed: make(chan struct{}), reply: make(chan struct{}), titleRelease: make(chan struct{})}
	h := newHostedTailChat(t, g)
	h.say("Save this answer on the other computer.")
	h.wait("shared answer", h.saved)
	g.down.Store(true)
	close(g.titleRelease)
	h.wait("late title record", func() bool {
		raw, err := os.ReadFile(session.Place{Dir: h.c.Root}.Transcript())
		return err == nil && strings.Contains(string(raw), `"title":"a saved conversation title"`)
	})
	output := h.quit(g)
	if !strings.Contains(output, "could not save") || !strings.Contains(output, "if this computer still holds the chat") {
		t.Errorf("failed quit made an unconditional recovery promise: %q", output)
	}
	if _, fence, err := h.a.SendLease(h.c.ID); err != nil || fence == 0 {
		t.Fatalf("failed quit lost its hold: %v", err)
	}
	g.down.Store(false)
	h.connect()
	h.wait("restarted publication", h.saved)
	if err := h.client.Agent().DetachAndWait(); err != nil {
		t.Fatal(err)
	}
}

// Contract 4b requires a removed computer to finish its local save quietly without a relay save.
func TestHostedRemovedComputerQuitsQuietlyAndKeepsLocalHistory(t *testing.T) {
	g := &tailPublishGate{committed: make(chan struct{}), reply: make(chan struct{})}
	h := newHostedTailChat(t, g)
	h.say("Keep this answer on this computer.")
	h.wait("shared answer", h.saved)
	if err := h.b.Dir.Revoke(t.Context(), h.a.Device.ID()); err != nil {
		t.Fatal(err)
	}
	_, _ = h.a.Dir.List(t.Context())
	h.wait("removal notice", h.a.Removed)
	h.say("Keep working locally after removal.")
	h.mu.Lock()
	h.requests = nil
	h.mu.Unlock()
	if output := h.quit(g); output != "" {
		t.Errorf("locally saved quit printed %q", output)
	}
	raw, err := os.ReadFile(session.Place{Dir: h.c.Root}.Transcript())
	if err != nil || !strings.Contains(string(raw), "Keep working locally after removal.") {
		t.Fatalf("local history was not kept: %v", err)
	}
	head, err := cellstore.Head(h.c)
	if err != nil || head == nil || head.Receipt.Transcript.End != int64(len(raw)) {
		t.Errorf("removed chat did not finish saving locally: %v", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, request := range h.requests {
		if strings.HasSuffix(request, "/publish") || strings.HasSuffix(request, "/release") || strings.HasPrefix(request, "POST /v1/store/") {
			t.Errorf("removed quit tried a relay save: %s", request)
		}
	}
}

// Contract 7 keeps real edits to a shared message when the chat is taken back.
func TestHostedTakeBackKeepsEditsToSharedMessages(t *testing.T) {
	g := &tailPublishGate{committed: make(chan struct{}), reply: make(chan struct{})}
	h := newHostedTailChat(t, g)
	h.say("Remember the original answer.")
	h.wait("shared answer", h.saved)
	if err := h.client.Agent().DetachAndWait(); err != nil {
		t.Fatal(err)
	}
	path := session.Place{Dir: h.c.Root}.Transcript()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after := strings.Replace(string(before), `"content":"saved answer"`, `"content":"local answer"`, 1)
	if after == string(before) || len(after) != len(before) {
		t.Fatal("fixture did not edit an existing answer in place")
	}
	if err := os.WriteFile(path, []byte(after), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.taker(h.b, h.eb, func(id string) string { return filepath.Join(h.bRoots, id) }, nil).Take(t.Context(), h.c.ID); err != nil {
		t.Fatal(err)
	}
	got, err := h.a.Continuer(h.ea, syncsetup.TakeOptions{RootFor: func(string) string { return h.c.Root }}).Take(t.Context(), h.c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Taken.Kept == "" || got.KeptTurns == 0 {
		t.Fatal("take back discarded the edit to the shared answer")
	}
	branches := t.TempDir()
	kept, err := h.b.Continuer(h.eb, syncsetup.TakeOptions{RootFor: func(id string) string { return filepath.Join(branches, id) }}).OpenBranch(t.Context(), got.Taken.Kept)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(session.Place{Dir: kept.Taken.Cell.Root}.Transcript())
	if err != nil || !strings.Contains(string(raw), `"content":"local answer"`) {
		t.Fatalf("kept chat lost the edited answer: %v", err)
	}
}

// Contracts 1 and 5 keep a late accounting callback from sealing after the writer has released the chat.
func TestHostedLateAccountingCannotSealAfterQuit(t *testing.T) {
	g := &tailPublishGate{committed: make(chan struct{}), reply: make(chan struct{}), costRelease: make(chan struct{}), costRequested: make(chan struct{})}
	h := newHostedTailChat(t, g)
	h.say("Save this answer before leaving.")
	h.wait("shared answer", h.saved)
	select {
	case <-g.costRequested:
	case <-time.After(10 * time.Second):
		t.Fatal("late receipt was not requested")
	}
	if err := h.client.Agent().DetachAndWait(); err != nil {
		t.Fatal(err)
	}
	before, err := cellstore.Turns(h.c)
	if err != nil {
		t.Fatal(err)
	}
	close(g.costRelease)
	h.wait("late accounting", func() bool {
		if !session.FlushUsage() {
			return false
		}
		rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
		if err != nil {
			return false
		}
		for _, row := range rows {
			if row.Input == 924 {
				return true
			}
		}
		return false
	})
	h.mu.Lock()
	seat := h.seat
	h.mu.Unlock()
	executor.Settle(t.Context(), seat)
	if err := cellstore.Await(t.Context(), h.c); err != nil {
		t.Fatal(err)
	}
	after, err := cellstore.Turns(h.c)
	if err != nil || len(after) != len(before) {
		t.Fatalf("late accounting sealed %d turns after release: %v", len(after)-len(before), err)
	}
}
