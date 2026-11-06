package plandb

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func treeBoundStore(t *testing.T) (*cliHarness, *Store) {
	t.Helper()
	t.Setenv(RunEnv, "")
	t.Setenv(TaskEnv, "")
	h := cliNewHarness(t)
	h.cliInitFresh()
	s, err := Open(h.db, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return h, s
}

func treeBoundAdd(t *testing.T, s *Store, specs ...TaskSpec) {
	t.Helper()
	if _, err := s.AddMany(specs); err != nil {
		t.Fatal(err)
	}
}

// L-LOOP C2a, C2b, C2c, C2d: every growth verb refuses the whole write.
func TestRunWorkerRefusesDuplicateTitles(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"own", []string{"add", "Create green.txt", "--parent", "green"}},
		{"ancestor", []string{"add", "DEMO!", "--parent", "green"}},
		{"sibling", []string{"add", "CREATE---GREEN TXT", "--parent", "red"}},
		{"batch", []string{"split", "red", "--into", "New work, NEW-work"}},
		{"insert", []string{"task", "insert", "--after", "red", "--title", "Create green.txt"}},
		{"pivot", []string{"task", "pivot", "red", "--subtasks", "Fresh, Create green.txt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, s := treeBoundStore(t)
			treeBoundAdd(t, s, TaskSpec{ID: "green", Title: "Create green.txt"}, TaskSpec{ID: "red", Title: "Create red.txt"})
			t.Setenv(RunEnv, s.RootID())
			before := s.Tasks()
			if code := h.run(append([]string{"--db", h.db}, tc.args...)...); code == 0 {
				t.Fatal("duplicate was accepted")
			}
			if !strings.Contains(h.errb.String(), "nothing was added") || !strings.Contains(h.errb.String(), "own hands") {
				t.Fatalf("refusal gives no next action: %s", h.errb)
			}
			if !reflect.DeepEqual(before, s.Tasks()) {
				t.Fatal("refused write changed the plan")
			}
		})
	}
}

// L-LOOP C2e: a person's unbound CLI keeps its existing duplicate-title use.
func TestUnboundPlanCLIStillAllowsDuplicateTitles(t *testing.T) {
	h, s := treeBoundStore(t)
	h.cliAdd("Same work", "one")
	h.cliAdd("SAME-work", "two")
	if len(s.Tasks()) != 3 {
		t.Fatal("unbound CLI lost a task")
	}
}

// L-LOOP C3a, C3b, C3d: a batch cannot partly cross either bound.
func TestRunWorkerTreeBoundsRefuseWholeBatches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		args  []string
		want  string
	}{
		{"count", TreeTaskLimit, []string{"add", "One too many"}, "20 tasks"},
		{"batch", TreeTaskLimit - 1, []string{"split", "root", "--into", "Extra one, Extra two"}, "20 tasks"},
		{"depth", 0, []string{"add", "Too deep", "--parent", "deep"}, "2 levels below"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, s := treeBoundStore(t)
			for i := 0; i < tc.count; i++ {
				treeBoundAdd(t, s, TaskSpec{ID: fmt.Sprintf("part%d", i), Title: fmt.Sprintf("Part %d", i)})
			}
			if tc.name == "depth" {
				treeBoundAdd(t, s, TaskSpec{ID: "middle", Title: "Middle"}, TaskSpec{ID: "deep", ParentID: "middle", Title: "Deep"})
			}
			t.Setenv(RunEnv, s.RootID())
			before := s.Tasks()
			if code := h.run(append([]string{"--db", h.db}, tc.args...)...); code == 0 || !strings.Contains(h.errb.String(), tc.want) {
				t.Fatalf("bound was not explained: exit %d, %s", code, h.errb)
			}
			if !reflect.DeepEqual(before, s.Tasks()) {
				t.Fatal("refused batch changed the plan")
			}
		})
	}
}

// L-LOOP C3c and C2: root/review/cancelled rows do not consume work slots;
// cancelled titles can be reused, but the root's title still cannot.
func TestTreeGrowthExclusions(t *testing.T) {
	h, s := treeBoundStore(t)
	treeBoundAdd(t, s, TaskSpec{ID: "cancelled", Title: "Reusable"})
	if _, err := s.AddReviewCheck(TaskSpec{ID: "check", Title: "Review", Role: RoleCheck}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cancel("cancelled", "no longer needed"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < TreeTaskLimit-1; i++ {
		treeBoundAdd(t, s, TaskSpec{ID: fmt.Sprintf("part%d", i), Title: fmt.Sprintf("Part %d", i)})
	}
	t.Setenv(RunEnv, s.RootID())
	h.cliAdd("Reusable", "replacement")
	if refusal := s.GrowthRefusal(s.RootID(), []string{"demo"}); !strings.Contains(refusal, "already has a task named") {
		t.Fatalf("root title was excluded: %s", refusal)
	}
}

// L-LOOP C3a, C3c: choosing the check seat cannot evade a worker's count.
func TestRunWorkerCreatedChecksConsumeTreeSlots(t *testing.T) {
	h, s := treeBoundStore(t)
	t.Setenv(RunEnv, s.RootID())
	for i := 0; i < TreeTaskLimit; i++ {
		h.cliAdd(fmt.Sprintf("Inspect part %d", i), fmt.Sprintf("check%d", i), "--role", "check", "--parent", s.RootID())
	}
	before := s.Tasks()
	if code := h.run("--db", h.db, "add", "Inspect one more part", "--role", "check", "--parent", s.RootID()); code == 0 || !strings.Contains(h.errb.String(), "20 tasks") {
		t.Fatalf("worker-created checks escaped the count: exit %d, %s", code, h.errb)
	}
	if !reflect.DeepEqual(before, s.Tasks()) {
		t.Fatal("refused check changed the plan")
	}
}

// L-LOOP C2, C3: concurrent handles enforce growth against the write snapshot.
func TestBoundedWritesCannotRacePastTheTreeLimit(t *testing.T) {
	_, s := treeBoundStore(t)
	other, err := Open(s.path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	for i := 0; i < TreeTaskLimit-1; i++ {
		treeBoundAdd(t, s, TaskSpec{ID: fmt.Sprintf("part%d", i), Title: fmt.Sprintf("Part %d", i)})
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, handle := range []*Store{s, other} {
		wg.Add(1)
		go func(i int, handle *Store) {
			defer wg.Done()
			_, err := handle.AddManyBounded([]TaskSpec{{ID: fmt.Sprintf("extra%d", i), Title: fmt.Sprintf("Extra %d", i)}})
			errs <- err
		}(i, handle)
	}
	wg.Wait()
	close(errs)
	accepted := 0
	for err := range errs {
		if err == nil {
			accepted++
		}
	}
	if accepted != 1 || len(s.Tasks()) != TreeTaskLimit+1 {
		t.Fatalf("accepted %d writes; plan holds %d rows", accepted, len(s.Tasks()))
	}
}

// L-LOOP C2, C3: archived work is still part of the same run's allowance.
func TestArchivingWorkDoesNotResetTreeGrowth(t *testing.T) {
	h, s := treeBoundStore(t)
	now := time.Now()
	s.now = func() time.Time { return now.Add(-2 * time.Hour) }
	for i := 0; i < TreeTaskLimit; i++ {
		treeBoundAdd(t, s, TaskSpec{ID: fmt.Sprintf("part%d", i), Title: fmt.Sprintf("Part %d", i)})
	}
	if _, err := s.Claim("part0", "part0", "test-owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Done("part0", "part0", "finished", nil, nil); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now }
	if archived, err := s.Archive(time.Hour); err != nil || len(archived) != 1 {
		t.Fatalf("archive: %v, %+v", err, archived)
	}
	t.Setenv(RunEnv, s.RootID())
	before := s.Tasks()
	for _, title := range []string{"PART---0", "Different work"} {
		if code := h.run("--db", h.db, "add", title); code == 0 || !strings.Contains(h.errb.String(), "nothing was added") {
			t.Fatalf("archived work reset the allowance: exit %d, %s", code, h.errb)
		}
	}
	if !reflect.DeepEqual(before, s.Tasks()) {
		t.Fatal("refused write changed the plan")
	}
}
