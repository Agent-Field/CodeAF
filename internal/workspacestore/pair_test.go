package workspacestore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

const (
	keyA = "now"
	keyB = "pl_0123456789abcdef"
	keyC = "pl_fedcba9876543210"
)

// tabSpec is one tab as the renderer shares it.
type tabSpec struct {
	id, draft string
	panes     []string
}

// pdoc builds a shared document with real saved state: drafts, a group, a
// split, a closed tab.
func pdoc(open []tabSpec, closed ...string) json.RawMessage {
	mk := func(t tabSpec) map[string]any {
		m := map[string]any{"id": t.id, "title": "Title " + t.id, "draft": t.draft, "pinned": t.id == "pin", "kind": "conversation", "groupId": "g1"}
		if len(t.panes) > 0 {
			var panes []map[string]any
			for _, p := range t.panes {
				panes = append(panes, map[string]any{"id": p, "title": "Pane " + p, "draft": "words in " + p, "kind": "conversation"})
			}
			m["split"] = map[string]any{"panes": panes}
		}
		return m
	}
	tabs := []map[string]any{}
	for _, t := range open {
		tabs = append(tabs, mk(t))
	}
	cl := []map[string]any{}
	for _, id := range closed {
		cl = append(cl, mk(tabSpec{id: id}))
	}
	data, _ := json.Marshal(map[string]any{
		"schema": 1, "tabs": tabs, "closed": cl, "nextNumber": 9,
		"groups": []any{map[string]any{"id": "g1", "title": "Group", "collapsed": true}},
	})
	return data
}

func tabs(ids ...string) []tabSpec {
	var out []tabSpec
	for _, id := range ids {
		out = append(out, tabSpec{id: id})
	}
	return out
}

func seed(t *testing.T, s *Store, key string, d json.RawMessage) Record {
	t.Helper()
	rec, err := s.Put(key, 0, "seed", d)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func req(intent string, src, dst Record, srcDoc, dstDoc json.RawMessage) PairRequest {
	return PairRequest{
		Intent: intent, Writer: "win-1", Source: src.Key, Destination: dst.Key,
		SourceRevision: src.Revision, DestinationRevision: dst.Revision,
		SourceWorkspace: srcDoc, DestinationWorkspace: dstDoc,
	}
}

// moveSetup saves A=[keep, mv(with split and drafts)] and B=[home] and returns
// the request that moves mv from A to B.
func moveSetup(t *testing.T, s *Store, intent string) (PairRequest, json.RawMessage, json.RawMessage) {
	t.Helper()
	moving := tabSpec{id: "mv", draft: "half a sentence", panes: []string{"mv-p1", "mv-p2"}}
	a := seed(t, s, keyA, pdoc([]tabSpec{{id: "keep"}, moving}))
	b := seed(t, s, keyB, pdoc(tabs("home")))
	nextA := pdoc(tabs("keep"))
	nextB := pdoc([]tabSpec{{id: "home"}, moving})
	return req(intent, a, b, nextA, nextB), nextA, nextB
}

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		out[e.Name()] = string(data)
	}
	return out
}

func compactOf(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestATransferPublishesBothDocumentsWholeAndNamesWhereTabsWent(t *testing.T) {
	s := open(t, t.TempDir())
	r, nextA, nextB := moveSetup(t, s, "intent-1")
	res, err := s.PutPair(r)
	if err != nil || res.Already {
		t.Fatalf("transfer: %+v %v", res, err)
	}
	if res.Source.Revision != 2 || res.Destination.Revision != 2 || res.Source.Writer != "win-1" {
		t.Fatalf("each side moves exactly one revision: %+v / %+v", res.Source, res.Destination)
	}
	// Everything the renderer shared survives byte for byte: drafts, split
	// panes, groups, pins, closed tabs.
	if string(res.Source.Workspace) != compactOf(t, nextA) || string(res.Destination.Workspace) != compactOf(t, nextB) {
		t.Fatalf("documents must be stored exactly as sent")
	}
	if !strings.Contains(string(res.Destination.Workspace), "half a sentence") || !strings.Contains(string(res.Destination.Workspace), "words in mv-p2") {
		t.Fatal("drafts and split panes must travel with the tab")
	}
	want := map[string]string{"mv": keyB, "mv-p1": keyB, "mv-p2": keyB}
	if !reflect.DeepEqual(res.Source.MovedTo, want) {
		t.Fatalf("movedTo = %v, want %v", res.Source.MovedTo, want)
	}
	if res.Destination.MovedTo != nil {
		t.Fatalf("the destination gave nothing away: %v", res.Destination.MovedTo)
	}
	// A reader on a different store sees the same, and nothing is left pending.
	other := open(t, s.opts.Dir)
	got, _ := other.Get(keyA)
	if got.Revision != 2 || !reflect.DeepEqual(got.MovedTo, want) {
		t.Fatalf("a second store reads %+v", got)
	}
	if _, err := os.Stat(s.journalPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a completed transfer leaves no journal: %v", err)
	}
}

func TestAStaleRevisionOnEitherSideChangesNeitherAndReservesNoIntent(t *testing.T) {
	for _, stale := range []string{"source", "destination"} {
		t.Run(stale, func(t *testing.T) {
			s := open(t, t.TempDir())
			r, _, _ := moveSetup(t, s, "intent-1")
			// Another window edits one side first.
			if stale == "source" {
				if _, err := s.Put(keyA, 1, "w2", pdoc(tabs("keep", "mv", "late"))); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := s.Put(keyB, 1, "w2", pdoc(tabs("home", "late"))); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshot(t, s.opts.Dir)
			_, err := s.PutPair(r)
			var c *PairConflictError
			if !errors.As(err, &c) {
				t.Fatalf("want a pair conflict, got %v", err)
			}
			if c.Source.Revision == 0 || c.Destination.Revision == 0 || c.Source.Workspace == nil {
				t.Fatalf("the refusal carries both current records: %+v", c)
			}
			if after := snapshot(t, s.opts.Dir); !reflect.DeepEqual(before, after) {
				t.Fatal("a refused transfer must not change a byte on disk")
			}
			// Rebase onto the latest records and retry under the SAME intent.
			r.SourceRevision, r.DestinationRevision = c.Source.Revision, c.Destination.Revision
			if _, err := s.PutPair(r); err != nil {
				t.Fatalf("a conflict reserves no intent, the rebased retry must commit: %v", err)
			}
		})
	}
}

func TestReplayingACommittedTransferIsAcknowledgedOnceAndChangedBytesAreRefused(t *testing.T) {
	s := open(t, t.TempDir())
	r, _, _ := moveSetup(t, s, "intent-1")
	first, err := s.PutPair(r)
	if err != nil {
		t.Fatal(err)
	}
	// A different store (the retry may come after a restart).
	other := open(t, s.opts.Dir)
	again, err := other.PutPair(r)
	if err != nil || !again.Already {
		t.Fatalf("identical replay: %+v %v", again, err)
	}
	if again.Source.Revision != first.Source.Revision || again.Destination.Revision != first.Destination.Revision {
		t.Fatalf("a replay must not move a revision: %d/%d", again.Source.Revision, again.Destination.Revision)
	}
	if !reflect.DeepEqual(again.Source.MovedTo, first.Source.MovedTo) {
		t.Fatal("the replay acknowledges the same relocation")
	}
	changed := r
	changed.DestinationWorkspace = pdoc(tabs("home", "mv", "extra"))
	if _, err := other.PutPair(changed); !errors.Is(err, ErrIntentChanged) {
		t.Fatalf("same id, different documents: %v", err)
	}
	rebased := r
	rebased.SourceRevision, rebased.DestinationRevision = 2, 2
	if _, err := other.PutPair(rebased); !errors.Is(err, ErrIntentChanged) {
		t.Fatalf("same id, different revisions: %v", err)
	}
	other2 := r
	other2.Writer = "win-2"
	if _, err := other.PutPair(other2); !errors.Is(err, ErrIntentChanged) {
		t.Fatalf("same id, different writer: %v", err)
	}
	if cur, _ := s.Get(keyA); cur.Revision != first.Source.Revision {
		t.Fatal("refusals must not move a revision")
	}
	// The same document serialised with other whitespace and key order is the
	// same transfer.
	var v any
	_ = json.Unmarshal(r.SourceWorkspace, &v)
	reordered, _ := json.MarshalIndent(v, "", "  ")
	r.SourceWorkspace = reordered
	if res, err := other.PutPair(r); err != nil || !res.Already {
		t.Fatalf("re-serialised replay: %+v %v", res, err)
	}
}

func TestTheLedgerKeepsOnlyTheLastTwentyIntents(t *testing.T) {
	s := open(t, t.TempDir())
	a := seed(t, s, keyA, pdoc(tabs("x")))
	b := seed(t, s, keyB, pdoc(tabs("y")))
	var first PairRequest
	for i := 0; i < 25; i++ {
		// Alternate the tab between the two places.
		var nextA, nextB json.RawMessage
		if i%2 == 0 {
			nextA, nextB = pdoc(tabs("keep")), pdoc(tabs("y", "x"))
		} else {
			nextA, nextB = pdoc(tabs("keep", "x")), pdoc(tabs("y"))
		}
		r := req(fmt.Sprintf("intent-%d", i), a, b, nextA, nextB)
		if i == 0 {
			first = r
		}
		res, err := s.PutPair(r)
		if err != nil {
			t.Fatalf("transfer %d: %v", i, err)
		}
		a, b = res.Source, res.Destination
	}
	led, err := s.readLedger()
	if err != nil || len(led.Entries) != maxLedgerEntries || led.Entries[0].Intent != "intent-5" || led.Entries[19].Intent != "intent-24" {
		t.Fatalf("the ledger is bounded to the newest intents: %v %+v", err, led.Entries)
	}
	// A forgotten intent is simply an old request: its revisions are stale.
	var c *PairConflictError
	if _, err := s.PutPair(first); !errors.As(err, &c) {
		t.Fatalf("forgotten intent: %v", err)
	}
}

func TestReverseTransfersAndRestoresMoveTheHintToTheOtherPlace(t *testing.T) {
	s := open(t, t.TempDir())
	r, _, _ := moveSetup(t, s, "out")
	out, err := s.PutPair(r)
	if err != nil {
		t.Fatal(err)
	}
	if out.Source.MovedTo["mv"] != keyB {
		t.Fatalf("out: %v", out.Source.MovedTo)
	}
	// Bring it back.
	back, err := s.PutPair(req("back", out.Destination, out.Source,
		pdoc(tabs("home")), pdoc([]tabSpec{{id: "keep"}, {id: "mv", draft: "half a sentence", panes: []string{"mv-p1", "mv-p2"}}})))
	// back's source is B (destination of "out").
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Destination.MovedTo) != 0 {
		t.Fatalf("A has mv open again, a stale hint would redirect drafts away from it: %v", back.Destination.MovedTo)
	}
	if back.Source.MovedTo["mv"] != keyA || back.Source.MovedTo["mv-p2"] != keyA {
		t.Fatalf("B's tabs went to A: %v", back.Source.MovedTo)
	}
	// An explicit restore: the tab was moved out, then reopened in A from its
	// own Closed list through an ordinary write. The hint must not point away.
	r2 := req("out2", back.Destination, back.Source, pdoc(tabs("keep"), "mv", "mv-p1", "mv-p2"), pdoc([]tabSpec{{id: "home"}, {id: "mv2"}}))
	r2.DestinationWorkspace = pdoc([]tabSpec{{id: "home"}, {id: "mv"}, {id: "mv2"}})
	out2, err := s.PutPair(r2)
	if err != nil {
		t.Fatal(err)
	}
	// mv is closed in A (still known there): not "removed from source".
	if _, hinted := out2.Source.MovedTo["mv"]; hinted {
		t.Fatalf("an id still closed in its source is not relocated: %v", out2.Source.MovedTo)
	}
}

func TestAnIdOpenInItsSourceAgainIsNeverHintedAway(t *testing.T) {
	s := open(t, t.TempDir())
	r, _, _ := moveSetup(t, s, "out")
	out, err := s.PutPair(r)
	if err != nil {
		t.Fatal(err)
	}
	// Undo: the source window puts the tab back with an ordinary write.
	undone, err := s.Put(keyA, out.Source.Revision, "win-1", pdoc([]tabSpec{{id: "keep"}, {id: "mv"}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, hinted := undone.MovedTo["mv"]; hinted {
		t.Fatalf("restored in the source: %v", undone.MovedTo)
	}
	if got, _ := s.Get(keyA); got.MovedTo["mv"] != "" {
		t.Fatalf("Get agrees: %v", got.MovedTo)
	}
	if _, hinted := undone.MovedTo["mv-p1"]; !hinted {
		t.Fatalf("only the restored ids are unhinted: %v", undone.MovedTo)
	}
}

// crashRun commits a transfer with a fault injected at stage and returns what
// the caller was told.
func crashRun(t *testing.T, s *Store, stage string, r PairRequest) PairResult {
	t.Helper()
	fired := false
	s.pairFault = func(at string) error {
		if at == stage && !fired {
			fired = true
			return errors.New("simulated crash at " + at)
		}
		return nil
	}
	defer func() { s.pairFault = nil }()
	res, err := s.PutPair(r)
	if err != nil {
		t.Fatalf("a crash after the commit still acknowledges the commit: %v", err)
	}
	if !fired {
		t.Fatalf("stage %q never ran", stage)
	}
	return res
}

func TestACrashAfterTheCommitShowsBothNewHalvesToEveryReader(t *testing.T) {
	for _, stage := range []string{"after-journal", "after-destination", "after-source", "after-ledger"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			s := open(t, dir)
			r, _, nextB := moveSetup(t, s, "intent-1")
			crashRun(t, s, stage, r)
			if _, err := os.Stat(s.journalPath()); err != nil {
				t.Fatalf("the interrupted transfer keeps its journal: %v", err)
			}
			// Physical state really is partial where the stage says so.
			rawA, _, _ := s.read(keyA)
			rawB, _, _ := s.read(keyB)
			switch stage {
			case "after-journal":
				if rawA.Revision != 1 || rawB.Revision != 1 {
					t.Fatal("nothing is materialized yet")
				}
			case "after-destination":
				if rawA.Revision != 1 || rawB.Revision != 2 {
					t.Fatalf("destination first: %d/%d", rawA.Revision, rawB.Revision)
				}
			default:
				if rawA.Revision != 2 || rawB.Revision != 2 {
					t.Fatalf("both materialized: %d/%d", rawA.Revision, rawB.Revision)
				}
			}
			// A fresh process (a new Store) sees the whole transfer, read-only.
			before := snapshot(t, dir)
			reader := open(t, dir)
			a, errA := reader.Get(keyA)
			b, errB := reader.Get(keyB)
			if errA != nil || errB != nil || a.Revision != 2 || b.Revision != 2 {
				t.Fatalf("readers see both new halves: %+v %+v %v %v", a, b, errA, errB)
			}
			if strings.Contains(string(a.Workspace), `"mv"`) || !strings.Contains(string(b.Workspace), `"mv"`) {
				t.Fatal("the tab is in exactly one place")
			}
			if string(b.Workspace) != compactOf(t, nextB) || a.MovedTo["mv"] != keyB || a.MovedTo["mv-p1"] != keyB {
				t.Fatalf("relocation is readable from the pending journal: %v", a.MovedTo)
			}
			if after := snapshot(t, dir); !reflect.DeepEqual(before, after) {
				t.Fatal("Get must not write")
			}
			// Retrying the same intent (the window never heard back) finishes it
			// and creates no second transfer.
			res, err := reader.PutPair(r)
			if err != nil || !res.Already || res.Source.Revision != 2 || res.Destination.Revision != 2 {
				t.Fatalf("retry: %+v %v", res, err)
			}
			if _, err := os.Stat(reader.journalPath()); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("the retry completes the journal")
			}
		})
	}
}

func TestANormalPutFinishesAPendingTransferAndKeepsUnrelatedEdits(t *testing.T) {
	for _, stage := range []string{"after-journal", "after-destination"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			s := open(t, dir)
			c := seed(t, s, keyC, pdoc(tabs("c1")))
			r, _, _ := moveSetup(t, s, "intent-1")
			crashRun(t, s, stage, r)

			other := open(t, dir)
			// A write to the SOURCE from its pre-transfer revision cannot overwrite the pair.
			stale := pdoc(tabs("keep", "mv", "typed"))
			_, err := other.Put(keyA, 1, "win-2", stale)
			var conflict *ConflictError
			if !errors.As(err, &conflict) || conflict.Current.Revision != 2 || strings.Contains(string(conflict.Current.Workspace), `"mv"`) {
				t.Fatalf("a stale source write is refused with the committed record: %v", err)
			}
			if got, _ := other.Get(keyB); got.Revision != 2 || !strings.Contains(string(got.Workspace), `"mv"`) {
				t.Fatal("the committed destination survives")
			}
			if _, err := os.Stat(other.journalPath()); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("the write that met the journal finished it first")
			}
			// New edits on top of the committed pair, and on an unrelated key.
			a, err := other.Put(keyA, 2, "win-2", pdoc(tabs("keep", "fresh")))
			if err != nil || a.Revision != 3 {
				t.Fatalf("edit on top of the pair: %+v %v", a, err)
			}
			if a.MovedTo["mv"] != keyB {
				t.Fatalf("relocation outlives the journal: %v", a.MovedTo)
			}
			if got, _ := other.Put(keyC, c.Revision, "win-2", pdoc(tabs("c1", "c2"))); got.Revision != 2 {
				t.Fatalf("unrelated key: %+v", got)
			}
			b, _ := other.Get(keyB)
			if b.Revision != 2 || !strings.Contains(string(b.Workspace), "half a sentence") {
				t.Fatalf("destination untouched by later source edits: %+v", b)
			}
		})
	}
}

func TestAFailureBeforeTheCommitLeavesNothingBehind(t *testing.T) {
	for _, stage := range []string{"before-journal", "journal-rename"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			s := open(t, dir)
			r, _, _ := moveSetup(t, s, "intent-1")
			before := snapshot(t, dir)
			s.pairFault = func(at string) error {
				if at == stage {
					return errors.New("disk full")
				}
				return nil
			}
			if _, err := s.PutPair(r); err == nil {
				t.Fatal("the failure must be reported: the transfer did not happen")
			}
			s.pairFault = nil
			if after := snapshot(t, dir); !reflect.DeepEqual(before, after) {
				t.Fatalf("no journal, no temp file, no change: %v", after)
			}
			// And the same intent is still free.
			if _, err := s.PutPair(r); err != nil {
				t.Fatalf("a failure before the commit reserves nothing: %v", err)
			}
		})
	}
}

func TestADirectorySyncFailurePreservesThePublishedCommit(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	r, _, _ := moveSetup(t, s, "intent-1")
	calls := 0
	s.syncDir = func(string) error { calls++; return errors.New("EIO") }
	if result, err := s.PutPair(r); err != nil || result.Source.Revision != 2 || result.Destination.Revision != 2 {
		t.Fatal("a public logical commit must be acknowledged", result, err)
	}
	s.syncDir = nil
	if calls == 0 {
		t.Fatal("the journal's directory must be synced")
	}
	if _, err := os.Stat(s.journalPath()); err != nil {
		t.Fatal("failed durable completion must retain journal", err)
	}
	if _, err := s.PutPair(r); err != nil {
		t.Fatalf("retry: %v", err)
	}
}

func TestAnUnreadableRecordOrPendingFileIsRefusedAndLeftAlone(t *testing.T) {
	t.Run("damaged record", func(t *testing.T) {
		dir := t.TempDir()
		s := open(t, dir)
		r, _, _ := moveSetup(t, s, "i")
		if err := os.WriteFile(filepath.Join(dir, keyB+".json"), []byte("{broken"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PutPair(r); !errors.Is(err, ErrDamaged) {
			t.Fatalf("got %v", err)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, keyB+".json")); string(got) != "{broken" {
			t.Fatal("damaged bytes stay exactly where they are")
		}
		if m, _ := filepath.Glob(filepath.Join(dir, "*.damaged-*")); len(m) != 0 {
			t.Fatal("a transfer never sets a file aside")
		}
	})
	for name, content := range map[string]string{
		"corrupt journal": "{not json",
		"newer journal":   `{"schema":9}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			s := open(t, dir)
			r, _, _ := moveSetup(t, s, "i")
			path := s.journalPath()
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			want := ErrPairState
			if name == "newer journal" {
				want = ErrUnsupportedVersion
			}
			if _, err := s.PutPair(r); !errors.Is(err, want) {
				t.Fatalf("transfer: %v", err)
			}
			if _, err := s.Put(keyC, 0, "w", pdoc(tabs("c"))); !errors.Is(err, want) {
				t.Fatalf("ordinary writes do not write around an unreadable journal: %v", err)
			}
			if rec, err := s.Get(keyA); err != nil || rec.Revision != 1 {
				t.Fatalf("reads keep serving the last good record: %+v %v", rec, err)
			}
			if got, _ := os.ReadFile(path); string(got) != content {
				t.Fatal("the unreadable journal is left in place")
			}
		})
	}
	t.Run("corrupt ledger", func(t *testing.T) {
		dir := t.TempDir()
		s := open(t, dir)
		r, _, _ := moveSetup(t, s, "i")
		if err := os.WriteFile(s.ledgerPath(), []byte("garbage"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PutPair(r); !errors.Is(err, ErrPairState) {
			t.Fatalf("transfer: %v", err)
		}
		if cur, _ := s.Get(keyA); cur.Revision != 1 {
			t.Fatal("refused before the commit")
		}
		if _, err := s.Put(keyC, 0, "w", pdoc(tabs("c"))); err != nil {
			t.Fatalf("ordinary writes do not need the ledger: %v", err)
		}
		if got, _ := os.ReadFile(s.ledgerPath()); string(got) != "garbage" {
			t.Fatal("left in place")
		}
	})
}

func TestARefusedTransferIsExplainedBeforeAnyLockOrFile(t *testing.T) {
	good := pdoc(tabs("a"))
	withLocal := func(extra string) json.RawMessage {
		return json.RawMessage(strings.Replace(string(good), `"schema":1`, `"schema":1,`+extra, 1))
	}
	splitFocus := json.RawMessage(`{"schema":1,"tabs":[{"id":"s","title":"s","draft":"","pinned":false,"split":{"panes":[{"id":"p","title":"","draft":""},{"id":"q","title":"","draft":""}],"focus":"p"}}],"groups":[],"closed":[],"nextNumber":2}`)
	base := func() PairRequest {
		return PairRequest{Intent: "ok-1", Writer: "w", Source: keyA, Destination: keyB, SourceWorkspace: good, DestinationWorkspace: good}
	}
	huge := json.RawMessage(`{"schema":1,"tabs":[{"id":"a","title":"","draft":"` + strings.Repeat("x", MaxDocumentBytes) + `","pinned":false}],"groups":[],"closed":[],"nextNumber":2}`)
	cases := map[string]struct {
		mut  func(*PairRequest)
		want error
	}{
		"active id":      {func(r *PairRequest) { r.SourceWorkspace = withLocal(`"activeId":"a"`) }, ErrInvalid},
		"scroll":         {func(r *PairRequest) { r.DestinationWorkspace = withLocal(`"scroll":{"a":3}`) }, ErrInvalid},
		"focus":          {func(r *PairRequest) { r.DestinationWorkspace = withLocal(`"focus":"a"`) }, ErrInvalid},
		"recent":         {func(r *PairRequest) { r.SourceWorkspace = withLocal(`"recentIds":["a"]`) }, ErrInvalid},
		"split focus":    {func(r *PairRequest) { r.SourceWorkspace = splitFocus }, ErrInvalid},
		"bad source key": {func(r *PairRequest) { r.Source = "../now" }, ErrInvalidKey},
		"bad dest key":   {func(r *PairRequest) { r.Destination = "pl_XYZ" }, ErrInvalidKey},
		"same place":     {func(r *PairRequest) { r.Destination = r.Source }, ErrInvalid},
		"empty intent":   {func(r *PairRequest) { r.Intent = "" }, ErrInvalid},
		"long intent":    {func(r *PairRequest) { r.Intent = strings.Repeat("a", 65) }, ErrInvalid},
		"odd intent":     {func(r *PairRequest) { r.Intent = "a b/../c" }, ErrInvalid},
		"odd writer":     {func(r *PairRequest) { r.Writer = "w w" }, ErrInvalid},
		"oversize":       {func(r *PairRequest) { r.DestinationWorkspace = huge }, ErrTooLarge},
		"not json":       {func(r *PairRequest) { r.SourceWorkspace = json.RawMessage(`[1`) }, ErrInvalid},
		"nothing":        {func(r *PairRequest) { r.SourceWorkspace = nil }, ErrInvalid},
	}
	names := make([]string, 0, len(cases))
	for n := range cases {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			s := open(t, dir)
			r := base()
			cases[name].mut(&r)
			if _, err := s.PutPair(r); !errors.Is(err, cases[name].want) {
				t.Fatalf("got %v, want %v", err, cases[name].want)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Fatalf("a refusal touches nothing, found %d files", len(entries))
			}
		})
	}
	// The bound the document validator names applies to the pair too.
	s := open(t, t.TempDir())
	r := base()
	var many []tabSpec
	for i := 0; i <= MaxTabs; i++ {
		many = append(many, tabSpec{id: fmt.Sprintf("t%d", i)})
	}
	r.SourceWorkspace = pdoc(many)
	if _, err := s.PutPair(r); !errors.Is(err, ErrInvalid) {
		t.Fatalf("too many tabs: %v", err)
	}
}

func TestSourceWritesAndTransfersNeverLoseAnUpdateAcrossStores(t *testing.T) {
	for round := 0; round < 15; round++ {
		dir := t.TempDir()
		s1, s2 := open(t, dir), open(t, dir)
		r, _, _ := moveSetup(t, s1, "intent-1")
		var wg sync.WaitGroup
		var putErr, pairErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, pairErr = s1.PutPair(r)
		}()
		go func() {
			defer wg.Done()
			_, putErr = s2.Put(keyA, 1, "win-2", pdoc(tabs("keep", "mv", "typed")))
		}()
		wg.Wait()
		pairOK, putOK := pairErr == nil, putErr == nil
		var pc *PairConflictError
		var c *ConflictError
		switch {
		case pairOK && putOK:
			t.Fatalf("round %d: both won from the same revision", round)
		case !pairOK && !errors.As(pairErr, &pc):
			t.Fatalf("round %d: transfer failed oddly: %v", round, pairErr)
		case !putOK && !errors.As(putErr, &c):
			t.Fatalf("round %d: put failed oddly: %v", round, putErr)
		case !pairOK && !putOK:
			t.Fatalf("round %d: both refused", round)
		}
		a, _ := s1.Get(keyA)
		b, _ := s1.Get(keyB)
		hasA, hasB := strings.Contains(string(a.Workspace), `"mv"`), strings.Contains(string(b.Workspace), `"mv"`)
		if pairOK && (hasA || !hasB) || putOK && (!hasA || hasB) {
			t.Fatalf("round %d: the winner's state must be whole: pair=%v put=%v A=%v B=%v", round, pairOK, putOK, hasA, hasB)
		}
	}
}

func TestTwoStoresRacingTheSameTransferCommitItOnce(t *testing.T) {
	dir := t.TempDir()
	s1, s2 := open(t, dir), open(t, dir)
	r1, _, _ := moveSetup(t, s1, "intent-1")
	r2 := r1
	r2.Intent = "intent-2"
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, c := range []struct {
		s *Store
		r PairRequest
	}{{s1, r1}, {s2, r2}} {
		wg.Add(1)
		go func() { defer wg.Done(); _, errs[i] = c.s.PutPair(c.r) }()
	}
	wg.Wait()
	wins := 0
	for _, e := range errs {
		if e == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("exactly one transfer may win from one pair of revisions: %v", errs)
	}
	if a, _ := s1.Get(keyA); a.Revision != 2 {
		t.Fatalf("source moved once: %d", a.Revision)
	}
}

func TestGetOnAFreshDirectoryCreatesNothingAndStampsMoveOnPublication(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	if _, err := s.Get(keyA); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a read creates no lock and no file: %d", len(entries))
	}
	r, _, _ := moveSetup(t, s, "intent-1")
	stamps := []string{s.PairStamp()}
	s.pairFault = func(at string) error {
		if at == "after-journal" {
			stamps = append(stamps, s.PairStamp()) // the commit is visible before any record moves
			return errors.New("crash")
		}
		return nil
	}
	if _, err := s.PutPair(r); err != nil {
		t.Fatal(err)
	}
	s.pairFault = nil
	if stamps[0] == stamps[1] {
		t.Fatalf("the stamp must change at the logical commit: %v", stamps)
	}
	if _, err := s.Put(keyC, 0, "w", pdoc(tabs("c"))); err != nil {
		t.Fatal(err)
	}
	if now := s.PairStamp(); now == stamps[1] {
		t.Fatalf("and again when the transfer completes: %q", now)
	}
}

func TestOrdinaryWritesStillNeverLoseAnUpdateWhileTransfersRun(t *testing.T) {
	dir := t.TempDir()
	s1, s2 := open(t, dir), open(t, dir)
	a := seed(t, s1, keyA, pdoc(tabs("x")))
	b := seed(t, s1, keyB, pdoc(tabs("y")))
	c := seed(t, s1, keyC, pdoc(tabs("z")))
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			rec := c
			for {
				ids := append([]string{"z"}, fmt.Sprint("n", i))
				next, err := s2.Put(keyC, rec.Revision, "w", pdoc(tabs(ids...)))
				var conflict *ConflictError
				if errors.As(err, &conflict) {
					rec = conflict.Current
					continue
				}
				if err != nil {
					t.Error(err)
					return
				}
				c = next
				break
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			var da, db json.RawMessage
			if i%2 == 0 {
				da, db = pdoc(tabs("w")), pdoc(tabs("y", "x"))
			} else {
				da, db = pdoc(tabs("w", "x")), pdoc(tabs("y"))
			}
			res, err := s1.PutPair(req(fmt.Sprint("i", i), a, b, da, db))
			if err != nil {
				t.Error(err)
				return
			}
			a, b = res.Source, res.Destination
		}
	}()
	wg.Wait()
	if got, _ := s1.Get(keyC); got.Revision != 11 {
		t.Fatalf("every one of the 10 edits landed exactly once: revision %d", got.Revision)
	}
	if got, _ := s1.Get(keyA); got.Revision != 11 {
		t.Fatalf("every transfer landed once: %d", got.Revision)
	}
}
