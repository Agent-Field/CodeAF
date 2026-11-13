package cellstore

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/executor"
)

type transportFunc func(context.Context, Target, Op) ([]byte, error)

func (f transportFunc) Do(ctx context.Context, target Target, op Op) ([]byte, error) {
	return f(ctx, target, op)
}

// TestU1SealAcknowledgesReadsAndBoundariesRequireDurability proves U1/C28:
// a fast seal releases completed reads without fsync; movement, upload, close,
// exit and gc reject an engine that cannot confirm durable storage.
func TestU1SealAcknowledgesReadsAndBoundariesRequireDurability(t *testing.T) {
	for _, boundary := range []string{"move", "upload", "close", "exit", "gc"} {
		t.Run(boundary, func(t *testing.T) {
			c := newCell(t)
			e := Engine{DataRoot: t.TempDir()}
			var confirm atomic.Bool
			e.Transport = transportFunc(func(_ context.Context, _ Target, op Op) ([]byte, error) {
				switch o := op.(type) {
				case capturePlanOp:
					return []byte(`{"token":"screen-this","full":true,"paths":[]}`), nil
				case sealOp:
					if o.Durable || o.Plan == "" {
						t.Fatalf("seal lost fast token-bound semantics: %+v", o)
					}
					return []byte(`{"snapshot":"saved","durable":false}`), nil
				case syncOp:
					return json.Marshal(map[string]bool{"durable": confirm.Load()})
				default:
					return nil, errors.New("unexpected " + op.Verb())
				}
			})
			if _, err := e.Seal(t.Context(), c, TurnInfo{}); err != nil {
				t.Fatal(err)
			}
			check := func() error {
				switch boundary {
				case "upload":
					_, err := e.Sync(SyncKeys{}, "test").Export(t.Context(), c, "saved")
					return err
				case "close":
					r, err := NewRecorder(&stubExec{}, e, c, e.WALPath(c), Options{})
					if err != nil {
						return err
					}
					return r.Drain(t.Context())
				case "exit":
					return Flush(t.Context())
				case "gc":
					_, _, err := e.CaptureForGC(t.Context(), c)
					return err
				default:
					return Await(t.Context(), c)
				}
			}
			if err := check(); err == nil || !strings.Contains(err.Error(), "durable") {
				t.Fatalf("%s hid engine durability failure: %v", boundary, err)
			}
			confirm.Store(true)
			if err := Await(t.Context(), c); err != nil {
				t.Fatalf("durability debt did not retry: %v", err)
			}
			confirm.Store(false)
			if err := Await(t.Context(), c); err == nil {
				t.Fatal("a later boundary did not require a new durable acknowledgement")
			}
			confirm.Store(true)
			if err := Await(t.Context(), c); err != nil {
				t.Fatal(err)
			}

		})
	}
}

// TestC9TokenRefusalScreensAgainBeforeCapturing proves C9/U2 against the real
// engine: a background writer changing a screened source cannot race a clean
// plan into the store. The token refusal must trigger a new screen, never an
// unplanned fallback.
func TestC9TokenRefusalScreensAgainBeforeCapturing(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		r := newRig(t)
		e := realEngine(t)
		e.Workspace, e.Guard = r.tree, r.guard
		r.write("config.py", "safe = True\n")
		r.write("safe.txt", "C9_SAFE_CAPTURE_MARKER")
		tr := e.Transport
		changed := false
		plans := 0
		e.Transport = transportFunc(func(ctx context.Context, target Target, op Op) ([]byte, error) {
			if op.Verb() == "capture-plan" {
				plans++
			}
			if o, ok := op.(sealOp); ok {
				if o.Plan == "" {
					t.Fatal("capture bypassed token")
				}
				if !changed {
					changed = true
					r.write("config.py", "key = '"+fakeKey+"'\n")
				}
			}
			return tr.Do(ctx, target, op)
		})
		if _, err := e.Seal(t.Context(), r.cell, TurnInfo{}); err != nil {
			t.Fatal(err)
		}
		if plans < 2 {
			t.Fatal("stale token did not re-plan")
		}
		assertC9Screened(t, e, r, "config.py", fakeKey)
	})
}

// TestC9CommandsBackgroundWritersAndEditorsStayOutOfUploads proves C9/U2:
// each real writing door is followed by capture and encrypted export/import;
// the receiving workspace contains the safe marker and no withheld secret.
func TestC9CommandsBackgroundWritersAndEditorsStayOutOfUploads(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		for _, writer := range []string{"command", "background between commands", "outside editor"} {
			t.Run(writer, func(t *testing.T) {
				r := newRig(t)
				real := realEngine(t)
				e := EngineFor(r.tree)
				e.Binary, e.Transport, e.DataRoot = real.Binary, real.Transport, real.DataRoot
				e.Guard.Home, e.Guard.Notify, e.Guard.Kept = r.guard.Home, r.guard.Notify, r.guard.Kept
				r.write("safe.txt", "C9_SAFE_CAPTURE_MARKER")
				if _, err := e.Seal(t.Context(), r.cell, TurnInfo{}); err != nil {
					t.Fatal(err)
				}
				seat, err := SeatOver(executor.HostBound, r.cell, r.tree, nil, nil, nil, func(at *Engine) { *at = e })
				if err != nil {
					t.Fatal(err)
				}
				write := func() { r.write("src/config.py", "key = '"+fakeKey+"'\n") }
				switch writer {
				case "command":
					err = seat.Around(t.Context(), executor.Call{Tool: "write", Changed: []string{"src/config.py"}}, func() ([]byte, bool) { write(); return nil, false })
				case "background between commands":
					done := make(chan struct{})
					go func() { write(); close(done) }()
					<-done
					err = seat.Around(t.Context(), executor.Call{Tool: "read", Changed: []string{}}, func() ([]byte, bool) { return nil, false })
				case "outside editor":
					write()
					_, err = e.Capture(t.Context(), r.cell)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err = executor.Drain(t.Context(), seat); err != nil {
					t.Fatal(err)
				}
				assertC9Screened(t, e, r, "src/config.py", fakeKey)
				snapshot, err := e.Capture(t.Context(), r.cell)
				if err != nil {
					t.Fatal(err)
				}
				sync := e.Sync(testKeys(), LedgerName("http://local.invalid", "c9-uploads"))
				exported, err := sync.Export(t.Context(), r.cell, snapshot)
				if err != nil {
					t.Fatal(err)
				}
				other := realEngine(t)
				arrived := newCell(t)
				inbound := other.Sync(testKeys(), LedgerName("http://local.invalid", "c9-uploads"))
				inbox := t.TempDir()
				for _, frame := range exported.Frames {
					raw, err := os.ReadFile(frame.Path)
					if err != nil {
						t.Fatal(err)
					}
					_, objects, err := blobstore.Decode(raw)
					if err != nil {
						t.Fatal(err)
					}
					for _, object := range objects {
						if err := os.WriteFile(filepath.Join(inbox, object.RID), object.Bytes, 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				if _, err := inbound.ImportPrimed(t.Context(), arrived, snapshot, inbox); err != nil {
					t.Fatal(err)
				}
				if len(exported.Frames) == 0 {
					t.Fatal("fixture uploaded no frames")
				}
				if err := inbound.Materialize(t.Context(), arrived, snapshot); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(arrived.Root, "src/config.py")); !os.IsNotExist(err) {
					t.Fatalf("secret reached receiving workspace: %v", err)
				}
				if raw, err := os.ReadFile(filepath.Join(arrived.Root, "safe.txt")); err != nil || string(raw) != "C9_SAFE_CAPTURE_MARKER" {
					t.Fatalf("safe workspace did not arrive: %q %v", raw, err)
				}
			})
		}
	})
}

// TestU3ChatsUseCanonicalFolderAndStableSharedRouting proves U3/P4: relative
// aliases use one folder identity and shared store while timeline IDs remain
// private and stable. No new engine field is needed for folder-scoped stat rows.
func TestU3ChatsUseCanonicalFolderAndStableSharedRouting(t *testing.T) {
	tree := t.TempDir()
	e := Engine{Workspace: filepath.Join(tree, "nested", ".."), DataRoot: t.TempDir()}
	a, b := newCell(t), newCell(t)
	first, second := e.target(a), e.target(b)
	if first.Tree != canonicalTree(tree) || second.Tree != first.Tree || first.DataDir != second.DataDir {
		t.Fatalf("folder/store identity diverged: %+v %+v", first, second)
	}
	if first.Timeline != a.ID || second.Timeline != b.ID || first.Timeline == second.Timeline || e.target(a) != first {
		t.Fatal("timeline identity drifted")
	}
	if err := os.Mkdir(filepath.Join(tree, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
}

// TestU2PlanFailureNeverCapturesUnscreenedContent proves U2/C9: a missing
// planning capability is a save failure, never permission to capture directly.
func TestU2PlanFailureNeverCapturesUnscreenedContent(t *testing.T) {
	c := newCell(t)
	e := Engine{DataRoot: t.TempDir(), Transport: transportFunc(func(_ context.Context, _ Target, op Op) ([]byte, error) {
		if op.Verb() != "capture-plan" {
			t.Fatalf("unexpected capture after refused plan: %s", op.Verb())
		}
		return nil, errors.New("planning refused")
	})}
	if _, err := e.Seal(t.Context(), c, TurnInfo{}); err == nil {
		t.Fatal("planning error hidden")
	}
	if head, err := Head(c); err != nil || head != nil {
		t.Fatalf("refused plan advanced head: %+v %v", head, err)
	}
}

// TestU1InterruptedAnswerNeverReplaysACapture proves U1/C28: once a daemon
// accepted a request, losing its answer cannot cause a fallback mutation.
func TestU1InterruptedAnswerNeverReplaysACapture(t *testing.T) {
	socket := testSocket(t)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
	}()
	fallback := &fixedTransport{out: `{"snapshot":"duplicate"}`}
	_, err = (Fallback{Primary: Daemon{Socket: socket}, Secondary: fallback}).Do(t.Context(), Target{}, sealOp{Plan: "single-use"})
	<-done
	if err == nil || errors.Is(err, ErrUnavailable) || fallback.calls != 0 {
		t.Fatalf("lost answer replayed capture: %v, calls=%d", err, fallback.calls)
	}
}

// TestC9UnlistableDirectoryIsWithheldBeforePlanningAndRechecked proves C9/U2:
// an unreadable folder cannot block safe work or enter a plan unscreened. When
// permissions recover, a fresh plan screens its newly eligible secret files.
func TestC9UnlistableDirectoryIsWithheldBeforePlanningAndRechecked(t *testing.T) {
	skipIfRoot(t)
	forEachTransport(t, func(t *testing.T) {
		r := newRig(t)
		e := realEngine(t)
		e.Workspace, e.Guard = r.tree, r.guard
		r.write("safe.txt", "C9_SAFE_CAPTURE_MARKER")
		r.write("locked/.env", "PRIVATE=c9-kept-in-unlistable-folder")
		r.chmod("locked", 0)
		if _, err := e.Seal(t.Context(), r.cell, TurnInfo{}); err != nil {
			t.Fatal(err)
		}
		policy, err := readPolicyAt(e.policyDir(r.cell))
		if err != nil || !policy.isUnreadable("locked") {
			t.Fatalf("unlistable directory not withheld: %+v %v", policy, err)
		}
		r.chmod("locked", 0700)
		if _, err := e.Seal(t.Context(), r.cell, TurnInfo{Changed: []string{}}); err != nil {
			t.Fatal(err)
		}
		policy, err = readPolicyAt(e.policyDir(r.cell))
		if err != nil || policy.isUnreadable("locked") || !policy.isWithheld("locked/.env") {
			t.Fatalf("recovered directory bypassed fresh screen: %+v %v", policy, err)
		}
		if hits := grepStore(t, e.StoreDir(r.cell), "c9-kept-in-unlistable-folder"); len(hits) != 0 {
			t.Fatalf("secret reached the store: %v", hits)
		}
	})
}

// TestC9GitHistoryChangedOutsideAToolStillTravels proves C9/U3: a command
// whose known paths are empty must still carry Git commits made between
// calls, while every chat keeps its stable private timeline.
func TestC9GitHistoryChangedOutsideAToolStillTravels(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		r := newRig(t)
		real := realEngine(t)
		e := EngineFor(r.tree)
		e.Binary, e.Transport, e.DataRoot = real.Binary, real.Transport, real.DataRoot
		e.Guard.Home, e.Guard.Notify = r.guard.Home, r.guard.Notify
		r.write("source.txt", "first")
		commitPaths(t, r.tree, "source.txt")
		if _, err := e.Seal(t.Context(), r.cell, TurnInfo{}); err != nil {
			t.Fatal(err)
		}
		r.write("source.txt", "second")
		commitPaths(t, r.tree, "source.txt")
		want, err := gitIn(r.tree, "rev-parse", "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := e.Seal(t.Context(), r.cell, TurnInfo{Changed: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		arrived := newCell(t)
		receiver := e
		receiver.Workspace = t.TempDir()
		if err := receiver.Sync(SyncKeys{}, LedgerName("http://local.invalid", "git")).Materialize(t.Context(), arrived, sealed.Turn.ID); err != nil {
			t.Fatal(err)
		}
		got, err := gitIn(receiver.tree(arrived), "rev-parse", "HEAD")
		if err != nil || got != want {
			t.Fatalf("commits made outside the hinted call did not travel: %q %q %v", got, want, err)
		}
	})
}

// TestU1ShutdownJoinsOwnedExitBeforeReleasingTheStore proves U1/C28/U4:
// a shutdown acknowledgement is not yet a closed catalog. The caller closes
// its control connection, then joins the owned process without a timed sleep.
func TestU1ShutdownJoinsOwnedExitBeforeReleasingTheStore(t *testing.T) {
	socket := testSocket(t)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	exited := make(chan struct{})
	daemonExits.Store(socket, (<-chan struct{})(exited))
	defer daemonExits.Delete(socket)
	connectionClosed := make(chan struct{})
	go func() {
		defer close(connectionClosed)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		raw, _ := bufio.NewReader(conn).ReadBytes('\n')
		var request wireRequest
		_ = json.Unmarshal(raw, &request)
		response, _ := json.Marshal(wireResponse{ID: request.ID, Ok: json.RawMessage(`{"status":"stopping"}`)})
		_, _ = conn.Write(append(response, '\n'))
		var probe [1]byte
		_, _ = conn.Read(probe[:])
	}()
	stopped := make(chan error, 1)
	go func() { stopped <- (Daemon{Socket: socket}).Stop(t.Context()) }()
	<-connectionClosed
	select {
	case err := <-stopped:
		t.Fatalf("shutdown returned before owned exit: %v", err)
	default:
	}
	close(exited)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}
