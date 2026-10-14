package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

var h1 = regexp.MustCompile(`prompt: "[^"]*" is not an app file`)

// lab is one served process on one road, with the socket the desk would hold.
type lab struct {
	judge string
	out   string
	road  string
	srv   *server
	wire  *wire
	hear  *collector
}

func newLab(ctx context.Context, bin, road, model, home, out, judge string) (*lab, error) {
	srv, err := startServe(ctx, bin, road, model, home)
	if err != nil {
		return nil, err
	}
	hear := newCollector()
	w, err := dial(ctx, srv.hello.URL, srv.hello.Token, hear)
	if err != nil {
		srv.stop()
		return nil, err
	}
	return &lab{judge: judge, out: out, road: road, srv: srv, wire: w, hear: hear}, nil
}

func (l *lab) close() { l.wire.close(); l.srv.stop() }

type limits struct{ plan, build time.Duration }

// build runs one paragraph the way the desk does: create, wait for the plan,
// write, wait for preview or failed, keep, then read the app the way Use does.
func (l *lab) build(ctx context.Context, p para, lim limits) row {
	r := row{ID: p.ID, Family: p.Family, Road: l.road}
	began := time.Now()
	var sum bridge.AppSummary
	if err := l.wire.call(ctx, "", bridge.MethodAppsCreate, bridge.CreateAppParams{Paragraph: p.Paragraph}, &sum); err != nil {
		r.Error = err.Error()
		return r
	}
	for _, topic := range []string{bridge.TopicBuild(sum.ID), bridge.TopicDraft(sum.ID), bridge.TopicState(sum.ID)} {
		if err := l.wire.subscribe(ctx, topic); err != nil {
			r.Error = err.Error()
			return r
		}
	}
	r.Stages.Plan = l.waitPlan(ctx, sum.ID, lim.plan)
	var ended time.Time
	if r.Stages.Plan {
		if err := l.startWrite(ctx, sum.ID, lim.plan); err != nil {
			r.State, r.Error = "write-refused", err.Error()
		} else {
			r.State = l.waitState(ctx, sum.ID, lim.build)
		}
	} else {
		r.State = "plan-failed"
	}
	ended = time.Now()
	time.Sleep(300 * time.Millisecond) // let the last events reach the collector
	l.finish(ctx, &r, sum.ID, began, ended)
	return r
}

// startWrite presses "Write it". The plan file lands before the plan task
// ends, and the Builder refuses a write while a step runs, so a refusal is
// retried until the plan card would be live.
func (l *lab) startWrite(ctx context.Context, app string, limit time.Duration) error {
	var last error
	ok := poll(ctx, limit, func() (bool, bool) {
		last = l.wire.call(ctx, app, bridge.MethodBuilderWrite, bridge.Empty{}, nil)
		return last == nil, last == nil
	})
	if ok {
		return nil
	}
	return last
}

func (l *lab) get(ctx context.Context, app string) (bridge.AppDetail, error) {
	var d bridge.AppDetail
	err := l.wire.call(ctx, app, bridge.MethodAppsGet, bridge.Empty{}, &d)
	return d, err
}

// waitPlan waits until plan.json holds screens, or a build line says failed.
func (l *lab) waitPlan(ctx context.Context, app string, limit time.Duration) bool {
	return poll(ctx, limit, func() (bool, bool) {
		if d, err := l.get(ctx, app); err == nil && planned(d.Files[spec.PlanFile]) {
			return true, true
		}
		return false, l.hear.take(app).lastFailure() != ""
	})
}

func planned(raw string) bool {
	var p spec.Plan
	return raw != "" && json.Unmarshal([]byte(raw), &p) == nil && len(p.Screens) > 0
}

func (l *lab) waitState(ctx context.Context, app string, limit time.Duration) string {
	state := "timeout"
	poll(ctx, limit, func() (bool, bool) {
		d, err := l.get(ctx, app)
		if err == nil && (d.State == bridge.StatePreview || d.State == bridge.StateFailed) {
			state = string(d.State)
			return true, true
		}
		return false, false
	})
	return state
}

// poll calls step every two seconds until it says done (ok, stop) or time ends.
func poll(ctx context.Context, limit time.Duration, step func() (ok, stop bool)) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if ok, stop := step(); stop {
			return ok
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

func (l *lab) finish(ctx context.Context, r *row, app string, began, ended time.Time) {
	t := l.hear.take(app)
	dumpTrace(l.out, l.road, r.ID, t)
	d, _ := l.get(ctx, app)
	l.keepFiles(r.ID, d.Files)
	r.Useful = "pending"
	v := judge(t.lastCheck(), d.Files, r.State)
	lines := t.lines()
	st := t.stages()
	st.Plan = r.Stages.Plan || st.Plan
	r.Stages = st
	r.Stages.Preview = r.State == string(bridge.StatePreview)
	root := l.srv.home + "/apps"
	r.RoadUsed = roadUsed(root, app)
	r.CheckPass, r.OpensInUse, r.RunsDryOK = v.pass, v.opens, v.runsDry
	r.RawErrors, r.RawSamples = countRaw(lines), rawSamples(lines)
	r.FirstLineS, r.TotalS = seconds(began, t.firstLine()), seconds(began, ended)
	r.CostUSD, r.Steps = t.cost(), journalSteps(root, app)
	if r.State != string(bridge.StatePreview) {
		if r.Error == "" {
			r.Error = t.lastFailure()
		}
		r.Cause = causeOf(r.Error)
		return
	}
	r.Stages.Keep, r.Stages.Use = l.keepAndUse(ctx, app)
	l.judgeBlind(r)
}

// keepAndUse keeps the app the way the desk does, then reads it the way Use
// does: apps.get, then a records query for every record type.
func (l *lab) keepAndUse(ctx context.Context, app string) (kept, used bool) {
	if l.wire.call(ctx, app, bridge.MethodAppsKeep, bridge.Empty{}, nil) != nil {
		return false, false
	}
	d, err := l.get(ctx, app)
	if err != nil || d.State != bridge.StateKept {
		return false, false
	}
	var a spec.App
	if json.Unmarshal([]byte(d.Files[spec.AppFile]), &a) != nil {
		return true, false
	}
	for typ := range a.Records {
		var res bridge.QueryResult
		if l.wire.call(ctx, app, bridge.MethodRecordsQuery, bridge.QueryParams{Type: typ}, &res) != nil {
			return true, false
		}
	}
	return true, true
}

func rawSamples(lines []string) []string {
	var out []string
	for _, l := range lines {
		if looksRaw(l) && len(out) < 3 {
			out = append(out, l)
		}
	}
	return out
}

// runAll builds every paragraph on every lab; the labs run side by side and
// each runs at most parallel builds at once. Rows are ordered paragraph-major.
func runAll(ctx context.Context, labs []*lab, ps []para, parallel int, lim limits, done func(row)) []row {
	rows := make([]row, len(ps)*len(labs))
	var wg sync.WaitGroup
	for li, l := range labs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem := make(chan struct{}, max(parallel, 1))
			var inner sync.WaitGroup
			for pi, p := range ps {
				inner.Add(1)
				sem <- struct{}{}
				go func() {
					defer inner.Done()
					defer func() { <-sem }()
					i := pi*len(labs) + li
					rows[i] = l.build(ctx, p, lim)
					done(rows[i])
				}()
			}
			inner.Wait()
		}()
	}
	wg.Wait()
	return rows
}

// appDir is where one app's final definition files are kept.
func (l *lab) appDir(app string) string { return filepath.Join(l.out, l.road, app) }

func (l *lab) keepFiles(app string, files map[string]string) {
	for name, text := range files {
		path := filepath.Join(l.appDir(app), name)
		if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
			_ = os.WriteFile(path, []byte(text), 0o644)
		}
	}
}

// judgeBlind asks the usefulness judge about the files alone; it is never told
// the road. Without a judge command the verdict stays "pending".
func (l *lab) judgeBlind(r *row) {
	if l.judge == "" {
		return
	}
	r.Useful, r.UsefulWhy = runJudge(l.judge, r.ID, l.appDir(r.ID))
}
