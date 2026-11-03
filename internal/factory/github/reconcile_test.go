package github_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	source "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/scout"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/triage"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

type issueScript struct {
	t           *testing.T
	st          *store.Store
	src         *source.Source
	api         source.API
	cursor      string
	state       map[int]string
	identity    map[int]int64
	status      map[int]int
	redirect    map[int]bool
	lists       []map[string]any
	reads       []int
	during      func(int)
	now         time.Time
	notModified int
	listCalls   int
}

func newIssueScript(t *testing.T, count int) *issueScript {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	f := &issueScript{t: t, st: st, state: map[int]string{}, identity: map[int]int64{}, status: map[int]int{}, redirect: map[int]bool{}, now: time.Now()}
	for n := 1; n <= count; n++ {
		f.state[n] = "open"
		f.identity[n] = int64(1000 + n)
		if _, err := st.Create(factory.Item{Product: "acme", Repo: "api", Num: n, Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew, Title: "issue"}); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user":
			fmt.Fprint(w, `{"login":"owner"}`)
		case r.URL.Path == "/repos/acme/api/issues":
			f.listCalls++
			body, _ := json.Marshal(f.lists)
			tag := fmt.Sprintf(`"%x"`, sha256.Sum256(append([]byte(r.URL.RawQuery), body...)))
			if r.Header.Get("If-None-Match") == tag {
				f.notModified++
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", tag)
			_, _ = w.Write(body)
		case strings.HasPrefix(r.URL.Path, "/repos/acme/api/issues/"):
			n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/repos/acme/api/issues/"))
			f.reads = append(f.reads, n)
			if f.during != nil {
				f.during(n)
			}
			if f.redirect[n] {
				http.Redirect(w, r, "/transferred", http.StatusMovedPermanently)
				return
			}
			if f.status[n] != 0 {
				w.WriteHeader(f.status[n])
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": f.identity[n], "number": n, "state": f.state[n], "title": "issue"})
		case r.URL.Path == "/transferred":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1001, "number": 1, "state": "closed", "title": "issue"})
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	f.api = forge.NewClient("reconcile-" + t.Name()).WithBaseURL(srv.URL)
	f.src = source.New(f.api, nil, func() time.Time { return f.now })
	return f
}
func (f *issueScript) poll() {
	f.t.Helper()
	f.now = f.now.Add(time.Minute)
	next, err := source.PollOnce(context.Background(), f.src, f.st, f.cursor, func() time.Time { return f.now })
	if err != nil {
		f.t.Fatal(err)
	}
	f.cursor = next
}
func (f *issueScript) row(n int) factory.Item {
	f.t.Helper()
	it, err := f.st.Get(n)
	if err != nil {
		f.t.Fatal(err)
	}
	return it
}
func (f *issueScript) list(n int, state string) {
	f.lists = []map[string]any{{"id": f.identity[n], "number": n, "state": state, "title": "issue", "updated_at": f.now}}
}

func TestReopenedIssueReturnsOnNextPoll(t *testing.T) {
	for _, waiting := range []int{0, 45} {
		t.Run(strconv.Itoa(waiting), func(t *testing.T) {
			f := newIssueScript(t, waiting+2)
			reopened, personal := waiting+1, waiting+2
			if err := f.st.Update(personal, func(it *factory.Item) error {
				it.State, it.DoneHow = factory.StateShipped, "closed"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			f.state[reopened] = "closed"
			for n := 1; n <= waiting; n++ {
				f.lists = append(f.lists, map[string]any{"id": f.identity[n], "number": n, "state": "open", "title": "issue", "updated_at": f.now})
			}
			f.lists = append(f.lists, map[string]any{"id": f.identity[reopened], "number": reopened, "state": "closed", "title": "issue", "updated_at": f.now})
			f.poll()
			if !f.row(reopened).SourceClosed {
				t.Fatal("automatic close missing")
			}
			f.poll()
			f.poll()
			if f.notModified == 0 || f.listCalls != 3 {
				t.Fatal("quiet poll did not conditionally request the list")
			}
			f.state[reopened] = "open"
			f.list(reopened, "open")
			for n := 1; n <= waiting; n++ {
				f.lists = append(f.lists, map[string]any{"id": f.identity[n], "number": n, "state": "closed", "title": "issue", "updated_at": f.now})
			}
			start := len(f.reads)
			f.poll()
			if got := f.row(reopened); got.State != factory.StateNew || got.SourceClosed {
				t.Fatalf("reopened issue stayed Done after next poll with %d waiting rows", waiting)
			}
			if len(f.reads) == start || f.reads[start] != reopened {
				t.Fatal("reopen was not read ahead of waiting rows")
			}
			if len(f.reads)-start > 40 {
				t.Fatal("reopen exceeded direct-read cap")
			}
			if f.row(personal).State != factory.StateShipped {
				t.Fatal("person's Done came back")
			}
		})
	}
}

func TestClosedListOnlyNominates(t *testing.T) {
	f := newIssueScript(t, 1)
	f.list(1, "closed") // The list is stale; the authoritative answer is open.
	f.poll()
	if got := f.row(1); got.State != factory.StateNew {
		t.Fatalf("stale closed list finished open issue: state=%s", got.State)
	}
	if len(f.reads) != 1 {
		t.Fatalf("stale list used %d direct reads, want 1", len(f.reads))
	}
}

func TestAutomaticCloseReopensButPersonsDoneDoesNot(t *testing.T) {
	f := newIssueScript(t, 2)
	f.state[1] = "closed"
	f.state[2] = "open"
	if err := f.st.Update(2, func(it *factory.Item) error { it.State = factory.StateShipped; it.DoneHow = "closed"; return nil }); err != nil {
		t.Fatal(err)
	}
	f.poll()
	if got := f.row(1); !got.SourceClosed || got.State != factory.StateShipped {
		t.Fatalf("automatic close missing: %+v", got)
	}
	f.state[1] = "open"
	f.list(1, "closed")
	f.poll()
	if got := f.row(1); got.State != factory.StateNew || got.SourceClosed {
		t.Fatal("direct open answer did not restore Backlog")
	}
	if got := f.row(2); got.State != factory.StateShipped {
		t.Fatal("person's Done came back")
	}
}

func TestIssueIdentityAndTransfers(t *testing.T) {
	for _, mode := range []string{"reuse", "redirect", "old row"} {
		t.Run(mode, func(t *testing.T) {
			f := newIssueScript(t, 1)
			f.poll()
			if f.row(1).GitHubIssueID != 1001 {
				t.Fatal("first direct read did not store permanent identity")
			}
			f.state[1] = "closed"
			switch mode {
			case "reuse":
				f.identity[1] = 9001
			case "redirect":
				f.redirect[1] = true
			case "old row":
				if err := f.st.Update(1, func(it *factory.Item) error { it.GitHubIssueID = 0; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			f.list(1, "closed")
			f.poll()
			want := factory.StateNew
			if mode == "old row" {
				want = factory.StateShipped
			}
			if got := f.row(1); got.State != want {
				t.Fatalf("%s moved incorrectly: %s", mode, got.State)
			}
		})
	}
}

func TestFailedIssueReadChangesNothingAndRetries(t *testing.T) {
	for _, status := range []int{403, 404, 410, 429, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			f := newIssueScript(t, 1)
			f.state[1] = "closed"
			f.status[1] = status
			before := f.row(1)
			f.poll()
			after := f.row(1)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("failed read changed row: before=%+v after=%+v", before, after)
			}
			count := len(f.reads)
			f.poll()
			if len(f.reads) != count {
				t.Fatal("failed read retried before backoff")
			}
			f.status[1] = 0
			f.poll()
			if f.row(1).State != factory.StateShipped {
				t.Fatal("retry lost nomination")
			}
		})
	}
}

func TestPersonActionClearsAutomaticCloseMark(t *testing.T) {
	f := newIssueScript(t, 1)
	f.state[1] = "closed"
	f.poll()
	seam := factory.LocalSeam(f.st, time.Now())
	if err := seam.SetCap(1, 7); err != nil {
		t.Fatal(err)
	}
	if got := f.row(1); got.SourceClosed || !got.PersonTouched {
		t.Fatal("person's budget edit kept automatic close provenance")
	}
	f.state[1] = "open"
	f.poll()
	if f.row(1).State != factory.StateShipped {
		t.Fatal("edited Done row returned by itself")
	}
}

func TestCloseReadBudgetOldestFirstAndRestart(t *testing.T) {
	f := newIssueScript(t, 45)
	for n := 1; n <= 45; n++ {
		f.state[n] = "closed"
	}
	f.poll()
	if len(f.reads) != 40 {
		t.Fatalf("first poll reads=%d, want 40", len(f.reads))
	}
	for i, n := range f.reads {
		if n != i+1 {
			t.Fatalf("read %d got #%d, want #%d", i, n, i+1)
		}
	}
	f.poll()
	if len(f.reads) != 80 {
		t.Fatalf("second poll reads=%d, want 80", len(f.reads))
	}
	if !reflect.DeepEqual(f.reads[40:45], []int{41, 42, 43, 44, 45}) {
		t.Fatalf("oldest nominations starved: %v", f.reads[40:45])
	}
	source.ForgetSweeps()
	f.src = source.New(f.api, nil, func() time.Time { return f.now })
	f.cursor = ""
	f.state[45] = "open"
	f.poll()
	f.poll()
	if f.row(45).State != factory.StateNew {
		t.Fatal("restart failed to rebuild reopen checks")
	}
}

func TestClosedPollNeverStopsRunningOrQueuedJobs(t *testing.T) {
	f := newIssueScript(t, 2)
	for n, state := range []factory.State{factory.StateRunning, factory.StateQueued} {
		f.state[n+1] = "closed"
		if err := f.st.Update(n+1, func(it *factory.Item) error { it.State = state; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	f.poll()
	if len(f.reads) != 0 || f.row(1).State != factory.StateRunning || f.row(2).State != factory.StateQueued {
		t.Fatal("poll interfered with work")
	}
	calls := 0
	if !factory.ClosedForAutomation(context.Background(), f.st.Root(), f.row(1), func(context.Context, factory.Item) (factory.SourceStanding, error) {
		calls++
		return factory.SourceStanding{How: "closed"}, nil
	}) || calls != 1 {
		t.Fatal("separate spend guard missing")
	}
}

func TestClosedIssueRandomSequences(t *testing.T) {
	for _, seed := range []int64{7, 52, 2280} {
		t.Run(strconv.FormatInt(seed, 10), func(t *testing.T) {
			f := newIssueScript(t, 6)
			_ = f.st.Update(6, func(it *factory.Item) error { it.State = factory.StateShipped; it.DoneHow = "closed"; return nil })
			rng := rand.New(rand.NewSource(seed))
			for step := 0; step < 120; step++ {
				n := 1 + rng.Intn(6)
				switch rng.Intn(12) {
				case 0:
					f.state[n] = "closed"
				case 1:
					f.state[n] = "open"
				case 2:
					f.redirect[n] = !f.redirect[n]
				case 3:
					f.identity[n] += 10000
				case 4:
					seam := factory.LocalSeam(f.st, time.Now())
					if err := seam.SetCap(n, float64(step+1)); err != nil {
						t.Fatal(err)
					}
				case 5:
					if err := f.st.Update(n, func(it *factory.Item) error { it.Scout = &factory.Scout{Plan: []string{"inspect"}}; return nil }); err != nil {
						t.Fatal(err)
					}
				case 6:
					source.ForgetSweeps()
					f.src = source.New(f.api, nil, func() time.Time { return f.now })
					f.cursor = ""
				case 7:
					f.status[n] = 404
				case 8:
					f.status[n] = 0
				case 9:
					row := f.row(n)
					fresh, err := f.src.Refetch(context.Background(), row)
					if err == nil {
						if err := source.Merge(f.st, source.Name, []factory.Item{fresh}); err != nil {
							t.Fatal(err)
						}
						if f.row(n).GitHubIssueID != f.identity[n] {
							t.Fatalf("seed=%d step=%d direct read identity lost", seed, step)
						}
					}
				case 10:
					if err := f.st.Update(n, func(it *factory.Item) error {
						it.Stages = factory.DefaultRecipe().Stages
						it.Stages[0].On = false
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					if err := f.st.Update(n, func(it *factory.Item) error { it.PersonTouched = false; return nil }); err != nil {
						t.Fatal(err)
					}
				case 11:
					row := f.row(n)
					closed := factory.ClosedForAutomation(context.Background(), "", row, func(ctx context.Context, it factory.Item) (factory.SourceStanding, error) {
						return source.Standing(ctx, f.api, it)
					})
					if row.GitHubIssueID != 0 && row.GitHubIssueID != f.identity[n] && closed {
						t.Fatalf("seed=%d step=%d replacement blocked automation", seed, step)
					}
				}
				// Lists deliberately disagree with direct reads, and arrive reordered.
				f.lists = nil
				for _, i := range rng.Perm(6) {
					if rng.Intn(3) == 0 {
						continue
					}
					state := "open"
					if rng.Intn(2) == 0 {
						state = "closed"
					}
					f.lists = append(f.lists, map[string]any{"id": f.identity[i+1], "number": i + 1, "title": fmt.Sprintf("issue %d", step), "state": state, "updated_at": f.now})
				}
				before := make([]factory.Item, 6)
				for i := range before {
					before[i] = f.row(i + 1)
				}
				start := len(f.reads)
				f.poll()
				read := map[int]bool{}
				for _, i := range f.reads[start:] {
					read[i] = true
				}
				for i, old := range before {
					num := i + 1
					got := f.row(num)
					if old.PersonTouched && (got.State != old.State || got.DoneHow != old.DoneHow) {
						t.Fatalf("seed=%d step=%d touched #%d moved", seed, step, num)
					}
					if old.State == factory.StateShipped && !old.SourceClosed && got.State != old.State {
						t.Fatalf("seed=%d step=%d person's Done came back", seed, step)
					}
					if read[num] && f.status[num] != 0 && (got.State != old.State || got.GitHubIssueID != old.GitHubIssueID || got.SourceClosed != old.SourceClosed) {
						t.Fatalf("seed=%d step=%d failed read changed #%d", seed, step, num)
					}
					same := old.GitHubIssueID == 0 || old.GitHubIssueID == f.identity[num]
					if !same && (got.Title != old.Title || got.GitHubIssueID != old.GitHubIssueID) {
						t.Fatalf("seed=%d step=%d replacement changed pinned row", seed, step)
					}
					if !factory.UntouchedIssue(old) && got.State != old.State {
						t.Fatalf("seed=%d step=%d edited row moved", seed, step)
					}
					listedOpen := false
					for _, listed := range f.lists {
						if listed["number"] == num && listed["state"] == "open" {
							listedOpen = true
						}
					}
					if (read[num] || listedOpen) && same && !f.redirect[num] && f.status[num] == 0 && f.state[num] == "open" && old.SourceClosed && factory.UntouchedIssue(old) && got.State != factory.StateNew {
						t.Fatalf("seed=%d step=%d direct-open #%d stayed Done", seed, step, num)
					}
				}
			}
		})
	}
}

func TestPersonalWorkDuringDirectReadWins(t *testing.T) {
	for _, action := range []string{"edit", "look closer", "start", "arrival read"} {
		t.Run(action, func(t *testing.T) {
			f := newIssueScript(t, 1)
			f.state[1] = "closed"
			f.during = func(int) {
				f.during = nil
				switch action {
				case "edit":
					if err := factory.LocalSeam(f.st, time.Now()).SetCap(1, 9); err != nil {
						t.Error(err)
					}
				case "look closer":
					_ = f.st.Update(1, func(it *factory.Item) error { it.Scout = &factory.Scout{}; return nil })
				case "start":
					_ = f.st.Update(1, func(it *factory.Item) error { it.State = factory.StateQueued; return nil })
				case "arrival read":
					triage.ReadOne(context.Background(), f.st, func(context.Context, string) (string, error) { return `{"read":"inspect"}`, nil }, triage.Options{})
				}
			}
			f.poll()
			if f.row(1).State == factory.StateShipped {
				t.Fatal("direct read overwrote concurrent work")
			}
		})
	}
}

func TestLookCloserAdmissionPreventsAutomaticClose(t *testing.T) {
	f := newIssueScript(t, 1)
	f.state[1] = "closed"
	d := scout.Door{Floor: f.st, Now: time.Now, Limits: scout.DefaultLimits,
		Head: func(context.Context, string) (string, error) { return "head", nil },
		Run:  func(context.Context, factory.Item) (factory.Scout, error) { f.poll(); return factory.Scout{}, nil },
	}
	if _, _, err := d.Look(context.Background(), 1, false); err != nil {
		t.Fatal(err)
	}
	if f.row(1).State != factory.StateNew {
		t.Fatal("Look closer spent while poll finished the row")
	}
}

func TestLegacyPersonalEditsAreUntouchedGateInputs(t *testing.T) {
	for _, name := range []string{"budget", "model", "steps", "marked"} {
		t.Run(name, func(t *testing.T) {
			f := newIssueScript(t, 1)
			f.state[1] = "closed"
			if err := f.st.Update(1, func(it *factory.Item) error {
				switch name {
				case "budget":
					it.Cap = 7
				case "model":
					it.Model = "chosen"
				case "marked":
					it.Marked = true
				case "steps":
					it.Stages = []factory.Stage{{Name: "review", By: factory.ByYou}}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			// This models an existing row written before touch provenance was stored.
			if err := f.st.Update(1, func(it *factory.Item) error { it.PersonTouched = false; return nil }); err != nil {
				t.Fatal(err)
			}
			f.poll()
			if f.row(1).State != factory.StateNew {
				t.Fatal("legacy personal edit was finished")
			}
		})
	}
}

func TestFailedDirectReadUsesOneRequest(t *testing.T) {
	f := newIssueScript(t, 1)
	f.status[1] = 500
	f.poll()
	if len(f.reads) != 1 {
		t.Fatalf("failed direct read used %d requests; budget counts requests", len(f.reads))
	}
}

func TestTransferredIssueRefetchDoesNotReadNumberZero(t *testing.T) {
	f := newIssueScript(t, 1)
	f.redirect[1] = true
	if _, err := f.src.Refetch(context.Background(), f.row(1)); err == nil {
		t.Fatal("transferred issue refresh accepted redirected data")
	}
	if len(f.reads) != 1 {
		t.Fatalf("transfer followed into another issue: %v", f.reads)
	}
}

func TestFailedLookCloserStillProtectsSpentRow(t *testing.T) {
	f := newIssueScript(t, 1)
	f.state[1] = "closed"
	d := scout.Door{Floor: f.st, Now: time.Now, Limits: scout.DefaultLimits,
		Head: func(context.Context, string) (string, error) { return "head", nil },
		Run: func(context.Context, factory.Item) (factory.Scout, error) {
			f.poll()
			return factory.Scout{USD: .01}, errors.New("scripted failure")
		},
	}
	if _, _, err := d.Look(context.Background(), 1, false); err == nil {
		t.Fatal("expected scripted failure")
	}
	f.poll()
	if got := f.row(1); got.State != factory.StateNew || !got.PersonTouched {
		t.Fatal("failed paid Look closer was finished")
	}
}

func TestRefreshClearsAutomaticCloseMark(t *testing.T) {
	f := newIssueScript(t, 1)
	f.state[1] = "closed"
	f.poll()
	seam := factory.LocalSeam(f.st, time.Now(), factory.WithRefetch(func(context.Context, factory.Item) error { return nil }))
	if err := seam.Refresh(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if f.row(1).SourceClosed {
		t.Fatal("personal Refresh retained automatic close mark")
	}
	f.state[1] = "open"
	f.poll()
	if f.row(1).State != factory.StateShipped {
		t.Fatal("personal Refresh was undone by reopen")
	}
}

func TestPersonalMetadataClearsAutomaticCloseMark(t *testing.T) {
	f := newIssueScript(t, 1)
	f.state[1] = "closed"
	f.poll()
	if err := f.st.UpdatePersonal(1, func(it *factory.Item) bool { it.BriefSeen = map[string]int{"sam": 1}; return true }); err != nil {
		t.Fatal(err)
	}
	// Exercise an in-place edit of existing personal metadata as well.
	if err := f.st.Update(1, func(it *factory.Item) error { it.PersonTouched = false; it.SourceClosed = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := f.st.UpdatePersonal(1, func(it *factory.Item) bool { it.BriefSeen["sam"] = 2; return true }); err != nil {
		t.Fatal(err)
	}
	if f.row(1).SourceClosed {
		t.Fatal("personal metadata action retained automatic close mark")
	}
}

func TestLegacyPaidArrivalReadProtectsRow(t *testing.T) {
	f := newIssueScript(t, 1)
	f.state[1] = "closed"
	if err := f.st.Annotate(1, func(it *factory.Item) error { it.Triage.TriagedAt = time.Now(); return nil }); err != nil {
		t.Fatal(err)
	}
	f.poll()
	if f.row(1).State != factory.StateNew {
		t.Fatal("previously paid arrival read was finished")
	}
}
