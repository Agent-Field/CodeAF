package triage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// The read sees the discussion: the last three comments, oldest first, each
// cut, under the body, and the vocabulary the answer must keep is unchanged.
func TestPromptCarriesTheLastThreeComments(t *testing.T) {
	long := strings.Repeat("x", CommentMost+50)
	it := factory.Item{Repo: "acme/api", Kind: factory.KindIssue, Title: "t", Body: "b", Comments: []factory.Comment{
		{Author: "ann", Body: "the first, which is dropped"},
		{Author: "bob", Body: "second"},
		{Author: "", Body: "third\nwith a line"},
		{Author: "dee", Body: long},
	}}
	p := Prompt(it)
	if strings.Contains(p, "the first") {
		t.Fatal("the prompt carried more than the last three comments")
	}
	for _, want := range []string{"latest comments, oldest first:", "- bob: second", "- someone: third with a line", "- dee: " + strings.Repeat("x", CommentMost) + "…"} {
		if !strings.Contains(p, want) {
			t.Fatalf("the prompt lacks %q:\n%s", want, p)
		}
	}
	if strings.Index(p, "body:") > strings.Index(p, "latest comments") {
		t.Fatal("the comments came before the body")
	}
	for _, word := range []string{"bug, feat, chore, question", "S, M, L", "priority"} {
		if !strings.Contains(p, word) {
			t.Fatalf("the vocabulary lost %q", word)
		}
	}
	if strings.Contains(Prompt(factory.Item{Title: "t"}), "latest comments") {
		t.Fatal("an item with no comments has a comments heading")
	}
}

// WHILE THE WORKER READS AN ITEM, the store says `reading` on it, and not
// after; the read itself appends `read` to the item's activity.
func TestTheWorkerMarksTheItemItIsReading(t *testing.T) {
	st := openStore(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Title: "one", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	var during string
	call := func(context.Context, string) (string, error) {
		busy, _, _ := st.Busy()
		during = busy[it.ID]
		return clean, nil
	}
	w := &worker{st: st, call: call, fails: map[int]int{}}
	if w.once(context.Background()) != tickRead {
		t.Fatal("the worker read nothing")
	}
	if during != factory.BusyReading {
		t.Fatalf("the item was %q while it was read", during)
	}
	if busy, _, _ := st.Busy(); len(busy) != 0 {
		t.Fatalf("the item stayed busy: %v", busy)
	}
	got, _ := st.Get(it.ID)
	if len(got.Activity) != 1 || got.Activity[0].What != factory.EventRead || got.Activity[0].At.After(time.Now()) {
		t.Fatalf("the read's activity is %+v", got.Activity)
	}
}
