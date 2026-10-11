package stream

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placeorg"
	"github.com/Agent-Field/codeaf/internal/roles"
)

func TestAClearLeafIsFiledWithoutAQuestion(t *testing.T) {
	t.Parallel()
	tree := threeLevel()
	j := &fakeJudge{}
	got, err := New().File(context.Background(), tree, arrival("c", []float32{1, 0}), j)
	if err != nil {
		t.Fatal(err)
	}
	if j.calls != 0 || got.Judged || got.Op.Kind != placeorg.OpFile || got.Op.Place != "leaf" {
		t.Fatalf("filing %+v calls %d", got, j.calls)
	}
}

func TestACloseCallAcrossBranchesAsksAndKeepsTheBroaderPlace(t *testing.T) {
	t.Parallel()
	// The leaf is the closer place. The other top-level place is within the
	// margin and shallower, so a judge that does not answer keeps the chat
	// there rather than at the leaf.
	tree := putTree([]placeorg.Node{
		{ID: "broad", Name: "Broad", Depth: 1, Children: nil, Centroids: centroid(placeorg.SpacePrimary, toward(0.68, 2))},
		{ID: "top", Name: "Top", Depth: 1, Children: []string{"mid"}, Centroids: centroid(placeorg.SpacePrimary, toward(0.50, 2))},
		{ID: "mid", Name: "Mid", Parent: "top", Depth: 2, Children: []string{"leaf"}, Centroids: centroid(placeorg.SpacePrimary, toward(0.55, 2))},
		{ID: "leaf", Name: "Leaf", Parent: "mid", Depth: 3, Centroids: centroid(placeorg.SpacePrimary, toward(0.70, 2))},
	}, []string{"broad", "top"})
	j := &fakeJudge{fallback: true}
	got, err := New().File(context.Background(), tree, arrival("c", []float32{1, 0}), j)
	if err != nil {
		t.Fatal(err)
	}
	if j.calls != 1 || !got.Judged || got.Op.Place != "broad" || got.Op.Kind != placeorg.OpPark {
		t.Fatalf("filing %+v calls %d", got, j.calls)
	}
	if len(got.Contest) != 2 {
		t.Fatalf("contest %v", got.Contest)
	}
	var places, none int
	for _, o := range j.last.Options {
		if o.Key == "none" {
			none++
			continue
		}
		if strings.Contains(strings.ToUpper(o.Key), "NEW") || strings.Contains(strings.ToUpper(o.Text), "NEW") {
			t.Fatalf("the menu offers a new place: %+v", o)
		}
		places++
	}
	if none != 1 || places > 5 || j.last.Role != roles.RolePlaceFile {
		t.Fatalf("menu %+v role %s", j.last.Options, j.last.Role)
	}
}

func TestNoneOfTheseLeavesTheChatInNow(t *testing.T) {
	t.Parallel()
	tree := twoRoots(0.70, 0.68)
	j := &fakeJudge{pick: placeorg.Pick{Key: "none"}}
	got, err := New().File(context.Background(), tree, arrival("c", []float32{1, 0}), j)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Judged || got.Op.Kind != placeorg.OpInbox {
		t.Fatalf("filing %+v", got)
	}
}

func TestAFolderMatchIsFiledWithNoQuestion(t *testing.T) {
	t.Parallel()
	tree := twoRoots(0.70, 0.68)
	j := &fakeJudge{}
	a := arrival("c", []float32{1, 0})
	a.FolderPlace = "b"
	s := New()
	got, err := s.File(context.Background(), tree, a, j)
	if err != nil {
		t.Fatal(err)
	}
	if j.calls != 0 || got.Judged || got.Op.Kind != placeorg.OpFile || got.Op.Place != "b" {
		t.Fatalf("filing %+v calls %d", got, j.calls)
	}
}

func TestBelowTheFloorTheChatStaysInNow(t *testing.T) {
	t.Parallel()
	tree := twoRoots(0.70, 0.68)
	got, err := New().File(context.Background(), tree, arrival("c", []float32{-1, 0}), &fakeJudge{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Op.Kind != placeorg.OpInbox || got.Judged {
		t.Fatalf("filing %+v", got)
	}
}

func TestFileStaysInsideItsBudgetAtFiveHundredPlaces(t *testing.T) {
	nodes := make([]placeorg.Node, 500)
	roots := make([]string, 500)
	for i := range nodes {
		id := "p" + itoa(i)
		// Spread the places around the first axis so scoring is real work
		// and not a pile of identical centroids.
		score := float32(i)/500 - 0.5
		nodes[i] = placeorg.Node{
			ID: id, Name: id, Depth: 1,
			Centroids: centroid(placeorg.SpacePrimary, toward(score, 256)),
		}
		roots[i] = id
	}
	tree := putTree(nodes, roots)
	s := New()
	a := arrival("c", toward(0.2, 256))
	if _, err := s.File(context.Background(), tree, a, &fakeJudge{}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := s.File(context.Background(), tree, a, &fakeJudge{}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed >= placeorg.FileBudget {
		t.Fatalf("File took %s, budget %s", elapsed, placeorg.FileBudget)
	}
}

func TestTheSourceNamesNoCorpusSubject(t *testing.T) {
	banned := []string{
		"cod" + "eaf", "des" + "k", "clou" + "dflare", "fle" + "et",
		"spa" + "rk", "pare" + "to", "wis" + "p", "carnat" + "ic", "gar" + "den",
	}
	files, err := paths("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src := read(t, f)
		for _, line := range strings.Split(src, "\n") {
			trim := strings.TrimSpace(line)
			if strings.Contains(trim, "github.com/Agent-Field/") {
				continue
			}
			low := strings.ToLower(trim)
			for _, w := range banned {
				if strings.Contains(low, w) {
					t.Errorf("%s contains %q", f, w)
				}
			}
		}
	}
}

func threeLevel() *placeorg.Tree {
	return putTree([]placeorg.Node{
		{ID: "top", Name: "Top", Depth: 1, Children: []string{"mid"}, Centroids: centroid(placeorg.SpacePrimary, toward(0.50, 2))},
		{ID: "mid", Name: "Mid", Parent: "top", Depth: 2, Children: []string{"leaf"}, Centroids: centroid(placeorg.SpacePrimary, toward(0.55, 2))},
		{ID: "leaf", Name: "Leaf", Parent: "mid", Depth: 3, Centroids: centroid(placeorg.SpacePrimary, toward(0.90, 2))},
	}, []string{"top"})
}

func twoRoots(a, b float32) *placeorg.Tree {
	return putTree([]placeorg.Node{
		{ID: "a", Name: "Alpha", Depth: 1, Centroids: centroid(placeorg.SpacePrimary, toward(a, 2))},
		{ID: "b", Name: "Beta", Depth: 1, Centroids: centroid(placeorg.SpacePrimary, toward(b, 2))},
	}, []string{"a", "b"})
}

type fakeJudge struct {
	calls    int
	last     placeorg.Question
	pick     placeorg.Pick
	fallback bool
}

func (f *fakeJudge) Choose(_ context.Context, q placeorg.Question, fallback placeorg.Option) placeorg.Pick {
	f.calls++
	f.last = q
	if f.fallback {
		return placeorg.Pick{Key: fallback.Key, FellBack: true, Why: "no model"}
	}
	if f.pick.Key == "" && f.pick.Written == "" {
		return placeorg.Pick{Key: fallback.Key, FellBack: true, Why: "no model"}
	}
	return f.pick
}

func (f *fakeJudge) Write(context.Context, placeorg.Question, placeorg.Validator, string) placeorg.Pick {
	return placeorg.Pick{Written: "kept"}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
