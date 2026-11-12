package store

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A REPOSITORY'S COPY IS KEPT UNDER ONE SPELLING, AND EVERY CHANGE MOVES ITS
// VERSION: a slot never written reads 1, a save and a stop each count, a
// change that writes nothing does not, and a stopped slot keeps its count, so
// a writer that read an old version can never land on it.
func TestPipelineCopiesKeepAVersionThroughSavesAndStops(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c, v, err := st.PipelineCopy("acme/api"); c != nil || v != 1 || err != nil {
		t.Fatalf("a new factory keeps %+v at %d (%v)", c, v, err)
	}
	saved := &factory.PipelineCopy{Repo: "acme/api", Text: "pipeline: {}\n", By: "test-owner", At: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)}
	put := func(next *factory.PipelineCopy, write bool) (*factory.PipelineCopy, int) {
		t.Helper()
		c, v, err := st.ChangePipelineCopy("acme/api", func(*factory.PipelineCopy, int) (*factory.PipelineCopy, bool, error) { return next, write, nil })
		if err != nil {
			t.Fatal(err)
		}
		return c, v
	}
	if c, v := put(saved, true); !c.Same(saved) || v != 2 {
		t.Fatalf("saved %+v at %d", c, v)
	}
	if c, v, _ := st.PipelineCopy("ACME/API"); !c.Same(saved) || v != 2 {
		t.Fatalf("read back %+v at %d", c, v)
	}
	if c, v := put(nil, false); !c.Same(saved) || v != 2 {
		t.Fatalf("a change that writes nothing: %+v at %d", c, v)
	}
	if c, v := put(nil, true); c != nil || v != 3 {
		t.Fatalf("stopped %+v at %d", c, v)
	}
	if c, v, _ := st.PipelineCopy("acme/api"); c != nil || v != 3 {
		t.Fatalf("after a stop %+v at %d", c, v)
	}
	if items, err := st.List(); err != nil || len(items) != 0 {
		t.Fatalf("the copies file reads as jobs: %v %v", items, err)
	}
}
