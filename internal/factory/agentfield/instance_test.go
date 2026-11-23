package agentfield

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
)

// respawn is the engine dying and the desk starting it again: the old node
// is gone, and a NEW Node — nothing in memory, only the floor and the kept
// connection — starts over the same home.
func (w *world) respawn() *Node {
	w.t.Helper()
	w.node.Stop()
	before := len(w.plane.registrations())
	again := &Node{Conn: w.node.Conn, Store: w.floor, Version: NodeVersion, Build: "test", Clock: w.node.Clock}
	if err := again.Start(context.Background()); err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { again.Stop(); again.Forget() })
	eventually(w.t, "the respawned engine's registration", func() bool { return len(w.plane.registrations()) > before })
	w.node = again
	return again
}

func (p *fakePlane) leasedInflight(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, l := range p.leases {
		if list, ok := l["inflight"].([]any); ok && slices.Contains(list, any(id)) {
			return true
		}
	}
	return false
}

// THE OWNER'S RULE: the engine can go up or down and AgentField never knows.
// An execution open when the engine died is not orphaned by the respawn: the
// new process registers the same instance, lists the execution as inflight
// at once, and the job's pull request still completes it with the result.
func TestAnEngineRestartOrphansNothingAndTheResultStillArrives(t *testing.T) {
	w := newWorld(t, factory.TierOwner)
	entry := map[string]any{"title": "AFNODE: add Len", "body": "In ledger.go add Len.", "repo": "santoshkumarradha/factory-demo"}
	code, out := w.post("/reasoners/intake", map[string]string{"X-Execution-ID": "exec-r"}, dispatch("generic_bearer", entry, "idem-r"))
	if code != http.StatusAccepted {
		t.Fatalf("intake: %d %v", code, out)
	}
	job := int(out["job"].(float64))
	eventually(t, "a running report", func() bool { return len(w.plane.reportsFor("exec-r")) > 0 })
	first := w.plane.registrations()[0].InstanceID

	w.respawn()
	regs := w.plane.registrations()
	if again := regs[len(regs)-1].InstanceID; again != first {
		t.Fatalf("the respawned engine registered instance %q, the one before it %q", again, first)
	}
	w.plane.mu.Lock()
	orphaned := len(w.plane.orphaned)
	w.plane.mu.Unlock()
	if orphaned != 0 {
		t.Fatalf("the respawn orphaned %d executions", orphaned)
	}
	eventually(t, "the respawned engine's lease listing the open execution", func() bool { return w.plane.leasedInflight("exec-r") })

	const pr = "https://github.com/santoshkumarradha/factory-demo/pull/12"
	if err := w.floor.Update(job, func(it *factory.Item) error {
		it.PullRequest = &factory.PullRequest{URL: pr, Number: 12, State: factory.PROpen}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	inbox.Notify(context.Background(), w.floor, job, inbox.Opened(pr))
	eventually(t, "the completion after the respawn", func() bool {
		r := w.plane.reportsFor("exec-r")
		return len(r) > 0 && r[len(r)-1].Status == ExecSucceeded
	})
	rs := w.plane.reportsFor("exec-r")
	if done := rs[len(rs)-1]; done.Result["pr_url"] != pr || int(done.Result["job"].(float64)) != job {
		t.Fatalf("completion %+v", done)
	}
}

// THE FIRST START AFTER THE UPGRADE: a build from before the kept instance
// registered a fresh one per process, so a floor with open executions and no
// kept instance adopts the one the control plane holds them under, rather
// than orphaning them on the way to keeping one.
func TestTheFirstStartAfterTheUpgradeAdoptsTheInstanceItRanUnder(t *testing.T) {
	w := newWorld(t, factory.TierOwner)
	code, out := w.post("/reasoners/intake", map[string]string{"X-Execution-ID": "exec-u"}, dispatch("generic_bearer", map[string]any{"title": "Fix it"}, "idem-u"))
	if code != http.StatusAccepted {
		t.Fatalf("intake: %d %v", code, out)
	}
	eventually(t, "a running report", func() bool { return len(w.plane.reportsFor("exec-u")) > 0 })
	// The old build: an instance of its own process, never kept.
	const old = "santoshs-macbook-pro-18b6afc77c733f2c"
	w.plane.mu.Lock()
	w.plane.instance = old
	w.plane.mu.Unlock()
	if err := os.Remove(filepath.Join(w.floor.Root(), instanceFile)); err != nil {
		t.Fatal(err)
	}

	w.respawn()
	regs := w.plane.registrations()
	if got := regs[len(regs)-1].InstanceID; got != old {
		t.Fatalf("the first start after the upgrade registered %q, want the held %q", got, old)
	}
	w.plane.mu.Lock()
	orphaned := len(w.plane.orphaned)
	w.plane.mu.Unlock()
	if orphaned != 0 {
		t.Fatalf("the upgrade orphaned %d executions", orphaned)
	}
	var kept instanceDoc
	if err := readJSON(filepath.Join(w.floor.Root(), instanceFile), &kept); err != nil || kept.InstanceID != old {
		t.Fatalf("kept instance %+v %v", kept, err)
	}
}

// The fake plane reaps as the real one does, so the two tests above prove
// something: a registration under another instance fails the open execution,
// and the node closes it on the refusal rather than retelling it forever.
func TestAChangedInstanceIsOrphanedByThePlane(t *testing.T) {
	w := newWorld(t, factory.TierOwner)
	code, out := w.post("/reasoners/intake", map[string]string{"X-Execution-ID": "exec-o"}, dispatch("generic_bearer", map[string]any{"title": "Fix it"}, "idem-o"))
	if code != http.StatusAccepted {
		t.Fatalf("intake: %d %v", code, out)
	}
	job := int(out["job"].(float64))
	eventually(t, "a running report", func() bool { return len(w.plane.reportsFor("exec-o")) > 0 })
	w.node.mu.Lock()
	r := w.node.running
	w.node.mu.Unlock()
	if err := r.client.Register(context.Background(), RegistrationFor(r.settings, "another-process", NodeVersion, "test")); err != nil {
		t.Fatal(err)
	}
	w.plane.mu.Lock()
	reaped := w.plane.orphaned["exec-o"]
	w.plane.mu.Unlock()
	if !reaped {
		t.Fatalf("the fake plane did not reap a changed instance")
	}
	_ = w.floor.Update(job, func(it *factory.Item) error { it.State = factory.StateDismissed; return nil })
	w.node.catchUp(context.Background(), r)
	if len(w.node.Open()) != 0 {
		t.Fatalf("an orphaned execution is still open here")
	}
}

// THE REGISTRATION SAYS THE FACTORY HOLDS ITS EXECUTIONS WHILE OFFLINE, under
// "custom", the one metadata key the control plane keeps besides deployment
// and performance; the factory's own metadata rides inside it.
func TestTheRegistrationHoldsExecutionsOfflineUnderCustom(t *testing.T) {
	w := newWorld(t, factory.TierOwner)
	reg := w.plane.registrations()[0]
	if len(reg.Metadata) != 1 {
		t.Fatalf("metadata keys %v, want only custom", reg.Metadata)
	}
	custom, _ := reg.Metadata["custom"].(map[string]any)
	if custom["holds_executions_offline"] != true {
		t.Fatalf("custom %v does not hold executions offline", custom)
	}
	own, _ := custom["codeaf"].(map[string]any)
	if own["kind"] != "factory" || own["build"] != "test" || own["trust_ceiling"] != string(factory.TierOwner) {
		t.Fatalf("custom.codeaf %v", own)
	}
}
