package worker_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/worker"
)

const compatRoom = "0a1b2c3d4e5f"

// compatSpend is the cost the stage writes beside its room, so a lost flush is
// visible as a missing cost and not only as a missing room.
const compatSpend = 0.25

// reportLog is every report body the factory's wrapper saw, in order.
type reportLog struct {
	mu     sync.Mutex
	bodies []string
}

func (l *reportLog) add(b string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bodies = append(l.bodies, b)
}

func (l *reportLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.bodies...)
}

// compatRig is a real factory behind a wrapper that can speak for an engine
// older than the room field, and a worker whose stage records a room and a
// cost on its mirror the way the factory's stage wrapper does.
func compatRig(t *testing.T, wrap func(next http.Handler, seen *reportLog) http.Handler) (rig, *reportLog) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(st, lease.Options{})
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(st, time.Now(), factory.WithRunner(lease.NewDoors(desk)))
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	handler := serve.New(seam, serve.Config{Address: "http://test", Store: st, Record: history, PullWait: 50 * time.Millisecond})
	seen := &reportLog{}
	server := httptest.NewServer(wrap(handler, seen))
	t.Cleanup(func() { server.Close(); handler.Close() })

	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	inner := run.NewCheckExecutor(run.CheckOptions{})
	r := run.New(run.Options{
		Store: mirror,
		Exec: map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(ctx context.Context, job run.Job) (factory.StageResult, error) {
			if err := mirror.Annotate(job.Item.ID, func(it *factory.Item) error {
				if it.Stream != nil {
					it.Stream.Room = compatRoom
					it.Stream.Spent += compatSpend
				}
				return nil
			}); err != nil {
				return factory.StageResult{}, err
			}
			return inner.Run(ctx, job)
		})},
		RepoDir: func(string) string { return dir },
	})
	var mu sync.Mutex
	creds := map[int]string{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx, worker.Options{
			Dial: func(slot int) *client.WorkerClient {
				mu.Lock()
				defer mu.Unlock()
				return client.Open(server.URL, "").Worker(creds[slot])
			},
			Keep: func(slot int, w *client.WorkerClient) {
				mu.Lock()
				defer mu.Unlock()
				creds[slot] = w.Credential()
			},
			Attach:      api.WorkerAttach{Machine: "laptop", Name: "laptop (pid 123)", Handle: "me", TakesUnassigned: map[string]bool{lease.EveryRepo: true}},
			Runner:      r,
			Mirror:      mirror,
			Slots:       1,
			ReportEvery: 20 * time.Millisecond,
			BeatEvery:   20 * time.Millisecond,
		})
	}()
	t.Cleanup(func() { cancel(); <-done })
	return rig{base: server.URL, st: st, floor: client.Open(server.URL, "").Seam(), doors: seam, run: r, record: history}, seen
}

// isReport is whether the request is a worker's report on a lease.
func isReport(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/report")
}

// peek reads a request body and puts it back for the handler behind.
func peek(r *http.Request) string {
	raw, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(strings.NewReader(string(raw)))
	return string(raw)
}

// An engine older than the room field reads every report strictly and answers
// 422 to a body naming one. The worker must lose only the room: the step, the
// log and the cost the same flush would have carried all reach the factory.
func TestAnOldEngineRefusingTheRoomStillReceivesTheRestOfTheReport(t *testing.T) {
	g, seen := compatRig(t, func(next http.Handler, seen *reportLog) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isReport(r) {
				body := peek(r)
				seen.add(body)
				if strings.Contains(body, `"room"`) {
					http.Error(w, `{"error":"json: unknown field \"room\""}`, http.StatusUnprocessableEntity)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	id := g.add(t, check("true"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateShipped)
	if it.Stream == nil || it.Stream.Room != "" {
		t.Fatalf("an old engine cannot hold a room, yet it has %+v", it.Stream)
	}
	if ph := it.Stream.Phases[0]; ph.State != factory.PhaseDone || ph.Result == nil {
		t.Fatalf("the step did not arrive after the room was refused: %+v", ph)
	}
	if it.Stream.Spent < compatSpend {
		t.Fatalf("the cost was lost with the refused room: spent %v, want at least %v", it.Stream.Spent, compatSpend)
	}
	if n := logHas(it, "shipped · habit"); n != 1 {
		t.Fatalf("the log did not arrive after the room was refused: %+v", it.Stream.Log)
	}
	var alone, withRoom int
	for _, b := range seen.all() {
		if strings.Contains(b, `"room"`) {
			withRoom++
			if strings.Contains(b, `"cost"`) || strings.Contains(b, `"log"`) || strings.Contains(b, `"stages"`) {
				t.Fatalf("the room travelled beside real work: %s", b)
			}
			alone++
		}
	}
	if withRoom == 0 || alone != withRoom {
		t.Fatalf("the room was never offered alone: %q", seen.all())
	}
}

// A report that never reached the factory is offered again whole: the baseline
// must not move over the room or the work beside it.
func TestATransientFailureSendingTheRoomKeepsTheBaseline(t *testing.T) {
	var drops int
	var mu sync.Mutex
	g, _ := compatRig(t, func(next http.Handler, seen *reportLog) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isReport(r) {
				body := peek(r)
				seen.add(body)
				mu.Lock()
				drop := strings.Contains(body, `"room"`) && drops < 3
				if drop {
					drops++
				}
				mu.Unlock()
				if drop {
					panic(http.ErrAbortHandler) // the connection dies; nothing is answered
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	id := g.add(t, check("true"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateShipped)
	mu.Lock()
	dropped := drops
	mu.Unlock()
	if dropped != 3 {
		t.Fatalf("the failure was injected %d times, want 3", dropped)
	}
	if it.Stream == nil || it.Stream.Room != compatRoom {
		t.Fatalf("the room was not offered again after the transient failures: %+v", it.Stream)
	}
	if it.Stream.Spent < compatSpend || it.Stream.Phases[0].State != factory.PhaseDone {
		t.Fatalf("work was lost across the transient failures: spent %v, %+v", it.Stream.Spent, it.Stream.Phases)
	}
}

// A current engine takes the room first, as metadata alone, and the full
// report after it.
func TestACurrentEngineReceivesTheRoomFirstThenTheFullReport(t *testing.T) {
	g, seen := compatRig(t, func(next http.Handler, seen *reportLog) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isReport(r) {
				seen.add(peek(r))
			}
			next.ServeHTTP(w, r)
		})
	})
	id := g.add(t, check("true"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateShipped)
	if it.Stream == nil || it.Stream.Room != compatRoom {
		t.Fatalf("the room did not reach a current engine: %+v", it.Stream)
	}
	if it.Stream.Spent < compatSpend || it.Stream.Phases[0].State != factory.PhaseDone {
		t.Fatalf("the full report did not follow the room: spent %v, %+v", it.Stream.Spent, it.Stream.Phases)
	}
	bodies := seen.all()
	first := -1
	for i, b := range bodies {
		if strings.Contains(b, `"room"`) {
			if strings.Contains(b, `"cost"`) || strings.Contains(b, `"log"`) || strings.Contains(b, `"stages"`) {
				t.Fatalf("the room travelled beside real work: %s", b)
			}
			if first < 0 {
				first = i
			}
		}
	}
	if first < 0 {
		t.Fatalf("no report carried the room: %q", bodies)
	}
	for _, b := range bodies[:first] {
		if strings.Contains(b, `"cost"`) {
			t.Fatalf("cost went before the room: %q", bodies)
		}
	}
	if !strings.Contains(strings.Join(bodies[first+1:], "\n"), `"cost"`) {
		t.Fatalf("no full report followed the room: %q", bodies)
	}
}
