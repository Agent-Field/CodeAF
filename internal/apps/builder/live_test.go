//go:build live

package builder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// The four paragraphs the owner is most likely to type first.
var liveApps = []struct{ name, paragraph string }{
	{"crm", "A CRM for our sales pipeline: companies, deals and contacts. The agent drafts follow-ups when a deal goes quiet for a week; I approve every email before it sends."},
	{"tracker", "An issue and task tracker for our small team: tasks with a status, priority, assignee and due date, grouped into projects. I want to see what is overdue and what each person is working on."},
	{"inventory", "Inventory for my shop: products with SKU, quantity on hand, reorder level, supplier and price, plus stock movements in and out. Show me what is low on stock and the total value of stock."},
	{"booking", "Appointments for my physio clinic: clients, practitioners, services with a duration and price, and bookings by date and time with a status. I want to see today's schedule and mark bookings as done or cancelled."},
}

// TestLiveSmoke builds each app against the real model: go test -tags live
// -run TestLiveSmoke -v ./internal/apps/builder/. LIVE_ONLY picks one by name.
func TestLiveSmoke(t *testing.T) {
	for _, tc := range liveApps {
		if only := os.Getenv("LIVE_ONLY"); only != "" && only != tc.name {
			continue
		}
		t.Run(tc.name, func(t *testing.T) { liveBuild(t, tc.name, tc.paragraph) })
	}
}

func liveBuild(t *testing.T, name, paragraph string) {
	r := newRig(t)
	r.checks = nil
	all := &svc.Services{Store: r.store, Hub: r.hub, Check: standInCheck{}, Sample: fakeSample{}, Records: r.seeds}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	all.Model = NewModel(svc.Config{}, all)
	live := New(svc.Config{Ctx: ctx}, all)

	began := time.Now()
	if _, err := live.Create(ctx, svc.Caller{Member: svc.Owner}, bridge.CreateAppParams{Paragraph: paragraph}); err != nil {
		t.Fatal(err)
	}
	waitLive(t, 90*time.Second, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })
	planAt := time.Since(began)
	if err := live.Write(ctx, r.scope()); err != nil {
		t.Fatal(err)
	}
	writeBegan := time.Now()
	waitLive(t, 10*time.Minute, "preview or failure", func() bool {
		s, _ := r.store.current()
		return s == bridge.StatePreview || s == bridge.StateFailed
	})
	state, files := r.store.current()
	last := r.hub.events()[len(r.hub.events())-1]
	dir := filepath.Join("testdata", "live-"+name)
	saveLive(t, dir, files)
	t.Logf("%s: state=%s plan=%.1fs write+check=%.1fs total=%.1fs cost=$%.4f files=%d model=%s",
		name, state, planAt.Seconds(), time.Since(writeBegan).Seconds(), time.Since(began).Seconds(), last.Cost.Spent, len(files), live.model)
	t.Log(strings.Join(eventLog(r.hub.events()), "\n"))
	if state != bridge.StatePreview {
		t.Fatalf("did not pass Check: %+v", last.Check)
	}
}

func eventLog(events []bridge.BuildEvent) []string {
	var lines []string
	for _, ev := range events {
		line := fmt.Sprintf("  %s %s r%d %s %v", ev.Step, ev.Status, ev.Round, ev.Log, ev.Files)
		if ev.Check != nil {
			for _, i := range ev.Check.Issues {
				line += "\n      " + i.String()
			}
		}
		lines = append(lines, line)
	}
	return lines
}

func saveLive(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	_ = os.RemoveAll(dir)
	for path, data := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func waitLive(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
