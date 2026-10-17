package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// richSharePlanRowForTest is a planned row with every nested pointer a caller
// could keep hold of — Deliver, File, Source, Media — shaped as the owned copy
// of the planned file: its own folder named for the id, the original named.
func richSharePlanRowForTest(r ShareReceipt, planned SharePlannedRow) Artifact {
	row := sharePlanRowForTest(r, planned)
	copyPath := filepath.Join("/copies", planned.ID, filepath.Base(planned.Path))
	row.Kind, row.Path = "image", copyPath
	row.Deliver.File.Canonical, row.Deliver.File.MIME = copyPath, "image/png"
	row.Deliver.Source = &DeliverableFile{Canonical: planned.Path, Bytes: row.Deliver.File.Bytes, SHA256: row.Deliver.File.SHA256, MIME: "image/png"}
	row.Media = &MediaFact{
		ID: NewMediaID(), Origin: MediaShared, Canonical: copyPath, MIME: "image/png", SHA256: row.Deliver.File.SHA256, Bytes: row.Deliver.File.Bytes,
		Width: 4, Height: 3, Run: r.Provenance.Run, Message: r.Provenance.Turn, Tool: r.Provenance.Call,
	}
	return row
}

func sharePlanJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// mutateSharePlanRowForTest changes every field a caller could still reach
// through a row it holds: the row itself and each nested pointer and slice.
func mutateSharePlanRowForTest(row *Artifact) {
	row.Path, row.Title = "/elsewhere/x.png", "x.png"
	row.Deliver.ID, row.Deliver.Run, row.Deliver.Turn, row.Deliver.Call = NewDeliverableID(), "run-later", "s#9", "call-later"
	row.Deliver.Version, row.Deliver.Authored, row.Deliver.State = "sha256:"+strings.Repeat("cd", 32), true, DeliverKept
	row.Deliver.File.SHA256, row.Deliver.File.Bytes, row.Deliver.File.Canonical = strings.Repeat("ef", 32), 999, "/elsewhere/x.png"
	row.Deliver.Source.Canonical, row.Deliver.Source.SHA256 = "/evil/source", "00"
	row.Media.ID, row.Media.Run, row.Media.Tool, row.Media.Message = NewMediaID(), "run-later", "call-later", "s#9"
	row.Media.Width, row.Media.Bytes = 9999, 9999
}

// TestSharePlanIsDetachedFromEveryCallerAlias is the causal proof: every field
// a caller could still reach after Plan — through the rows it passed in, or
// through the exported Receipt view — is changed, and the claim's own plan,
// what Settle writes, and what a reload reads are all still the original.
func TestSharePlanIsDetachedFromEveryCallerAlias(t *testing.T) {
	root := t.TempDir()
	prov := ShareProvenance{Run: "run-1", Turn: "s#1", Call: "call-1"}
	claim, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.png", "/w/b.png"}, prov, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Release()
	rows := []Artifact{richSharePlanRowForTest(claim.Receipt, claim.Receipt.Planned[0]), richSharePlanRowForTest(claim.Receipt, claim.Receipt.Planned[1])}
	want := sharePlanJSON(t, rows)
	if err := claim.Plan(rows); err != nil {
		t.Fatal(err)
	}

	// The caller's own rows, and the exported view, both after Plan.
	for i := range rows {
		mutateSharePlanRowForTest(&rows[i])
	}
	for i := range claim.Receipt.Planned {
		mutateSharePlanRowForTest(claim.Receipt.Planned[i].Row)
	}
	claim.Receipt.Provenance = ShareProvenance{Session: "s", Run: "run-later", Turn: "s#9", Call: "call-later"}
	claim.Receipt.Planned[0].ID = NewDeliverableID()

	planned, err := claim.PlannedRows()
	if err != nil {
		t.Fatal(err)
	}
	if got := sharePlanJSON(t, planned); got != want {
		t.Fatalf("the claim's append plan follows a caller alias:\n got %s\nwant %s", got, want)
	}
	// PlannedRows hands out its own memory too: changing one answer cannot
	// change the next.
	mutateSharePlanRowForTest(&planned[0])
	again, err := claim.PlannedRows()
	if err != nil {
		t.Fatal(err)
	}
	if got := sharePlanJSON(t, again); got != want {
		t.Fatal("PlannedRows answered memory a previous caller could change")
	}

	ids := []string{again[0].Deliver.ID, again[1].Deliver.ID}
	if err := claim.Settle(ShareOpAccepted, ids); err != nil {
		t.Fatalf("settle from the durable plan: %v", err)
	}
	loaded, err := LoadShareReceipt(root, "s", "op")
	if err != nil {
		t.Fatal(err)
	}
	reloaded := []Artifact{*loaded.Planned[0].Row, *loaded.Planned[1].Row}
	if got := sharePlanJSON(t, reloaded); got != want {
		t.Fatalf("the durable manifest followed a caller alias:\n got %s\nwant %s", got, want)
	}
	if loaded.Provenance != (ShareProvenance{Session: "s", Run: "run-1", Turn: "s#1", Call: "call-1"}) || !reflect.DeepEqual(loaded.RecordedIDs, ids) {
		t.Fatalf("settle wrote the edited view: prov=%+v recorded=%v", loaded.Provenance, loaded.RecordedIDs)
	}
}

// TestShareSettleWritesTheDurableReceiptNotTheView proves the exported view is
// not settle's authority even before any plan: an edited view of a pending
// receipt cannot become the planned ids, provenance or status on disk.
func TestShareSettleWritesTheDurableReceiptNotTheView(t *testing.T) {
	root := t.TempDir()
	claim, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.md"}, ShareProvenance{Call: "call-1"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Release()
	id := claim.Receipt.Planned[0].ID
	claim.Receipt.Planned[0].ID = NewDeliverableID()
	claim.Receipt.Provenance.Call = "call-later"
	claim.Receipt.Status = ShareOpPlanned // a view that lies about a plan
	if err := claim.Settle(ShareOpAccepted, []string{id}); !errors.Is(err, ErrShareOpInvalid) {
		t.Fatalf("accepted from an edited view of a pending receipt err=%v", err)
	}
	loaded, err := LoadShareReceipt(root, "s", "op")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != ShareOpPending || loaded.Planned[0].ID != id || loaded.Provenance.Call != "call-1" {
		t.Fatalf("the edited view reached disk: %+v", loaded)
	}

	// AND A LEGAL TRANSITION CANNOT CARRY AN EDIT EITHER. Refused is allowed
	// from pending, and a provenance that is merely different is well-formed,
	// so no validation could catch it: only settling from the durable bytes
	// keeps the original caller on the receipt.
	other, err := AdmitShareOperation(root, "s", "op-refuse", []string{"/w/a.md"}, ShareProvenance{Run: "run-1", Turn: "s#1", Call: "call-1"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	other.Receipt.Provenance = ShareProvenance{Session: "s", Run: "run-later", Turn: "s#9", Call: "call-later"}
	if err := other.Settle(ShareOpRefused, nil); err != nil {
		t.Fatal(err)
	}
	refused, err := LoadShareReceipt(root, "s", "op-refuse")
	if err != nil {
		t.Fatal(err)
	}
	if refused.Status != ShareOpRefused || refused.Provenance != (ShareProvenance{Session: "s", Run: "run-1", Turn: "s#1", Call: "call-1"}) {
		t.Fatalf("settle wrote an edited provenance: %+v", refused)
	}
}

// TestSharePlanRefusesARowAttributedToAnyoneButTheOriginalCaller checks each
// attribution field on its own, in the deliverable fact and the media fact,
// and proves the refusal comes BEFORE any effect: the receipt on disk is
// still the pending one, byte for byte, and the claim can still plan honestly.
func TestSharePlanRefusesARowAttributedToAnyoneButTheOriginalCaller(t *testing.T) {
	mutations := map[string]func(*Artifact){
		"deliver run":   func(a *Artifact) { a.Deliver.Run = "run-later" },
		"deliver turn":  func(a *Artifact) { a.Deliver.Turn = "s#9" },
		"deliver call":  func(a *Artifact) { a.Deliver.Call = "call-later" },
		"deliver blank": func(a *Artifact) { a.Deliver.Call = "" },
		"media run":     func(a *Artifact) { a.Media.Run = "run-later" },
		"media message": func(a *Artifact) { a.Media.Message = "s#9" },
		"media tool":    func(a *Artifact) { a.Media.Tool = "call-later" },
		"media id":      func(a *Artifact) { a.Media.ID = "" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			prov := ShareProvenance{Run: "run-1", Turn: "s#1", Call: "call-1"}
			claim, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.png"}, prov, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			defer claim.Release()
			_, path := ShareOpPaths(root, "s", "op")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			row := richSharePlanRowForTest(claim.Receipt, claim.Receipt.Planned[0])
			mutate(&row)
			if err := claim.Plan([]Artifact{row}); !errors.Is(err, ErrShareOpInvalid) {
				t.Fatalf("misattributed plan err=%v, want invalid", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("a refused plan changed the receipt on disk")
			}
			if _, err := claim.PlannedRows(); !errors.Is(err, ErrShareOpInvalid) {
				t.Fatalf("a refused plan left rows to append: %v", err)
			}
			if err := claim.Plan([]Artifact{richSharePlanRowForTest(claim.Receipt, claim.Receipt.Planned[0])}); err != nil {
				t.Fatalf("an honest plan after the refusal: %v", err)
			}
		})
	}
}

// TestShareReceiptOnDiskRefusesAMisattributedPlan is the same law at the read
// door: a receipt file whose rows name another run, message or call is not
// trusted, so it can neither replay nor be appended from.
func TestShareReceiptOnDiskRefusesAMisattributedPlan(t *testing.T) {
	mutations := map[string]func(*Artifact){
		"run":           func(a *Artifact) { a.Deliver.Run = "run-later" },
		"turn":          func(a *Artifact) { a.Deliver.Turn = "s#9" },
		"call":          func(a *Artifact) { a.Deliver.Call = "call-later" },
		"media run":     func(a *Artifact) { a.Media.Run = "run-later" },
		"media message": func(a *Artifact) { a.Media.Message = "s#9" },
		"media tool":    func(a *Artifact) { a.Media.Tool = "call-later" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			prov := ShareProvenance{Run: "run-1", Turn: "s#1", Call: "call-1"}
			claim, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.png"}, prov, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if err := claim.Plan([]Artifact{richSharePlanRowForTest(claim.Receipt, claim.Receipt.Planned[0])}); err != nil {
				claim.Release()
				t.Fatal(err)
			}
			claim.Release()
			loaded, err := LoadShareReceipt(root, "s", "op")
			if err != nil {
				t.Fatal(err)
			}
			mutate(loaded.Planned[0].Row)
			_, path := ShareOpPaths(root, "s", "op")
			if err := os.WriteFile(path, []byte(sharePlanJSON(t, loaded)), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadShareReceipt(root, "s", "op"); err == nil {
				t.Fatal("a misattributed plan on disk was trusted")
			}
			if got, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.png"}, prov, time.Time{}); err == nil {
				got.Release()
				t.Fatal("a misattributed plan on disk replayed")
			}
		})
	}
}
