package worker_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

type stoppedPublisher struct {
	*pullSource
	prepare func()
	sent    func()
}

func (s *stoppedPublisher) Write(ctx context.Context, a factory.Action) (factory.Receipt, error) {
	if s.prepare != nil {
		s.prepare()
	}
	if a.BeforeWrite != nil {
		if err := a.BeforeWrite(ctx); err != nil {
			return factory.Receipt{}, err
		}
	}
	if s.sent != nil {
		s.sent()
	}
	return s.pullSource.Write(context.Background(), a)
}

func TestStopPublishWorkerBoundaries(t *testing.T) {
	for _, point := range []string{"proof_finishing", "publish_started", "after_push", "before_request", "request_sent"} {
		t.Run(point, func(t *testing.T) { testStopPublishWorker(t, point) })
	}
}

func testStopPublishWorker(t *testing.T, point string) {
	checkout := filepath.Join(t.TempDir(), "api")
	git(t, "", "init", "-q", "-b", "main", checkout)
	if err := os.WriteFile(filepath.Join(checkout, "ledger.go"), []byte("package ledger\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, checkout, "add", "ledger.go")
	git(t, checkout, "commit", "-qm", "initial")
	dirs := run.NewWorkdirs(filepath.Join(t.TempDir(), "work"), run.ExecGit{})
	src := &stoppedPublisher{pullSource: &pullSource{}}
	entered, answer := make(chan struct{}), make(chan struct{})
	block := func() { close(entered); <-answer }
	if point == "before_request" {
		src.prepare = block
	}
	if point == "request_sent" {
		src.sent = block
	}
	var pushes atomic.Int32
	var once sync.Once
	release := func() { once.Do(func() { close(answer) }) }
	defer release()
	var mirror *store.Store
	g := newRigWith(t, nil, func(o *run.Options) {
		mirror = o.Store
		o.Source = func(string) factory.Source { return src }
		o.Workdir = func(it factory.Item) (string, error) { return dirs.For(it, checkout) }
		o.Push = func(string, string) error {
			pushes.Add(1)
			if point == "after_push" {
				block()
			}
			return nil
		}
		if point == "publish_started" {
			o.Publisher = func(context.Context, factory.Item) (run.PublishTarget, error) {
				block()
				return run.PublishTarget{Source: src, Push: o.Push}, nil
			}
		}
		if point == "proof_finishing" {
			checkExec := o.Exec[factory.StageCheck]
			o.Exec[factory.StageCheck] = run.ExecutorFunc(func(ctx context.Context, j run.Job) (factory.StageResult, error) {
				res, err := checkExec.Run(ctx, j)
				block()
				return res, err
			})
		}
	})
	write := check("printf 'package ledger\nconst fixed = true\n' > ledger.go")
	write.Name = "proof"
	id := g.add(t, write)
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Autonomy = factory.AutonomyAuto
		it.Starter = "me"
		it.Cap = 10
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	opened := make(chan factory.Item, 1)
	g.st.ObserveWrites(func(before, after factory.Item) {
		if after.PullRequest != nil && after.State == factory.StateShipped {
			select {
			case opened <- after:
			default:
			}
		}
	})
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(8 * time.Second):
		t.Fatal("automatic proof did not send PR request")
	}
	if err := g.floor.Stop(id); err != nil {
		t.Fatal(err)
	}
	release()
	if point != "request_sent" {
		deadline := time.After(8 * time.Second)
		for g.run.Running(id) {
			select {
			case <-deadline:
				t.Fatal("stopped worker did not settle")
			default:
				runtime.Gosched()
			}
		}
		it, _ := g.st.Get(id)
		if it.State != factory.StateNew || len(src.writes()) != 0 {
			t.Fatalf("Stop lost at %s: state=%s requests=%d", point, it.State, len(src.writes()))
		}
		if (point == "proof_finishing" || point == "publish_started") && pushes.Load() != 0 {
			t.Fatalf("stopped worker pushed %d branches", pushes.Load())
		}
		return
	}
	select {
	case it := <-opened:
		if it.PullRequest.URL != "https://github.com/acme/api/pull/7" {
			t.Fatalf("wrong PR: %+v", it.PullRequest)
		}
		var words string
		for _, l := range it.Stream.Log {
			words += l.Text
		}
		if !strings.Contains(words, "not closed") {
			t.Fatal("person was not told the PR remains open")
		}
	case <-time.After(8 * time.Second):
		local, localErr := mirror.Get(id)
		t.Logf("mirror state=%s publishing=%+v failure=%+v error=%v", local.State, local.PublishStatus, local.Failure, localErr)
		paths, _ := filepath.Glob(filepath.Join(mirror.Root(), "delivery", "*.json"))
		for _, p := range paths {
			raw, _ := os.ReadFile(p)
			var d map[string]any
			_ = json.Unmarshal(raw, &d)
			t.Logf("delivery reason=%v", d["reason"])
		}
		it, _ := g.st.Get(id)
		t.Fatalf("floor lost the successful PR receipt: state=%s pr=%+v", it.State, it.PullRequest)
	}
	for _, a := range src.writes() {
		if a.Verb == "close" {
			t.Fatal("closed automatically")
		}
	}
}
