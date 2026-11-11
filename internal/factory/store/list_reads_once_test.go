package store

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A floor read decodes each job document once. Hydration read every document
// a second time even when no tier lookup ran, which doubled the cost of every
// List and Get on a floor of hundreds of synced issues.
func TestListDecodesEachDocumentOnce(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const n = 40
	var last factory.Item
	for i := 0; i < n; i++ {
		last, err = st.Create(factory.Item{Repo: "agentfield/codeaf", Title: fmt.Sprintf("job %d", i), Body: strings.Repeat("a body that costs something to decode ", 20)})
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(st.ItemPath(last.ID))
	if err != nil {
		t.Fatal(err)
	}
	decode := testing.AllocsPerRun(10, func() {
		var d doc
		if err := json.Unmarshal(data, &d); err != nil {
			t.Fatal(err)
		}
	})
	list := testing.AllocsPerRun(5, func() {
		items, err := st.List()
		if err != nil || len(items) != n {
			t.Fatalf("list = %d items, %v", len(items), err)
		}
	})
	// One decode per document, plus the directory read and sorting.
	if limit := n*decode*1.4 + 200; list > limit {
		t.Fatalf("List allocated %v times for %d documents of %v allocations each (limit %v): documents are decoded more than once", list, n, decode, limit)
	}
	get := testing.AllocsPerRun(10, func() {
		if _, err := st.Get(last.ID); err != nil {
			t.Fatal(err)
		}
	})
	if limit := decode*1.4 + 20; get > limit {
		t.Fatalf("Get allocated %v times for one document of %v allocations (limit %v)", get, decode, limit)
	}
}
