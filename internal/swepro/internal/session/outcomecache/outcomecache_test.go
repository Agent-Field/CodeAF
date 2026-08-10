package outcomecache

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// Translation of src/session/outcome-cache.test.ts. Subtest names are the
// describe/test strings verbatim; assertion semantics are preserved (toEqual on
// a structure becomes byte equality of its V8 serialization, a TS `throw`
// inside the injected exec becomes a returned error).

func makeOutcome(mut func(*LeafOutcome)) LeafOutcome {
	o := LeafOutcome{
		TaskID:        "task-1",
		Model:         LeafModel{ProviderID: "provider", ModelID: "model-a"},
		SizeBand:      "s",
		Verdict:       "pass",
		RepairRounds:  0,
		Turns:         1,
		ToolErrors:    0,
		CostUsd:       0.01,
		WallMs:        100,
		MergeConflict: false,
		Timestamp:     123,
	}
	if mut != nil {
		mut(&o)
	}
	return o
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := jscompat.Stringify(v)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	return string(b)
}

func eqJSON(t *testing.T, got any, want string) {
	t.Helper()
	if s := mustJSON(t, got); s != want {
		t.Errorf("got %s, want %s", s, want)
	}
}

func TestOutcomeCacheKeyTS(t *testing.T) {
	t.Run("is stable under whitespace reshuffling", func(t *testing.T) {
		first := OutcomeCacheKey(OutcomeCacheKeyOpts{BriefText: "  ship\n the   thing ", TreeHash: "tree", ModelID: "model"})
		second := OutcomeCacheKey(OutcomeCacheKeyOpts{BriefText: "ship the thing", TreeHash: "tree", ModelID: "model"})
		if first != second {
			t.Errorf("expected %q to be %q", first, second)
		}
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(first) {
			t.Errorf("expected %q to match /^[0-9a-f]{64}$/", first)
		}
	})

	t.Run("changes when the brief, tree, or model changes", func(t *testing.T) {
		base := OutcomeCacheKeyOpts{BriefText: "ship the thing", TreeHash: "tree", ModelID: "model"}
		key := OutcomeCacheKey(base)

		otherBrief := base
		otherBrief.BriefText = "ship another thing"
		if OutcomeCacheKey(otherBrief) == key {
			t.Error("expected a different brief to change the key")
		}
		otherTree := base
		otherTree.TreeHash = "other-tree"
		if OutcomeCacheKey(otherTree) == key {
			t.Error("expected a different tree to change the key")
		}
		otherModel := base
		otherModel.ModelID = "other-model"
		if OutcomeCacheKey(otherModel) == key {
			t.Error("expected a different model to change the key")
		}
	})
}

func TestComputeTreeStateTS(t *testing.T) {
	t.Run("reads the tree hash and dirty status", func(t *testing.T) {
		var commands [][]string
		state := ComputeTreeState(func(cmd []string) (ExecResult, error) {
			commands = append(commands, cmd)
			if cmd[1] == "rev-parse" {
				return ExecResult{Stdout: "abc123\n", ExitCode: 0}, nil
			}
			return ExecResult{Stdout: " M src/file.ts\n", ExitCode: 0}, nil
		})
		eqJSON(t, state, `{"treeHash":"abc123","dirty":true}`)
		eqJSON(t, commands, `[["git","rev-parse","HEAD^{tree}"],["git","status","--porcelain"]]`)
	})

	t.Run("returns null when either command fails or throws", func(t *testing.T) {
		failing := ComputeTreeState(func(cmd []string) (ExecResult, error) {
			if cmd[1] == "rev-parse" {
				return ExecResult{Stdout: "", ExitCode: 1}, nil
			}
			return ExecResult{Stdout: "", ExitCode: 0}, nil
		})
		if failing != nil {
			t.Errorf("expected null, got %s", mustJSON(t, failing))
		}
		throwing := ComputeTreeState(func([]string) (ExecResult, error) {
			return ExecResult{}, fmt.Errorf("git unavailable")
		})
		if throwing != nil {
			t.Errorf("expected null, got %s", mustJSON(t, throwing))
		}
	})
}

func TestCreateOutcomeCacheTS(t *testing.T) {
	t.Run("accepts an evidence-backed pass", func(t *testing.T) {
		cache := CreateOutcomeCache(nil)
		result := cache.Put(PutEntry{
			Key: "pass-key",
			Outcome: makeOutcome(func(o *LeafOutcome) {
				o.Evidence = &LeafEvidence{AuditCommandsRun: 1, AuditBlockers: 0, InRunTestsPassed: false}
			}),
			Dirty: false,
		})
		eqJSON(t, result, `{"stored":true,"reason":"verified pass stored"}`)
		hit := cache.Get("pass-key")
		if hit == nil {
			t.Fatal("expected a hit for pass-key")
		}
		if hit.Outcome.Verdict != "pass" {
			t.Errorf("got verdict %q, want %q", hit.Outcome.Verdict, "pass")
		}
	})

	t.Run("accepts a pass backed by in-run tests", func(t *testing.T) {
		cache := CreateOutcomeCache(nil)
		stored := cache.Put(PutEntry{
			Key: "test-key",
			Outcome: makeOutcome(func(o *LeafOutcome) {
				o.Evidence = &LeafEvidence{AuditCommandsRun: 0, AuditBlockers: 0, InRunTestsPassed: true}
			}),
			Dirty: false,
		}).Stored
		if !stored {
			t.Error("expected stored to be true")
		}
	})

	t.Run("rejects a failed verdict, missing evidence, and dirty tree distinctly", func(t *testing.T) {
		cache := CreateOutcomeCache(nil)
		failed := cache.Put(PutEntry{Key: "fail", Outcome: makeOutcome(func(o *LeafOutcome) { o.Verdict = "fail" }), Dirty: false})
		unverified := cache.Put(PutEntry{Key: "unverified", Outcome: makeOutcome(nil), Dirty: false})
		dirty := cache.Put(PutEntry{
			Key: "dirty",
			Outcome: makeOutcome(func(o *LeafOutcome) {
				o.Evidence = &LeafEvidence{AuditCommandsRun: 1, AuditBlockers: 0, InRunTestsPassed: false}
			}),
			Dirty: true,
		})
		eqJSON(t, failed, `{"stored":false,"reason":"outcome verdict is not pass"}`)
		eqJSON(t, unverified, `{"stored":false,"reason":"pass outcome has no verification evidence"}`)
		eqJSON(t, dirty, `{"stored":false,"reason":"tree is dirty"}`)
		distinct := map[string]bool{failed.Reason: true, unverified.Reason: true, dirty.Reason: true}
		if len(distinct) != 3 {
			t.Errorf("got %d distinct reasons, want 3", len(distinct))
		}
	})

	t.Run("returns a copy so mutations cannot corrupt the stored outcome", func(t *testing.T) {
		cache := CreateOutcomeCache(nil)
		outcome := makeOutcome(func(o *LeafOutcome) {
			o.Evidence = &LeafEvidence{AuditCommandsRun: 1, AuditBlockers: 0, InRunTestsPassed: false}
		})
		cache.Put(PutEntry{Key: "copy-key", Outcome: outcome, Dirty: false})
		first := cache.Get("copy-key")
		if first == nil {
			t.Fatal("expected a hit for copy-key")
		}
		first.Outcome.Evidence.AuditCommandsRun = 99
		first.Outcome.Model.ModelID = "mutated"
		eqJSON(t, cache.Get("copy-key"), mustJSON(t, CachedOutcome{Key: "copy-key", Outcome: outcome}))
	})
}

func TestWorkspacePersistenceLookupRecordTS(t *testing.T) {
	verifiedPass := func(mut func(*LeafOutcome)) LeafOutcome {
		return makeOutcome(func(o *LeafOutcome) {
			o.Evidence = &LeafEvidence{AuditCommandsRun: 2, AuditBlockers: 0, InRunTestsPassed: true}
			if mut != nil {
				mut(o)
			}
		})
	}
	clean := func(treeHash string) TreeState { return TreeState{TreeHash: treeHash, Dirty: false} }

	t.Run("tolerant reader: missing/corrupt cache file yields an empty store", func(t *testing.T) {
		ws := t.TempDir()
		if n := LoadOutcomeCacheStore(ws).Len(); n != 0 {
			t.Errorf("got size %d, want 0", n)
		}
		mustMkdirAll(t, filepath.Join(ws, ".codeaf"))
		mustWrite(t, filepath.Join(ws, ".codeaf", "outcome-cache.json"), []byte("{ not json ]"))
		if n := LoadOutcomeCacheStore(ws).Len(); n != 0 {
			t.Errorf("got size %d, want 0", n)
		}
	})

	t.Run("persist → load round-trips string entries only", func(t *testing.T) {
		ws := t.TempDir()
		store := jscompat.NewOrderedMap[string, string]()
		store.Set("k", "v")
		PersistOutcomeCacheStore(ws, store)
		back := LoadOutcomeCacheStore(ws)
		if v, _ := back.Get("k"); v != "v" {
			t.Errorf("got %q, want %q", v, "v")
		}
	})

	t.Run("record a verified pass, then look it up: HIT on identical (brief, tree, model)", func(t *testing.T) {
		ws := t.TempDir()
		rec := RecordVerifiedOutcome(ws, RecordOpts{
			BriefText: "  build   the widget ",
			TreeState: clean("tree-abc"),
			ModelID:   "model-x",
			Outcome:   verifiedPass(nil),
		})
		if !rec.Stored {
			t.Fatalf("expected stored, got %s", mustJSON(t, rec))
		}
		// whitespace-insensitive brief, same tree + model ⇒ hit
		hit := LookupVerifiedOutcome(ws, LookupOpts{
			BriefText: "build the widget",
			TreeState: clean("tree-abc"),
			ModelID:   "model-x",
		})
		if hit == nil || hit.Outcome.Verdict != "pass" {
			t.Fatalf("expected a pass hit, got %s", mustJSON(t, hit))
		}
	})

	t.Run("INVALIDATION: a different tree hash misses (sibling merge / advanced base)", func(t *testing.T) {
		ws := t.TempDir()
		RecordVerifiedOutcome(ws, RecordOpts{
			BriefText: "build the widget",
			TreeState: clean("tree-abc"),
			ModelID:   "model-x",
			Outcome:   verifiedPass(nil),
		})
		if hit := LookupVerifiedOutcome(ws, LookupOpts{
			BriefText: "build the widget",
			TreeState: clean("tree-DIFFERENT"),
			ModelID:   "model-x",
		}); hit != nil {
			t.Errorf("expected null, got %s", mustJSON(t, hit))
		}
	})

	t.Run("INVALIDATION: a different model or brief misses", func(t *testing.T) {
		ws := t.TempDir()
		RecordVerifiedOutcome(ws, RecordOpts{
			BriefText: "build the widget",
			TreeState: clean("tree-abc"),
			ModelID:   "model-x",
			Outcome:   verifiedPass(nil),
		})
		if hit := LookupVerifiedOutcome(ws, LookupOpts{
			BriefText: "build the widget", TreeState: clean("tree-abc"), ModelID: "model-y",
		}); hit != nil {
			t.Errorf("expected null, got %s", mustJSON(t, hit))
		}
		if hit := LookupVerifiedOutcome(ws, LookupOpts{
			BriefText: "build the OTHER thing", TreeState: clean("tree-abc"), ModelID: "model-x",
		}); hit != nil {
			t.Errorf("expected null, got %s", mustJSON(t, hit))
		}
	})

	t.Run("a dirty tree never yields a hit even when the key would match", func(t *testing.T) {
		ws := t.TempDir()
		RecordVerifiedOutcome(ws, RecordOpts{
			BriefText: "build the widget",
			TreeState: clean("tree-abc"),
			ModelID:   "model-x",
			Outcome:   verifiedPass(nil),
		})
		hit := LookupVerifiedOutcome(ws, LookupOpts{
			BriefText: "build the widget",
			TreeState: TreeState{TreeHash: "tree-abc", Dirty: true},
			ModelID:   "model-x",
		})
		if hit != nil {
			t.Errorf("expected null, got %s", mustJSON(t, hit))
		}
	})

	t.Run("NEVER caches a failed outcome — nothing is persisted, later lookup misses", func(t *testing.T) {
		ws := t.TempDir()
		rec := RecordVerifiedOutcome(ws, RecordOpts{
			BriefText: "build the widget",
			TreeState: clean("tree-abc"),
			ModelID:   "model-x",
			Outcome:   verifiedPass(func(o *LeafOutcome) { o.Verdict = "fail" }),
		})
		if rec.Stored {
			t.Error("expected stored to be false")
		}
		if rec.Reason != "outcome verdict is not pass" {
			t.Errorf("got reason %q", rec.Reason)
		}
		// no file written / no entry
		if _, err := os.Stat(filepath.Join(ws, cacheFile)); err == nil {
			t.Error("expected no sidecar file to be written")
		}
		if n := LoadOutcomeCacheStore(ws).Len(); n != 0 {
			t.Errorf("got size %d, want 0", n)
		}
		if hit := LookupVerifiedOutcome(ws, LookupOpts{
			BriefText: "build the widget", TreeState: clean("tree-abc"), ModelID: "model-x",
		}); hit != nil {
			t.Errorf("expected null, got %s", mustJSON(t, hit))
		}
	})

	t.Run("NEVER caches an unverified pass (no evidence)", func(t *testing.T) {
		ws := t.TempDir()
		rec := RecordVerifiedOutcome(ws, RecordOpts{
			BriefText: "build the widget",
			TreeState: clean("tree-abc"),
			ModelID:   "model-x",
			Outcome:   makeOutcome(nil), // pass but no evidence
		})
		if rec.Stored {
			t.Error("expected stored to be false")
		}
		if rec.Reason != "pass outcome has no verification evidence" {
			t.Errorf("got reason %q", rec.Reason)
		}
		if n := LoadOutcomeCacheStore(ws).Len(); n != 0 {
			t.Errorf("got size %d, want 0", n)
		}
	})

	t.Run("NEVER caches against a dirty tree", func(t *testing.T) {
		ws := t.TempDir()
		rec := RecordVerifiedOutcome(ws, RecordOpts{
			BriefText: "build the widget",
			TreeState: TreeState{TreeHash: "tree-abc", Dirty: true},
			ModelID:   "model-x",
			Outcome:   verifiedPass(nil),
		})
		if rec.Stored {
			t.Error("expected stored to be false")
		}
		if rec.Reason != "tree is dirty" {
			t.Errorf("got reason %q", rec.Reason)
		}
		if n := LoadOutcomeCacheStore(ws).Len(); n != 0 {
			t.Errorf("got size %d, want 0", n)
		}
	})
}
