package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

var r10RestartRounds = flag.Int("r10-restart-rounds", 2, "consecutive relay restarts in the R10 terminal walk")

// Two cycles in one live terminal, repeated five times by the contract shard,
// cover ten restart/new/title/release/!/model sequences with the real seam.
func TestIdleRelease_R10_RealTerminalRelayRestartNewThenSend(t *testing.T) {
	r10TerminalSequence(t, *r10RestartRounds, false)
}

func TestIdleRelease_R10b_RealTerminalLostReplyRecoversAfterLapse(t *testing.T) {
	r10TerminalSequence(t, 1, true)
}

func r10TerminalSequence(t *testing.T, rounds int, loseReply bool) {
	t.Helper()
	var clockOffset atomic.Int64
	now := func() time.Time { return time.Now().Add(time.Duration(clockOffset.Load())) }
	store := t.TempDir()
	var service atomic.Pointer[relayserve.Service]
	service.Store(relayserve.New(relayserve.Config{Store: store, Now: now}))
	t.Cleanup(func() {
		if s := service.Swap(nil); s != nil {
			_ = s.Close()
		}
	})
	var slow atomic.Int32
	h := newHostedMoveRig(t, func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s := service.Load()
			if s == nil {
				http.Error(w, "restarting", http.StatusServiceUnavailable)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/acquire") {
				mode := slow.Swap(0)
				if mode != 0 {
					reply := httptest.NewRecorder()
					s.Handler.ServeHTTP(reply, r.WithContext(context.WithoutCancel(r.Context())))
					if mode == 2 && reply.Code == http.StatusOK {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err == nil {
							_ = conn.Close()
						}
						return
					}
					select {
					case <-time.After(2700 * time.Millisecond):
					case <-r.Context().Done():
						return
					}
					for k, values := range reply.Header() {
						w.Header()[k] = values
					}
					w.WriteHeader(reply.Code)
					_, _ = io.Copy(w, reply.Body)
					return
				}
			}
			s.Handler.ServeHTTP(w, r)
		})
	})
	t.Setenv("CODEAF_NO_UPDATE_CHECK", "1")
	t.Setenv("CODEAF_ENGINE_DAEMON", "1")
	t.Setenv("CODEAF_PROFILE_DIR", h.a.Home)
	t.Setenv("OPENROUTER_API_KEY", "stub")
	t.Setenv(syncsetup.IntervalVar, "5000")
	if err := h.b.Rename(t.Context(), "other computer"); err != nil {
		t.Fatal(err)
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = io.WriteString(w, `{"data":[{"id":"stub/chat","name":"Stub","context_length":131072,"pricing":{"prompt":"0","completion":"0"}}]}`)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"r10 terminal answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer model.Close()
	t.Setenv("CODEAF_BASE_URL", model.URL)
	proc, err := openV3ProcessWith("chat", true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { proc.closeAll(); syncDrives.closeAll() }()
	seed := v3Options{Model: "stub/chat", Workspace: h.work, Interactive: true, Yolo: true, OneModel: true}
	launch, err := openV3Launch(proc, seed)
	if err != nil {
		t.Fatal(err)
	}
	seam := &v3Seam{proc: proc, boot: launch, seed: seed}
	initial, err := seam.open(launch, launch.Config, false)
	if err != nil {
		t.Fatal(err)
	}
	var current atomic.Pointer[tui3.Conversation]
	current.Store(&initial)
	started := make(chan *tui3.Conversation, 1)
	start := func(workspace string) (tui3.Conversation, error) {
		conv, err := seam.start(workspace)
		if err == nil {
			current.Store(&conv)
			started <- &conv
		}
		return conv, err
	}
	input, typed := io.Pipe()
	screen := &surfaceFrameWriter{ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	previous := runSurfaceProgram
	runSurfaceProgram = func(ctx context.Context, o tui3.Options) error {
		o.Input, o.Output, o.Width, o.Height = input, screen, 120, 40
		return tui3.Run(ctx, o)
	}
	defer func() { runSurfaceProgram = previous }()
	noRestore := &walkNoRestore{attempted: make(chan struct{}, 1)}
	go func() {
		done <- runSurface(ctx, tui3.Options{Agent: initial.Agent, Workspace: initial.Workspace, SessionFile: initial.SessionFile, Owned: initial.Owned, Seal: initial.Seal, ProfileDir: h.a.Home, Start: start, Machines: surfaceMachines{h.a}, Takeover: noRestore, Input: input, Output: screen, Width: 120, Height: 40})
	}()
	defer func() { cancel(); _ = input.Close(); _ = typed.Close(); <-done }()
	select {
	case <-screen.ready:
	case <-time.After(10 * time.Second):
		t.Fatal("no terminal frame")
	}
	uncertainSeen := false
	send := func(text string, model bool) {
		t.Helper()
		conv := current.Load()
		before, _ := os.ReadFile(conv.SessionFile)
		frame := len(screen.String())
		if _, err := io.WriteString(typed, text+"\r"); err != nil {
			t.Fatal(err)
		}
		retried := false
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			raw, _ := os.ReadFile(conv.SessionFile)
			tail := ""
			if len(raw) >= len(before) {
				tail = string(raw[len(before):])
			}
			if !model && strings.Contains(tail, "r10-runs.txt") {
				return
			}
			users, answered := 0, false
			for _, line := range strings.Split(tail, "\n") {
				var record struct {
					Role    string
					Content json.RawMessage
				}
				if json.Unmarshal([]byte(line), &record) != nil {
					continue
				}
				var content string
				_ = json.Unmarshal(record.Content, &content)
				if record.Role == "user" && content == text {
					users++
				}
				if users > 0 && record.Role == "assistant" && strings.Contains(string(record.Content), "r10 terminal answer") {
					answered = true
				}
			}
			if users > 1 {
				t.Fatalf("model send recorded %d times", users)
			}
			if model && users == 1 && answered {
				return
			}
			if !retried && strings.Contains(screen.String()[min(frame, len(screen.String())):], "couldn't confirm taking this chat back from the sync service; try again") {
				if users != 0 {
					t.Fatal("uncertain take-back recorded the draft before admission")
				}
				uncertainSeen = true
				// Only the relay fixture clock advances. The real 90-second
				// lease must lapse before this window can acquire another fence.
				clockOffset.Add(int64(directory.LeaseTTL + time.Millisecond))
				// Renderer output is incremental, so its appended bytes are not
				// a complete screen. Enter without retyping must send exactly
				// the retained draft, checked against the real journal above.
				_, _ = io.WriteString(typed, "\r")
				retried = true
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("terminal did not execute %q: %s", text, screen.String())
	}
	released := func(after uint64) directory.CellView {
		t.Helper()
		id := filepath.Base(session.DirOf(current.Load().SessionFile))
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			view, err := h.a.Dir.Cell(t.Context(), id)
			if err == nil && view.Cell.Lease.Fence > after && view.Cell.Lease.Expires == 0 && view.Cell.Head != "" {
				return view
			}
			time.Sleep(40 * time.Millisecond)
		}
		t.Fatalf("terminal did not release: %s", screen.String())
		return directory.CellView{}
	}
	if rounds < 1 || rounds > 10 {
		t.Fatal("R10 terminal rounds must be between 1 and 10")
	}
	for round := range rounds {
		old := service.Swap(nil)
		if err := old.Close(); err != nil {
			t.Fatal(err)
		}
		service.Store(relayserve.New(relayserve.Config{Store: store, Now: now}))
		if _, err := io.WriteString(typed, "/new\r"); err != nil {
			t.Fatal(err)
		}
		select {
		case <-started:
		case <-time.After(30 * time.Second):
			t.Fatalf("/new failed: %s", screen.String())
		}
		send(fmt.Sprintf("r10 title round %d", round), true)
		first := released(0)
		if title := current.Load().Agent.Title(); strings.TrimSpace(title) == "" {
			t.Fatal("untitled chat")
		}
		slow.Store(1)
		send(fmt.Sprintf("! printf 'round-%d\\n' >> r10-runs.txt; printf 'node_modules/\\n' > .gitignore; mkdir -p node_modules/x; printf kept > node_modules/x/index.js", round), false)
		second := released(first.Cell.Lease.Fence)
		// The separate lost-reply case advances the fixture past lease expiry;
		// the repeated sequence uses a slow reply to check the write budget.
		if loseReply {
			slow.Store(2)
		} else {
			slow.Store(1)
		}
		send(fmt.Sprintf("r10 ordinary model round %d", round), true)
		third := released(second.Cell.Lease.Fence)
		if loseReply && (!uncertainSeen || third.Cell.Lease.Fence != second.Cell.Lease.Fence+2) {
			t.Fatalf("uncertain take-back skipped refusal or lapsed reacquire: uncertain=%v fences=%d/%d", uncertainSeen, second.Cell.Lease.Fence, third.Cell.Lease.Fence)
		}
		if noRestore.calls.Load() != 0 {
			t.Fatal("take-back restored the workspace")
		}
		if raw, err := os.ReadFile(filepath.Join(h.work, "node_modules/x/index.js")); err != nil || string(raw) != "kept" {
			t.Fatalf("ignored install=%q error=%v", raw, err)
		}
		for _, forbidden := range []string{"Continue this chat here?", "submit failed: This chat is available to continue here.", "Moved here", "Set this machine up like"} {
			if strings.Contains(screen.String(), forbidden) {
				t.Fatalf("terminal showed %q", forbidden)
			}
		}
		raw, err := os.ReadFile(filepath.Join(h.work, "r10-runs.txt"))
		if err != nil || strings.Count(string(raw), fmt.Sprintf("round-%d\n", round)) != 1 {
			t.Fatalf("shell execution count: %q %v", raw, err)
		}
		t.Logf("R10 restart/new/title/release/!/model cycle %d completed: fences %d/%d/%d", round+1, first.Cell.Lease.Fence, second.Cell.Lease.Fence, third.Cell.Lease.Fence)
	}
}
