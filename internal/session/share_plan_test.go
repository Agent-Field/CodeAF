package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sharePlanRowForTest is the exact row shape Plan accepts for one planned id:
// the planned file itself, recorded in place, as a generic file.
// It carries the receipt's own provenance, as every planned row must.
func sharePlanRowForTest(r ShareReceipt, planned SharePlannedRow) Artifact {
	session, prov := r.Session, r.Provenance
	path := planned.Path
	sum := strings.Repeat("ab", 32)
	return Artifact{
		Path: path, Session: session, Title: filepath.Base(path), Kind: shareKindFile, Created: time.Now().UTC(),
		Deliver: &DeliverableFact{
			ID: planned.ID, At: time.Now().UTC(), Origin: DeliverOriginSharedByAgent, Run: prov.Run, Turn: prov.Turn, Call: prov.Call,
			File: &DeliverableFile{Canonical: path, Bytes: 3, SHA256: sum, MIME: "text/plain; charset=utf-8"}, Version: "sha256:" + sum,
		},
	}
}

func planShareForTest(t *testing.T, claim *ShareClaim) {
	t.Helper()
	rows := make([]Artifact, len(claim.Receipt.Planned))
	for i, planned := range claim.Receipt.Planned {
		rows[i] = sharePlanRowForTest(claim.Receipt, planned)
	}
	if err := claim.Plan(rows); err != nil {
		t.Fatalf("plan: %v", err)
	}
}

func TestSharePlanIsDurableAndImmutableOnReplay(t *testing.T) {
	root := t.TempDir()
	claim, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.md", "/w/b.png"}, ShareProvenance{Call: "first"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	planShareForTest(t, claim)
	want := claim.Receipt.Planned
	claim.Release() // the crash: planned, nothing settled

	replay, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.md", "/w/b.png"}, ShareProvenance{Call: "later"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Release()
	if replay.Outcome != ShareClaimReplay || replay.Receipt.Status != ShareOpPlanned || !replay.Receipt.PlanWritten() {
		t.Fatalf("replay=%q status=%q", replay.Outcome, replay.Receipt.Status)
	}
	for i := range want {
		got, _ := json.Marshal(replay.Receipt.Planned[i])
		was, _ := json.Marshal(want[i])
		if string(got) != string(was) {
			t.Fatalf("planned row %d changed across restart:\n got %s\nwant %s", i, got, was)
		}
	}
	// A plan is written once: a replay cannot replace it with a second look.
	if err := replay.Plan([]Artifact{sharePlanRowForTest(replay.Receipt, want[0]), sharePlanRowForTest(replay.Receipt, want[1])}); !errors.Is(err, ErrShareOpInvalid) {
		t.Fatalf("re-plan err=%v, want invalid", err)
	}
}

func TestShareSettleTransitionsKeepAPlanFromBeingCalledNeverShared(t *testing.T) {
	root := t.TempDir()
	claim, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.md"}, ShareProvenance{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if err := claim.Settle(ShareOpAccepted, nil); !errors.Is(err, ErrShareOpInvalid) {
		t.Fatalf("accepted with no plan err=%v", err)
	}
	claim, err = AdmitShareOperation(root, "s", "op", []string{"/w/a.md"}, ShareProvenance{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	planShareForTest(t, claim)
	if err := claim.Settle(ShareOpRefused, nil); !errors.Is(err, ErrShareOpInvalid) {
		t.Fatalf("refused over a plan err=%v", err)
	}
	r, err := LoadShareReceipt(root, "s", "op")
	if err != nil || r.Status != ShareOpPlanned {
		t.Fatalf("status=%q err=%v, want the plan kept", r.Status, err)
	}

	fresh, err := AdmitShareOperation(root, "s", "op2", []string{"/w/a.md"}, ShareProvenance{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.Settle(ShareOpRefused, nil); err != nil {
		t.Fatalf("refused before a plan: %v", err)
	}
	again, err := AdmitShareOperation(root, "s", "op2", []string{"/w/a.md"}, ShareProvenance{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Release()
	if again.Receipt.Status != ShareOpRefused {
		t.Fatalf("refusal is terminal, got %q", again.Receipt.Status)
	}
	if err := again.Plan([]Artifact{sharePlanRowForTest(again.Receipt, again.Receipt.Planned[0])}); !errors.Is(err, ErrShareOpInvalid) {
		t.Fatalf("plan after refusal err=%v", err)
	}
}

func TestShareReceiptRefusesAPlanThatClaimsMoreThanSharing(t *testing.T) {
	mutations := map[string]func(r *ShareReceipt){
		"authored":      func(r *ShareReceipt) { r.Planned[0].Row.Deliver.Authored = true },
		"agent origin":  func(r *ShareReceipt) { r.Planned[0].Row.Deliver.Origin = DeliverOriginAgent },
		"kept state":    func(r *ShareReceipt) { r.Planned[0].Row.Deliver.State = DeliverKept },
		"update":        func(r *ShareReceipt) { r.Planned[0].Row.Deliver.Update = true },
		"other id":      func(r *ShareReceipt) { r.Planned[0].Row.Deliver.ID = NewDeliverableID() },
		"other session": func(r *ShareReceipt) { r.Planned[0].Row.Session = "other" },
		"version":       func(r *ShareReceipt) { r.Planned[0].Row.Deliver.Version = "sha256:" + strings.Repeat("cd", 32) },
		"relative":      func(r *ShareReceipt) { r.Planned[0].Row.Path = "shared/a.md" },
		"branch":        func(r *ShareReceipt) { r.Planned[0].Row.Deliver.Branch = "task/1" },
		"source hash": func(r *ShareReceipt) {
			r.Planned[0].Row.Deliver.Source = &DeliverableFile{Canonical: "/x/a.md", SHA256: "00"}
		},
		"media origin":   func(r *ShareReceipt) { r.Planned[0].Row.Media = &MediaFact{ID: NewMediaID(), Origin: MediaGenerated} },
		"from":           func(r *ShareReceipt) { r.Planned[0].Row.Deliver.From = []string{"input"} },
		"profile":        func(r *ShareReceipt) { r.Planned[0].Row.Deliver.Profile = "writer" },
		"no mime":        func(r *ShareReceipt) { r.Planned[0].Row.Deliver.File.MIME = "" },
		"other kind":     func(r *ShareReceipt) { r.Planned[0].Row.Kind = "export" },
		"image no media": func(r *ShareReceipt) { r.Planned[0].Row.Kind = "image" },
		"not planned path": func(r *ShareReceipt) {
			row := r.Planned[1].Row
			row.Path, row.Title, row.Deliver.File.Canonical = "/w/b2.md", "b2.md", "/w/b2.md"
		},
		"other folder": func(r *ShareReceipt) {
			row := r.Planned[0].Row
			row.Path, row.Deliver.File.Canonical = "/elsewhere/a.md", "/elsewhere/a.md"
		},
		"partial":        func(r *ShareReceipt) { r.Planned[1].Row = nil },
		"pending rows":   func(r *ShareReceipt) { r.Status = ShareOpPending },
		"planned record": func(r *ShareReceipt) { r.RecordedIDs = []string{r.Planned[0].ID} },
		"oversize":       func(r *ShareReceipt) { r.Planned[0].Row.Title = strings.Repeat("t", deliverRowMax) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			claim, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.md", "/w/b.md"}, ShareProvenance{}, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			planShareForTest(t, claim)
			r := claim.Receipt
			claim.Release()
			raw, _ := json.Marshal(r)
			var corrupt ShareReceipt
			_ = json.Unmarshal(raw, &corrupt)
			mutate(&corrupt)
			raw, _ = json.Marshal(corrupt)
			_, path := ShareOpPaths(root, "s", "op")
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadShareReceipt(root, "s", "op"); err == nil {
				t.Fatal("a plan claiming more than sharing was trusted")
			}
		})
	}
}
