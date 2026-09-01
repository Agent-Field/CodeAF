package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/packed"
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
// about the network: the fetch fails, the catalog falls back to its compiled-in
// rows, and nothing here ever reaches OpenRouter or the person's own cache.
func deadCatalogEndpoint(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no catalog for a test", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	t.Setenv("AFORGE_BASE_URL", server.URL)
}

// v3SubharnessBlockingReads is what wiring one conversation's subharness
// registry is allowed to ask the catalog through the waiting door, and the
// whole of it is ONE call that does not belong to this surface: [buildLinear]
// in subharness.go, which asks ContextLength to size a leaf. That file is
// shared with `aforge exec`, `aforge run` and `aforge subharness` — headless
// doors where waiting for a catalog is correct — and it is the KNOWN RESIDUAL
// on this path, recorded here rather than asserted away.
//
// Everything chatv3_subharness.go asks for itself is zero, and that is the law
// the perf wave landed: the context window it configures the runner with comes
// from [v3Window], which reads [catalog.Catalog.ModelsNow] and takes "not yet"
// for an answer. Asking it the blocking way held the first frame of every
// conversation behind a fetch for a number no frame reads.
const v3SubharnessBlockingReads = 1

// TestTheSubharnessWiringAsksTheCatalogNothingThatWaits pins that.
func TestTheSubharnessWiringAsksTheCatalogNothingThatWaits(t *testing.T) {
	deadCatalogEndpoint(t)
	proc := v3TestProcess(t)

	before := proc.Models.BlockingReads()
	v3Subharnesses(proc.Settings, proc.Models, "test/model", t.TempDir(), proc.Harnesses)
	asked := proc.Models.BlockingReads() - before

	if asked != v3SubharnessBlockingReads {
		t.Fatalf("wiring a conversation's subharnesses asked the catalog %d blocking questions, and the law is %d.\n"+
			"The one that is allowed is subharness.go's buildLinear, shared with the headless doors.\n"+
			"If you added a question: ask it through catalog.ModelsNow() instead, which answers nil while the\n"+
			"catalog is still warming — that is the honest answer and it costs no frame. If you REMOVED the\n"+
			"residual, lower the constant here and in PERF.md in the same commit.", asked, v3SubharnessBlockingReads)
	}
}

// v3LaunchBlockingReads is the whole launch's bill, and it is a RATCHET rather
// than a law: the number is what the path costs today, it is too high, and the
// only direction it may move without a conversation is down.
//
// The sixteen are two families:
//
//   - ONE from subharness.go's [buildLinear], described above.
//   - FIFTEEN from [v3RunHarness] (chatv3.go's harness seam), which builds the
//     harness tool bridge EAGERLY at launch. [session.HarnessBelt] arms the
//     media hands, each of those asks [v3MediaModel] which model would draw,
//     see, speak or sing, and every one of those questions goes through
//     config.CandidateMediaModel to a blocking listing. Nothing drawn in the
//     first frame depends on any of it — the answers are wanted the first time
//     somebody asks for a picture, minutes later — so this is a whole family of
//     fetches held in front of a dark terminal, and it is written down here so
//     that it is a known debt rather than a discovery.
//
// A change that lowers this is a change that made the launch faster; lower the
// constant with it. A change that raises it has put a fetch in front of the
// first frame, which is the thing this file exists to stop.
const v3LaunchBlockingReads = 16

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
// corpus a question never pays for it — its package doc says `aforge --version`
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
		// names: an installer asking whether aforge is here.
		before := packed.Unpacks()
		if err := runVersion(); err != nil {
			t.Fatalf("--version: %v", err)
		}
		if unpacked := packed.Unpacks() - before; unpacked != 0 {
			t.Fatalf("`aforge --version` decompressed %d packed corpora. It reads nothing, writes nothing "+
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
