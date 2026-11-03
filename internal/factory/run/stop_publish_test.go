package run

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

type stopPublishSource struct {
	*prSource
	before func()
	after  func()
}

func (s stopPublishSource) Write(ctx context.Context, a factory.Action) (factory.Receipt, error) {
	if s.before != nil {
		s.before()
	}
	// GitHub may accept an already sent request despite local cancellation.
	rcpt, err := s.prSource.Write(context.Background(), a)
	if s.after != nil {
		s.after()
	}
	return rcpt, err
}

type stopPublishGit struct {
	Git
	afterPush func()
}

func (g stopPublishGit) Run(dir string, args ...string) (string, error) {
	out, err := g.Git.Run(dir, args...)
	if len(args) > 1 && args[0] == "update-ref" && strings.HasPrefix(args[1], "refs/factory/pushed/") && g.afterPush != nil {
		g.afterPush()
	}
	return out, err
}

func TestStopBeatsPublishBoundaries(t *testing.T) {
	for _, point := range []string{"publish_started", "before_push", "after_push", "request_sent", "github_answered"} {
		t.Run(point, func(t *testing.T) {
			g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "Stop publication"})
			stop := func() {
				if err := g.st.Update(g.id, func(it *factory.Item) error { return StopItem(it, time.Now()) }); err != nil {
					t.Errorf("Stop: %v", err)
				}
			}
			pushes := 0
			originalPush := g.r.opts.Push
			g.r.opts.Push = func(dir, ref string) error { pushes++; return originalPush(dir, ref) }
			switch point {
			case "publish_started":
				g.r.opts.Publisher = func(context.Context, factory.Item) (PublishTarget, error) {
					stop()
					return PublishTarget{Source: g.src, Push: g.r.opts.Push}, nil
				}
			case "before_push":
				workdir := g.r.opts.Workdir
				g.r.opts.Workdir = func(it factory.Item) (string, error) { stop(); return workdir(it) }
			case "after_push":
				g.r.opts.Git = stopPublishGit{Git: ExecGit{}, afterPush: stop}
			case "request_sent":
				g.r.opts.Source = func(string) factory.Source { return stopPublishSource{prSource: g.src, before: stop} }
			case "github_answered":
				g.r.opts.Source = func(string) factory.Source { return stopPublishSource{prSource: g.src, after: stop} }
			}
			if err := g.r.Publish(g.id, testTrailer); err != nil {
				t.Fatal(err)
			}
			deadline := time.After(10 * time.Second)
			for g.r.Running(g.id) {
				select {
				case <-deadline:
					t.Fatal("publish did not settle")
				default:
					runtime.Gosched()
				}
			}
			it := g.item(t)
			late := point == "request_sent" || point == "github_answered"
			if !late {
				if len(g.src.got) != 0 {
					t.Errorf("stopped job sent %d PR requests", len(g.src.got))
				}
				if point != "after_push" && pushes != 0 {
					t.Errorf("stopped job pushed %d branches", pushes)
				}
			} else {
				if it.PullRequest == nil || it.PullRequest.URL != "https://github.com/acme/api/pull/7" || it.State != factory.StateShipped {
					t.Errorf("PR opened but receipt lost: state=%s pr=%+v", it.State, it.PullRequest)
				}
				if it.Stream == nil || !strings.Contains(logText(it), "not closed") {
					t.Error("person was not told PR remains open")
				}
				for _, a := range g.src.got {
					if a.Verb == "close" {
						t.Error("closed PR automatically")
					}
				}
			}
		})
	}
}

func logText(it factory.Item) string {
	var out string
	if it.Stream != nil {
		for _, l := range it.Stream.Log {
			out += l.Text + "\n"
		}
	}
	return out
}

func TestStopBeatsPublishWorkerReport(t *testing.T) {
	it := factory.Item{State: factory.StateRunning, Publishing: true, Stream: &factory.Stream{}}
	now := time.Now()
	if err := StopItem(&it, now); err != nil {
		t.Fatal(err)
	}
	report := factory.RunReport{PR: &factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen}, Publishing: &factory.Publishing{State: "opened", Attempt: 1}}
	if err := ApplyReport(&it, report, now); err != nil {
		t.Fatalf("successful in-flight PR receipt rejected after Stop: %v", err)
	}
	if it.State != factory.StateShipped || it.PullRequest == nil || !strings.Contains(logText(it), "not closed") {
		t.Fatalf("PR receipt not shown after Stop: %+v", it)
	}
}

func TestStopBeatsPublishProofAndBetweenSteps(t *testing.T) {
	for _, point := range []string{"proof_finishing", "between_steps"} {
		t.Run(point, func(t *testing.T) {
			g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
				return factory.StageResult{Done: true, Claims: []factory.Claim{{Text: "works", OK: true, Evidence: "scripted test", Medium: "test"}}}, nil
			})}, nil)
			id := g.add("Stop finishing proof", chat("proof"))
			stopped := false
			g.st.ObserveWrites(func(before, after factory.Item) {
				if stopped || after.Stream == nil || len(after.Stream.Phases) == 0 {
					return
				}
				ph := after.Stream.Phases[0]
				ready := ph.Result != nil
				if point == "between_steps" {
					ready = ph.State == factory.PhaseDone
				}
				if !ready {
					return
				}
				stopped = true
				if err := g.st.Update(id, func(it *factory.Item) error { return StopItem(it, time.Now()) }); err != nil {
					t.Error(err)
				}
			})
			if err := g.r.Launch(id); err != nil {
				t.Fatal(err)
			}
			deadline := time.After(10 * time.Second)
			for g.r.Running(id) {
				select {
				case <-deadline:
					t.Fatal("proof did not settle")
				default:
					runtime.Gosched()
				}
			}
			it, err := g.st.Get(id)
			if err != nil || !stopped || it.State != factory.StateNew || it.Publishing {
				t.Fatalf("Stop lost at %s: state=%s publishing=%v stopped=%v err=%v", point, it.State, it.Publishing, stopped, err)
			}
		})
	}
}
