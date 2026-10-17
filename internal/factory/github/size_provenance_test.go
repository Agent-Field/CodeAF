package github

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/triage"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func sizeIssue(body string) factory.Item {
	return factory.Item{Product: "acme", Repo: "api", Num: 5, Kind: factory.KindIssue, Title: "t", Body: body,
		Origin: factory.OriginForge, Triage: factory.Triage{Type: "bug"}}
}

func sizePull(adds, dels int, files []factory.FileChange) factory.Item {
	it := factory.Item{Product: "acme", Repo: "api", Num: 9, Kind: factory.KindPR, Title: "p", Body: "b",
		Origin: factory.OriginForge, Diff: DiffWords(adds, dels), Files: files}
	it.Triage = pullTriage(forge.Pull{Additions: adds, Deletions: dels})
	return it
}

// A pull request's diff size is stamped only from a measurement: the list
// leaves the line counts out, and zeros measure nothing.
func TestPullTriageStampsADiffOnlyWhenMeasured(t *testing.T) {
	for _, tc := range []struct {
		adds, dels int
		size, from string
	}{
		{0, 0, "", ""}, {10, 5, "S", factory.SizeFromDiff}, {200, 100, "M", factory.SizeFromDiff}, {500, 1, "L", factory.SizeFromDiff},
	} {
		got := pullTriage(forge.Pull{Additions: tc.adds, Deletions: tc.dels})
		if got.Type != "review" || got.Size != tc.size || got.SizeFrom != tc.from {
			t.Errorf("+%d −%d => %+v", tc.adds, tc.dels, got)
		}
	}
}

// An issue arrives with no size however long its body is, the read's size
// lands when the read does, and it is shown only then.
func TestIssueArrivesWithoutABodyLengthSizeAndTheReadFillsIt(t *testing.T) {
	st := openStore(t)
	if err := Merge(st, Name, []factory.Item{sizeIssue(strings.Repeat("x", 3000))}); err != nil {
		t.Fatal(err)
	}
	items, _ := st.List()
	if items[0].Triage.Size != "" || items[0].Triage.SizeFrom != "" {
		t.Fatalf("a long body arrived with a size: %+v", items[0].Triage)
	}
	if err := st.Update(items[0].ID, func(it *factory.Item) error {
		triage.Apply(it, factory.Triage{Read: "r", Type: "bug", Size: "S"})
		it.Triage.TriagedAt = time.Now()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(items[0].ID)
	if size, from := factory.ShownSize(got); size != "S" || from != factory.ShownRead {
		t.Fatalf("the read's size was not shown: %q from %q (%+v)", size, from, got.Triage)
	}
	if factory.JobSize(got) != "" {
		t.Fatalf("the read became a start size: %q", factory.JobSize(got))
	}
}

// An edit takes off the size the read wrote, and nothing derives another from
// the new words.
func TestBodyEditClearsTheReadSizeWithoutAReseed(t *testing.T) {
	st := openStore(t)
	if err := Merge(st, Name, []factory.Item{sizeIssue("short")}); err != nil {
		t.Fatal(err)
	}
	items, _ := st.List()
	_ = st.Update(items[0].ID, func(it *factory.Item) error {
		triage.Apply(it, factory.Triage{Read: "r", Size: "M", StartJudged: true})
		it.Triage.TriagedAt = time.Now()
		return nil
	})
	if err := Merge(st, Name, []factory.Item{sizeIssue(strings.Repeat("y", 5000))}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(items[0].ID)
	tr := got.Triage
	if tr.Size != "" || tr.SizeFrom != "" || !tr.TriagedAt.IsZero() || tr.StartJudged {
		t.Fatalf("the edit kept the read or reseeded a size: %+v", tr)
	}
	if size, _ := factory.ShownSize(got); size != "" {
		t.Fatalf("a cleared read still shows %q", size)
	}
	// ...and the item is read once more, so its next size is a read's.
	if !triage.Wants(got) {
		t.Fatal("the edited item is not waiting for a new read")
	}
}

// A legacy size (no provenance) survives an edit as unknown: it is neither
// cleared into a lie nor promoted.
func TestBodyEditLeavesALegacySizeUnknown(t *testing.T) {
	st := openStore(t)
	if err := Merge(st, Name, []factory.Item{sizeIssue("short")}); err != nil {
		t.Fatal(err)
	}
	items, _ := st.List()
	_ = st.Update(items[0].ID, func(it *factory.Item) error { it.Triage.Size = "S"; return nil })
	if err := Merge(st, Name, []factory.Item{sizeIssue("a different body")}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(items[0].ID)
	if size, from := factory.ShownSize(got); size != "" || from != "" {
		t.Fatalf("a legacy size is shown: %q from %q", size, from)
	}
}

// A pull request's size follows the forge's measurement: stamped on arrival,
// restamped when the counts move, upgraded from a legacy value, and left
// alone by a read that carried no counts.
func TestPullRequestDiffSizeFollowsTheForge(t *testing.T) {
	st := openStore(t)
	files := []factory.FileChange{{Path: "a.go", Added: 5}}
	if err := Merge(st, Name, []factory.Item{sizePull(20, 10, files)}); err != nil {
		t.Fatal(err)
	}
	items, _ := st.List()
	id := items[0].ID
	shown := func() (string, string) { got, _ := st.Get(id); return factory.ShownSize(got) }
	if size, from := shown(); size != "S" || from != factory.ShownDiff {
		t.Fatalf("arrival: %q from %q", size, from)
	}
	// The counts move: the size is measured again.
	if err := Merge(st, Name, []factory.Item{sizePull(300, 100, files)}); err != nil {
		t.Fatal(err)
	}
	if size, from := shown(); size != "M" || from != factory.ShownDiff {
		t.Fatalf("refresh: %q from %q", size, from)
	}
	// A read that has not measured (no files, no counts) keeps the size.
	undetailed := sizePull(0, 0, nil)
	undetailed.Title = "p renamed"
	if err := Merge(st, Name, []factory.Item{undetailed}); err != nil {
		t.Fatal(err)
	}
	if size, from := shown(); size != "M" || from != factory.ShownDiff {
		t.Fatalf("an unmeasured read changed the size: %q from %q", size, from)
	}
	// A pull request stored before provenance is unknown until the forge measures it.
	_ = st.Update(id, func(it *factory.Item) error { it.Triage.Size, it.Triage.SizeFrom = "L", ""; return nil })
	if size, from := shown(); size != "" || from != "" {
		t.Fatalf("legacy pull request shows %q from %q", size, from)
	}
	if err := Merge(st, Name, []factory.Item{sizePull(300, 100, files)}); err != nil {
		t.Fatal(err)
	}
	if size, from := shown(); size != "M" || from != factory.ShownDiff {
		t.Fatalf("the forge did not restamp a legacy pull request: %q from %q", size, from)
	}
}

// A title edit on a pull request clears its read but keeps the measured diff.
func TestPullRequestEditKeepsTheDiffSize(t *testing.T) {
	st := openStore(t)
	files := []factory.FileChange{{Path: "a.go", Added: 5}}
	if err := Merge(st, Name, []factory.Item{sizePull(20, 10, files)}); err != nil {
		t.Fatal(err)
	}
	items, _ := st.List()
	_ = st.Update(items[0].ID, func(it *factory.Item) error { it.Triage.Read, it.Triage.TriagedAt = "r", time.Now(); return nil })
	edited := sizePull(20, 10, files)
	edited.Title = "retitled"
	if err := Merge(st, Name, []factory.Item{edited}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(items[0].ID)
	if got.Triage.Read != "" || got.Triage.Size != "S" || got.Triage.SizeFrom != factory.SizeFromDiff {
		t.Fatalf("after a retitle: %+v", got.Triage)
	}
}
