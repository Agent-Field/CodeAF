package main

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/packed"
)

// WHAT THE WAY TO THE FIRST FRAME IS ALLOWED TO DO, COUNTED.
//
// Two costs can hold a terminal dark before anything is drawn in it, and
// neither of them shows up as a slow function: a question put to the model
// catalog through the door that WAITS (a GET /models with a fifteen-second
// ceiling on a cold cache), and a packed corpus decompressed (a megabyte of
// gunzip for prose nothing has asked for yet). Both are invisible in review,
// because the call that causes them looks exactly like the call that does not.
//
// So they are counted rather than timed. A count is a fact about the code and
// the same fact on every machine; a stopwatch here would pass on a fast laptop,
// fail on a loaded CI box, and teach everyone to re-run the suite instead of
// reading it. PERF.md states that doctrine once, for all of these gates.

// deadCatalogEndpoint points the catalog at a server that refuses immediately,
// which is what makes every count below a fact about the launch rather than
// about the network: the fetch fails, this custom base honestly has no rows,
// and nothing here ever reaches OpenRouter or the person's own cache.
func deadCatalogEndpoint(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no catalog for a test", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	t.Setenv("CODEAF_BASE_URL", server.URL)
}

// v3SubharnessBlockingReads is zero: an unknown launch window stays unknown
// until discovery finishes. The shared leaf constructor must preserve it.
const v3SubharnessBlockingReads = 0

// TestTheSubharnessWiringAsksTheCatalogNothingThatWaits pins that.
func TestTheSubharnessWiringAsksTheCatalogNothingThatWaits(t *testing.T) {
	deadCatalogEndpoint(t)
	proc := v3TestProcess(t)

	before := proc.Models.BlockingReads()
	v3Subharnesses(proc.Settings, proc.Models, "test/model", 0, t.TempDir(), proc.Harnesses)
	asked := proc.Models.BlockingReads() - before

	if asked != v3SubharnessBlockingReads {
		t.Fatalf("wiring a conversation's subharnesses asked the catalog %d blocking questions, and the law is %d.\n"+
			"An interactive launch must not wait for the catalog.\n"+
			"If you added a question: ask it through catalog.ModelsNow() instead, which answers nil while the\n"+
			"catalog is still warming — that is the honest answer and it costs no frame.", asked, v3SubharnessBlockingReads)
	}
}

// v3LaunchBlockingReads is zero. Media tool bridges are built on the first
// harness run, and subharness wiring preserves a nonblocking window reading.
const v3LaunchBlockingReads = 0

// TestTheLaunchesBlockingCatalogReadsDoNotGrow is the ratchet.
func TestTheLaunchesBlockingCatalogReadsDoNotGrow(t *testing.T) {
	deadCatalogEndpoint(t)
	proc := v3TestProcess(t)

	before := proc.Models.BlockingReads()
	if _, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()}); err != nil {
		t.Fatalf("the launch every v3 door assembles through did not open: %v", err)
	}
	asked := proc.Models.BlockingReads() - before

	if asked != v3LaunchBlockingReads {
		t.Fatalf("assembling one conversation asked the catalog %d blocking questions, and the recorded figure is %d.\n"+
			"Every one of these can be a GET /models with a fifteen-second ceiling in front of a dark terminal.\n"+
			"Higher: you put a fetch on the way to the first frame — ask through catalog.ModelsNow(), or defer the\n"+
			"question into the closure that actually needs it, the way chatv3_media.go's callers do at message time.\n"+
			"Lower: you paid one off. Say so — move the constant here and in PERF.md in the same commit.",
			asked, v3LaunchBlockingReads)
	}
}

// NOTHING ON THE WAY TO THE FIRST FRAME UNPACKS A CORPUS.
//
// internal/packed's whole design is that declaring a folder reads nothing and
// the first read pays for the whole thing at once, so a run that never asks a
// corpus a question never pays for it — its package doc says `codeaf --version`
// decompresses none of this, in those words. That is a claim about the launch,
// and until this test it was a claim nothing checked: one manual lookup moved
// onto the launch path, one roster consulted while assembling a registry, and a
// megabyte of gunzip lands in front of the first frame with nothing to say so.
//
// The reading is a DIFFERENCE and not an absolute, because the package's counter
// is process-wide and other tests in this binary read corpora legitimately.
func TestNothingOnTheWayToTheFirstFrameUnpacksACorpus(t *testing.T) {
	deadCatalogEndpoint(t)

	t.Run("version", func(t *testing.T) {
		// The shortest path through the binary, and the one the package doc
		// names: an installer asking whether codeaf is here.
		before := packed.Unpacks()
		if err := runVersion(); err != nil {
			t.Fatalf("--version: %v", err)
		}
		if unpacked := packed.Unpacks() - before; unpacked != 0 {
			t.Fatalf("`codeaf --version` decompressed %d packed corpora. It reads nothing, writes nothing "+
				"and needs no key (version.go), and internal/packed's own doc says so in as many words.", unpacked)
		}
	})

	t.Run("chat", func(t *testing.T) {
		before := packed.Unpacks()
		proc := v3TestProcess(t)
		if _, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()}); err != nil {
			t.Fatalf("the launch every v3 door assembles through did not open: %v", err)
		}
		if unpacked := packed.Unpacks() - before; unpacked != 0 {
			t.Fatalf("opening a conversation decompressed %d packed corpora before a single frame was drawn.\n"+
				"The manual is read when the model calls the `manual` tool and the agent roster when a leaf is "+
				"built — both of them minutes after the person is looking at something. Whatever now reads a "+
				"corpus at launch should be asking it later, behind the sync.Once it already has.", unpacked)
		}
	})
}

// Holding the response open catches a launch that waits even if an immediately
// failed endpoint would make its blocking read look fast.
func TestAColdCatalogCannotHoldLaunch(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		http.Error(w, "no catalog", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	t.Setenv("CODEAF_BASE_URL", server.URL)
	proc := v3TestProcess(t)
	workspace := t.TempDir()
	done := make(chan error, 1)
	go func() {
		launch, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: workspace})
		if err == nil {
			cfg, open := v3Shape(launch.Config, v3LanesHere())
			agent, _, _, openErr := openV3Agent(cfg, launch.Project, open)
			err = openErr
			if agent != nil {
				_ = agent.Close()
			}
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		releaseOnce()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		t.Fatalf("launch waited for a catalog response that no first frame needs\n%s", buf[:n])
	}
}
