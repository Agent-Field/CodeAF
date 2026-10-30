package syncsetup

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestTeamFieldsAndUnknownMemberFieldsSurviveGuardedApply(t *testing.T) {
	home := t.TempDir()
	path := teams.Path(home)
	raw := `{"version":2,"teams":[{"id":"alpha","name":"alpha","parent":"","members":[{"key":"local","file":"","where":"","word":"storage","future_member":{"value":7}}],"manager":"","former_manager":"historical-leader","made":"2026-01-01T00:00:00Z","future_team":{"enabled":true}}]}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	p := teamKeys{home, home, "device-a", nil}
	expected, err := p.Values()
	if err != nil {
		t.Fatal(err)
	}
	// A person types while the other computer's response is in flight.
	if err := teams.Update(home, func(f *teams.File) error { f.Teams[0].Name = "typed during request"; return nil }); err != nil {
		t.Fatal(err)
	}
	changes := map[string]string{"alpha/field/name": `"remote name"`, "alpha/field/parent": `"missing-parent"`, "alpha/field/future_flag": `{"nested":[1,2]}`}
	if err := p.ApplyIf(changes, expected); err != nil {
		t.Fatal(err)
	}
	f, err := teams.Load(home)
	if err != nil || f.Teams[0].Name != "typed during request" || f.Teams[0].Parent != "" {
		t.Fatalf("guard: %+v %v", f, err)
	}
	saved, _ := os.ReadFile(path)
	for _, field := range []string{"future_member", "future_team", "future_flag"} {
		if !strings.Contains(string(saved), field) {
			t.Fatalf("lost %s", field)
		}
	}
	values, err := p.Values()
	if err != nil {
		t.Fatal(err)
	}
	if values["alpha/field/former_manager"] == "" || values["alpha/field/former_manager"] == `""` {
		t.Fatal("history without a former membership lost its manager identity")
	}
	if values["alpha/field/future_team"] != "{\"enabled\":true}" {
		t.Fatalf("future field not carried independently: %s", values["alpha/field/future_team"])
	}
}

func TestTeamsUseReceivingComputersOwnTranscriptAndBranchIdentity(t *testing.T) {
	home := t.TempDir()
	p := teamKeys{home, home, "device-a", nil}
	f := &teams.File{Teams: []teams.Team{{ID: "alpha", Name: "alpha", Members: []teams.Member{{Key: "local-key", File: "/local/transcript", Where: "/local/work", Word: "storage", SyncID: "cell:shared-cell"}}}}}
	m, err := p.identities(f)
	if err != nil {
		t.Fatal(err)
	}
	values, err := p.values(f, m)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if strings.Contains(value, "/local/") {
			t.Fatal("machine paths reached the wire")
		}
	}
	decoded, err := p.decode(values, m)
	if err != nil {
		t.Fatal(err)
	}
	member := decoded[0].Members[0]
	if member.Key != "local-key" || member.File != "/local/transcript" || member.Where != "/local/work" {
		t.Fatalf("lost receiver's path: %+v", member)
	}
}

func TestContinuedMemberRebindsToLocalChatWithoutChangingPortableMembership(t *testing.T) {
	home := t.TempDir()
	c, err := cell.Create(home, cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	p := teamKeys{home, home, "this-device", nil}
	remote := teams.RemoteKey(c.ID)
	if err := teams.Update(home, func(f *teams.File) error {
		f.Teams = []teams.Team{{ID: "alpha", Name: "alpha", Members: []teams.Member{{Key: remote, Word: "storage", SyncID: "cell:" + c.ID}}, Manager: remote}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := p.Values()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.rebindChat(c.ID, c); err != nil {
		t.Fatal(err)
	}
	after, err := p.Values()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("taking a chat changed portable membership")
	}
	f, err := teams.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	m := f.Teams[0].Members[0]
	if m.File != filepath.Join(c.Root, cell.TranscriptPath) || m.Key != m.File || f.Teams[0].Manager != m.Key {
		t.Fatalf("continued member did not rebind: %+v", f.Teams[0])
	}
}

func TestMembershipStartBoundsNeverReuseAForeignSequenceNumber(t *testing.T) {
	left := &teamIdentities{device: "device-a", local: map[string]teams.Member{}}
	right := &teamIdentities{device: "device-b", local: map[string]teams.Member{}}
	member := teams.Member{Key: teams.RemoteKey("shared"), Word: "storage", StartID: "000000000001"}
	portable := left.portable(member)
	if portable.StartID != "device-a/000000000001" {
		t.Fatal("start boundary lost its origin")
	}
	received := right.realized(portable)
	if received.StartID != "" || received.StartOrigin != portable.StartID {
		t.Fatal("foreign start reused a local delivery id")
	}
	returned := right.portable(received)
	if returned.StartID != portable.StartID || left.realized(returned).StartID != member.StartID {
		t.Fatal("start boundary did not survive round trip")
	}
}

// Capture cannot inspect unrelated sessions, including legacy rows whose
// directory id is not a transferable cell id.
func TestUnchangedTeamCaptureScansNoWorldAndUsesDeterministicIdentities(t *testing.T) {
	home := t.TempDir()
	for i := 0; i < 500; i++ {
		root := filepath.Join(home, "v3", "projects", "project", fmt.Sprintf("%016x", i+1))
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := session.SaveMeta(root, session.Meta{ID: filepath.Base(root), Title: "legacy chat", Workspace: home, Created: time.Unix(int64(i+1), 0)}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "transcript.jsonl"), []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := teams.Update(home, func(f *teams.File) error {
		for i := 0; i < 5; i++ {
			key := filepath.Join(home, "v3", "projects", "project", fmt.Sprintf("%016x", i+1), "transcript.jsonl")
			f.Teams = append(f.Teams, teams.Team{ID: fmt.Sprintf("team-%d", i), Name: fmt.Sprintf("team-%d", i), Members: []teams.Member{{Key: key, File: key, Word: "worker"}}})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := teams.AppendTraffic(home, fmt.Sprintf("team-%d", i), teams.Entry{Kind: teams.KindNote, From: teams.FromYou, To: teams.ToRoom, Text: "local activity"}); err != nil {
			t.Fatal(err)
		}
	}
	cache := &teamIdentityCache{}
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	p := teamKeys{home, home, dev.ID(), cache}
	first, err := p.Values()
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Values()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) || cache.scans != 0 || cache.opens != 0 {
		t.Fatalf("unstable capture or %d world scans", cache.scans)
	}
	for _, raw := range second {
		if strings.Contains(raw, "cell:legacy") {
			t.Fatal("legacy row became a cell")
		}
	}
	celltest.Available(t)
	// One real cell can be in several teams while their stored portable ids
	// are at different migration stages. An unchanged capture opens it zero
	// times after warming, rather than alternating one file-cache entry.
	c, err := cell.Create(home, cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(c.Root, cell.TranscriptPath)
	if err := teams.Update(home, func(f *teams.File) error {
		for i := 0; i < 2; i++ {
			member := teams.Member{Key: file, File: file, Word: "shared worker"}
			if i == 1 {
				member.SyncID = "device:" + dev.ID() + ":prior-local-id"
			}
			f.Teams[i].Members = append(f.Teams[i].Members, member)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	warm, err := p.Values()
	if err != nil {
		t.Fatal(err)
	}
	opened := cache.opens
	again, err := p.Values()
	if err != nil || !reflect.DeepEqual(warm, again) || cache.opens != opened {
		t.Fatalf("unchanged shared cell reopened metadata: %d -> %d, %v", opened, cache.opens, err)
	}
	t.Setenv(URLVar, relay(t).URL)
	t.Setenv(IntervalVar, "")
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	syncer, ok, err := Open(home)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	syncer.teamsCache = cache
	if err := syncer.SyncState(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(home, "vault.enc"))
	scans, opens, reads := cache.scans, cache.opens, cache.trafficReads
	if err := syncer.SyncState(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(home, "vault.enc"))
	if cache.scans != scans || cache.opens != opens || cache.trafficReads != reads || string(before) != string(after) {
		t.Fatal("unchanged round performed discovery or wrote vault")
	}

}

// A later restore resolves the actual local cell after immediate rebinding
// was unavailable. The vault need not contain another team edit to retry it.
func TestTeamRestoreHealsMembershipAfterDamagedFileDelayedRebinding(t *testing.T) {
	home := t.TempDir()
	cache := &teamIdentityCache{}
	p := teamKeys{home, home, "device-b", cache}
	c, err := cell.Create(home, cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	remote := teams.RemoteKey(c.ID)
	if err := teams.Update(home, func(f *teams.File) error {
		f.Teams = []teams.Team{{ID: "alpha", Name: "alpha", Manager: remote, Members: []teams.Member{{Key: remote, SyncID: "cell:" + c.ID, Word: "worker"}}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	good, _ := os.ReadFile(teams.Path(home))
	if err := os.WriteFile(teams.Path(home), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.rebindChat(c.ID, c); err == nil {
		t.Fatal("fixture did not fail rebind")
	}
	if err := os.WriteFile(teams.Path(home), good, 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	f, err := teams.Snapshot(home)
	if err != nil {
		t.Fatal(err)
	}
	m := f.Teams[0].Members[0]
	if m.File != filepath.Join(c.Root, cell.TranscriptPath) || f.Teams[0].Manager != m.Key {
		t.Fatal("later restore did not heal", m)
	}
}

func TestMergedTeamsFollowCreationTimeWhileUnpairedFileKeepsItsOrder(t *testing.T) {
	home := t.TempDir()
	early := time.Unix(10, 0)
	if err := teams.Update(home, func(f *teams.File) error {
		f.Teams = []teams.Team{{ID: "aa", Name: "second", Made: early.Add(time.Second)}, {ID: "zz", Name: "first", Made: early}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(teams.Path(home))
	p := teamKeys{home, home, "dev", nil}
	values, err := p.Values()
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(teams.Path(home))
	if string(before) != string(after) {
		t.Fatal("capture reordered local file")
	}
	if err := p.Apply(values); err != nil {
		t.Fatal(err)
	}
	f, _ := teams.Snapshot(home)
	if f.Teams[0].ID != "zz" || f.Teams[1].ID != "aa" {
		t.Fatal("merge sorted random ids", f.Teams)
	}
}

// When the real cell engine is available, this drives the continuation door
// that owns the lease, not a separate call to the bookkeeping helper.
func TestContinuingAChatKeepsItsLeaseDespiteDamagedTeamsAndLaterHeals(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	h := newTwoHomes(t)
	// Land in the same home cell tree that the next profile round discovers.
	h.rootsB = cell.Dir(h.b.Home)
	ctx := t.Context()
	// The receiver will open the actual session after the take. Start with a
	// journal written by that public entry point, not the relay rig's header.
	if err := os.Remove(transcriptOf(h.cell)); err != nil {
		t.Fatal(err)
	}
	source, err := session.New(session.Config{Workspace: h.work, Place: session.Place{Dir: h.cell.Root, Workspace: h.work}, SessionFile: transcriptOf(h.cell), ProfileDir: h.a.Home, Model: "stub/chat", APIKey: "fixture", BaseURL: "http://127.0.0.1:1", System: "Continuation fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	source.SettleWrites()
	remote := teams.ConversationIdentity(transcriptOf(h.cell))
	if err := teams.Update(h.a.Home, func(f *teams.File) error {
		off := false
		f.Teams = []teams.Team{{ID: "alpha", Name: "alpha", Manager: remote, Settings: teams.Settings{Wake: &off}, Members: []teams.Member{{Key: remote, File: transcriptOf(h.cell), Where: h.work, SyncID: "cell:" + h.cell.ID, Word: "worker"}}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(model.Close)
	// Save through the real session boundary while A is still unpaired.
	solo := filepath.Join(h.a.Home, "identity-solo")
	if err := os.WriteFile(solo, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_HOME", h.a.Home)
	for range 5 {
		if err := teams.AppendTraffic(h.a.Home, "alpha", teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: teams.ToRoom, Text: "A history"}); err != nil {
			t.Fatal(err)
		}
	}
	unpaired, err := session.New(session.Config{Workspace: h.work, Place: session.Place{Dir: h.cell.Root, Workspace: h.work}, SessionFile: transcriptOf(h.cell), ProfileDir: h.a.Home, Model: "stub/chat", APIKey: "fixture", BaseURL: model.URL, System: "Continuation fixture"})
	if err != nil {
		t.Fatal(err)
	}
	// Opening the session establishes its initial delivery position.
	events, err := unpaired.Submit(ctx, "establish the unpaired delivery cursor")
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event.Kind == session.EventError {
			t.Fatal(event.Text)
		}
	}
	if err := unpaired.Close(); err != nil {
		t.Fatal(err)
	}
	unpaired.SettleWrites()
	cursor, err := os.ReadFile(filepath.Join(h.cell.Root, ".cell", "team.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cursor), "device") || !strings.Contains(string(cursor), "000000000005") {
		t.Fatalf("unpaired cursor: %s", cursor)
	}
	if err := identity.EndSolo(h.a.Home); err != nil {
		t.Fatal(err)
	}
	a := h.openA()
	a.mustSay("publish with team positions")
	h.durable(h.cell.ID, h.cell)
	if err := h.a.SyncState(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.b.SyncState(ctx); err != nil {
		t.Fatal(err)
	}
	good, err := os.ReadFile(teams.Path(h.b.Home))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(teams.Path(h.b.Home), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	h.lapse()
	took, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatalf("teams failed continuation: %v", err)
	}
	view, err := h.b.Dir.Cell(ctx, h.cell.ID)
	if err != nil || view.Cell.Lease.Device != h.b.Device.ID() || view.Cell.Lease.Fence != took.Taken.Driving.Fence {
		t.Fatal("continuation lost lease", view, err)
	}
	if !strings.Contains(strings.Join(h.wall.all(), "\n"), "teams could not be updated yet") {
		t.Fatal("failed rebind had no notice")
	}
	received, err := os.ReadFile(filepath.Join(took.Taken.Cell.Root, ".cell", "team.json"))
	if err != nil || string(received) != string(cursor) {
		t.Fatal("cursor fixture did not travel", string(received), err)
	}
	if err := os.WriteFile(teams.Path(h.b.Home), good, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.b.SyncState(ctx); err != nil {
		t.Fatal(err)
	}
	f, err := teams.Snapshot(h.b.Home)
	if err != nil {
		t.Fatal(err)
	}
	m := f.Teams[0].Members[0]
	if m.File != filepath.Join(took.Taken.Cell.Root, cell.TranscriptPath) {
		t.Fatalf("later round did not heal membership: %+v", m)
	}

	// Drive delivery through a real received agent and a loopback scripted
	// model. The carried cursor must skip earlier B work and admit new work.
	t.Setenv("CODEAF_HOME", h.b.Home)
	if err := teams.AppendTraffic(h.b.Home, "alpha", teams.Entry{At: time.Now().Add(-time.Second), Kind: teams.KindDirective, From: "helper", To: teams.ToManager, Text: "before B opened"}); err != nil {
		t.Fatal(err)
	}
	work := workspaceOf(took.Taken.Cell.Root)
	receiver, err := session.New(session.Config{Workspace: work, Place: session.Place{Dir: took.Taken.Cell.Root, Workspace: work}, SessionFile: m.File, ProfileDir: h.b.Home, Model: "stub/chat", APIKey: "fixture", BaseURL: model.URL, System: "Continuation fixture"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = receiver.Close() })
	submit := func() {
		events, err := receiver.Submit(ctx, "check local team messages")
		if err != nil {
			t.Fatal(err)
		}
		for event := range events {
			if event.Kind == session.EventError {
				t.Fatalf("received agent: %s", event.Text)
			}
		}
	}
	submit()
	if err := teams.AppendTraffic(h.b.Home, "alpha", teams.Entry{Kind: teams.KindDirective, From: "helper", To: teams.ToManager, Text: "after B opened"}); err != nil {
		t.Fatal(err)
	}
	submit()
	submit()
	receiver.SettleWrites()
	journal, err := os.ReadFile(m.File)
	if err != nil {
		t.Fatal(err)
	}
	var deliveries string
	for _, line := range strings.Split(string(journal), "\n") {
		var entry struct{ Content string }
		if json.Unmarshal([]byte(line), &entry) == nil && strings.Contains(entry.Content, "Team traffic in") {
			deliveries += entry.Content + "\n"
		}
	}
	if strings.Contains(deliveries, "before B opened") || strings.Count(deliveries, "after B opened") != 1 {
		t.Fatalf("continued cursor lost, replayed or admitted old work: %s", deliveries)
	}
}

func TestTrafficMirrorsRetireRevokedDevicesWithoutRepublishingThem(t *testing.T) {
	home := t.TempDir()
	if err := teams.Update(home, func(f *teams.File) error { f.Teams = []teams.Team{{ID: "alpha", Name: "alpha"}}; return nil }); err != nil {
		t.Fatal(err)
	}
	paired := true
	p := &teamTraffic{teamKeys: teamKeys{home, home, "here", nil}, peers: func() (map[string]bool, error) { return map[string]bool{"desktop": paired}, nil }}
	values := map[string]string{"alpha/desktop": teamValue([]teams.Entry{{ID: "desktop/1", At: time.Now(), Kind: teams.KindNote, Text: "elsewhere"}})}
	if err := p.Apply(values); err != nil {
		t.Fatal(err)
	}
	got, _ := teams.ReadHistory(home, "alpha", 0)
	if len(got) != 1 {
		t.Fatal(got)
	}
	local, err := p.Values()
	if err != nil || len(local) != 0 {
		t.Fatal("received lines were republished", local, err)
	}
	paired = false
	if err := p.Apply(values); err != nil {
		t.Fatal(err)
	}
	got, _ = teams.ReadHistory(home, "alpha", 0)
	if len(got) != 0 {
		t.Fatal("revoked device retained mirror", got)
	}
}

func TestLegacyChatsNeverAcquireCellIdentityFromTheDiscoveryIndex(t *testing.T) {
	home := t.TempDir()
	p := teamKeys{home, home, "writer", &teamIdentityCache{}}
	legacy := filepath.Join(home, "v3", "projects", "project", "0000000000000001", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("legacy\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(filepath.Dir(legacy), session.Meta{ID: "0000000000000001", Title: "legacy", Workspace: home, Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	c, err := cell.Create(home, cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	realFile := filepath.Join(c.Root, cell.TranscriptPath)
	f := &teams.File{Teams: []teams.Team{{ID: "alpha", Name: "alpha", Members: []teams.Member{{Key: legacy, File: legacy, Word: "legacy"}, {Key: realFile, File: realFile, Word: "real"}}}}}
	m, err := p.identities(f)
	if err != nil {
		t.Fatal(err)
	}
	p.resolve(m, []string{"cell:" + c.ID, "cell:unresolved"})
	if !strings.HasPrefix(m.reverse[legacy], "device:writer:") || m.reverse[realFile] != "cell:"+c.ID {
		t.Fatal("ambiguous reverse identities", m.reverse)
	}
	if _, ok := m.local["cell:0000000000000001"]; ok {
		t.Fatal("legacy world row entered cell index")
	}
	first, _ := p.values(f, m)
	for range 10 {
		next, err := p.identities(f)
		if err != nil {
			t.Fatal(err)
		}
		values, _ := p.values(f, next)
		if !reflect.DeepEqual(values, first) {
			t.Fatal("identities depend on map traversal")
		}
	}
}
