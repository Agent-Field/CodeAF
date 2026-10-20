package serve_test

import (
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A MODEL PICK IS NEVER STALE. Opening a job's chat keeps the conversation on
// the item, which moves its version under the page that had just read it; the
// first pick then carried a version one behind and was refused. A pick sent
// with that stale version still sets the model, and so does one whose state and
// step no longer match.
func TestAModelPickWithAStaleVersionStillSetsTheModel(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam"})
	read := f.read(t, it).Version
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.Talk = "chat-1"; return nil }); err != nil {
		t.Fatal(err)
	}
	if now := f.read(t, it).Version; now == read {
		t.Fatalf("keeping the chat did not move the version (%d), so this proves nothing", now)
	}
	f.post(t, it, "model", fmt.Sprintf(`{"model":"vendor/model-a","expect":{"version":%d}}`, read), 200)
	if j := f.read(t, it); j.Model != "vendor/model-a" {
		t.Fatalf("model = %q", j.Model)
	}
	f.post(t, it, "model", fmt.Sprintf(`{"model":"vendor/model-b","expect":{"version":%d,"state":"running","step":"plan"}}`, read), 200)
	if j := f.read(t, it); j.Model != "vendor/model-b" {
		t.Fatalf("model = %q", j.Model)
	}
}

// The state guard stays: a Done job has no step left to run on a model, so a
// pick on one is refused whatever version it carries, the current one too.
func TestAModelPickOnADoneJobIsStillRefused(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateShipped, Assignee: "sam", PullRequest: &factory.PullRequest{State: factory.PRMerged}})
	version := f.read(t, it).Version
	f.post(t, it, "model", fmt.Sprintf(`{"model":"vendor/model-a","expect":{"version":%d}}`, version), 409)
	f.post(t, it, "model", `{"model":"vendor/model-a"}`, 409)
	if j := f.read(t, it); j.Model != "" {
		t.Fatalf("a Done job took a model: %q", j.Model)
	}
}
