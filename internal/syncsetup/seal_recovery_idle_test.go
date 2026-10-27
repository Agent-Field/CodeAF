package syncsetup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Sealing recovery needs native captures and a signed relay, without unrelated consensus storage.
func nativeSealingRig(t *testing.T) *twoHomes {
	t.Helper()
	celltest.RequireEngine(t)
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Fatalf("native engine: %v", err)
	}
	server := relay(t)
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(IntervalVar, "50")
	r := &driveRig{t: t, bin: bin, work: t.TempDir()}
	r.a = openSync(t, machine(t, server.URL))
	r.cell, err = cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	mustNil(t, err)
	r.engine = cellstore.EngineFor(r.work)
	r.engine.DataRoot, r.engine.Binary, r.engine.Transport = t.TempDir(), bin, cellstore.Spawn{Binary: bin}
	r.sealMeta()
	t.Cleanup(cellstore.StopSpawned)
	t.Cleanup(session.CloseUsage)
	return &twoHomes{driveRig: r}
}

// Contract E1 carries recovery after a failed first seal, two good seals and publication.
func TestPublishedRecoveryReachesAnIdleHostedChat(t *testing.T) {
	r := nativeSealingRig(t)
	watch := &cellstore.SealWatch{}
	agent, err := session.New(session.Config{Place: session.Place{Dir: r.cell.Root}, Workspace: r.work, Model: "test/sealing", APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1", System: "Test only", Seals: watch})
	mustNil(t, err)
	d, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	defer func() { mustNil(t, d.Close(context.Background())) }()
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"first turn"}`+"\n")
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	_, err = d.Store(r.engine).Seal(ctx, r.cell, cellstore.TurnInfo{})
	cancel()
	if err == nil {
		t.Fatal("expired first seal unexpectedly succeeded")
	}
	watch.Report(err)
	watch.Take()
	loop, err := remote.Loopback(remote.Hello{}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) { return &remote.Engine{Agent: agent}, nil }})
	mustNil(t, err)
	defer loop.Close()
	for _, words := range []string{"first reply", "second reply"} {
		appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"assistant","content":"`+words+`"}`+"\n")
		_, err = d.Store(r.engine).Seal(t.Context(), r.cell, cellstore.TurnInfo{})
		mustNil(t, err)
		watch.Report(nil)
	}
	r.durable(r.cell.ID, r.cell)
	var said string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		said += loop.Client.TakeNotice()
		if strings.Contains(said, "sealing works again") && !loop.Client.Agent().SealFailing() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if strings.Count(said, "sealing works again") != 1 || loop.Client.Agent().SealFailing() {
		t.Fatalf("idle hosted recovery=%q failing=%v", said, loop.Client.Agent().SealFailing())
	}
	if watch.Take() != "" || loop.Client.TakeNotice() != "" {
		t.Fatal("recovery remained queued after delivery")
	}
}

// Contract E1 lets unused startup bookkeeping recover without publishing a chat.
func TestUnusedPairedStartupSealRecoversWithoutPublication(t *testing.T) {
	r := nativeSealingRig(t)
	var watch cellstore.SealWatch
	watch.TrackTurn(r.cell.Root)
	watch.Report(errors.New("startup seal failed"))
	watch.Take()
	d, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{})
	mustNil(t, err)
	defer func() { mustNil(t, d.Close(context.Background())) }()
	_, err = d.Store(r.engine).Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	watch.Report(nil)
	if watch.Failing() || !strings.Contains(watch.Take(), "sealing works again") || watch.Take() != "" {
		t.Fatal("unused paired startup still waits for a publication it does not owe")
	}
}
