package syncsetup

import (
	"bytes"
	"encoding/json"
	"errors"
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
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
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
// F1 supplies the same live pairing gate as production while proving continuation cursor repair.
func TestContinuingAChatKeepsItsLeaseDespiteDamagedTeamsAndLaterHeals(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	h := newTwoHomes(t)
	enableSyncTeamSharing(t, h.a.Home)
	enableSyncTeamSharing(t, h.b.Home)
	// Land in the same home cell tree that the next profile round discovers.
	h.rootsB = cell.Dir(h.b.Home)
	ctx := t.Context()
	// The receiver will open the actual session after the take. Start with a
	// journal written by that public entry point, not the relay rig's header.
	if err := os.Remove(transcriptOf(h.cell)); err != nil {
		t.Fatal(err)
	}
	source, err := session.New(session.Config{TeamsPaired: func() bool { return MayTalk(h.a.Home) }, Workspace: h.work, Place: session.Place{Dir: h.cell.Root, Workspace: h.work}, SessionFile: transcriptOf(h.cell), ProfileDir: h.a.Home, Model: "stub/chat", APIKey: "fixture", BaseURL: "http://127.0.0.1:1", System: "Continuation fixture"})
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
	unpaired, err := session.New(session.Config{TeamsPaired: func() bool { return MayTalk(h.a.Home) }, Workspace: h.work, Place: session.Place{Dir: h.cell.Root, Workspace: h.work}, SessionFile: transcriptOf(h.cell), ProfileDir: h.a.Home, Model: "stub/chat", APIKey: "fixture", BaseURL: model.URL, System: "Continuation fixture"})
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
	receiver, err := session.New(session.Config{TeamsPaired: func() bool { return MayTalk(h.b.Home) }, Workspace: work, Place: session.Place{Dir: took.Taken.Cell.Root, Workspace: work}, SessionFile: m.File, ProfileDir: h.b.Home, Model: "stub/chat", APIKey: "fixture", BaseURL: model.URL, System: "Continuation fixture"})
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

// F1 preserves the existing sharing fixture and history contract.
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
	got, _ := teams.ReadHistory(home, "alpha", 0, true)
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
	got, _ = teams.ReadHistory(home, "alpha", 0, true)
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

func enableSyncTeamSharing(t *testing.T, profile string) {
	t.Helper()
	row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Row(config.KeyTeamsShare)
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
}

// C9 pins off explicitly so continuation still preserves legacy team files and cursors.
// F1 preserves the existing sharing fixture and history contract.
func TestSharingOffContinuesTeamMemberWithoutRebindingOrChangingLegacyFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	h := newTwoHomes(t)
	key := teams.ConversationIdentity(transcriptOf(h.cell))
	for _, s := range []*Sync{h.a, h.b} {
		sharing, _ := config.NewSettings(config.SettingsOptions{ProfileDir: s.Home}).Row(config.KeyTeamsShare)
		if err := sharing.Apply("off"); err != nil {
			t.Fatal(err)
		}
		if config.TeamsShareAt(s.Home) {
			t.Fatal("paired home ignored the explicit off choice")
		}
		if err := teams.Update(s.Home, func(f *teams.File) error {
			f.Teams = []teams.Team{{ID: "alpha", Name: "local", Members: []teams.Member{{Key: key, File: transcriptOf(h.cell), Word: "worker"}}}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := teams.AppendTraffic(s.Home, "alpha", teams.Entry{Kind: teams.KindNote, From: teams.FromYou, To: teams.ToRoom, Text: "local history"}); err != nil {
			t.Fatal(err)
		}
	}
	before := map[string]string{}
	for _, s := range []*Sync{h.a, h.b} {
		for _, path := range []string{teams.Path(s.Home), teams.TrafficPath(s.Home, "alpha")} {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			before[path] = string(raw)
		}
	}
	cursor := []byte(`{"read":{"alpha":"000000000007"}}`)
	if err := os.MkdirAll(filepath.Join(h.cell.Root, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.cell.Root, ".cell", "team.json"), cursor, 0600); err != nil {
		t.Fatal(err)
	}
	a := h.openA()
	a.mustSay("publish with legacy team positions")
	h.durable(h.cell.ID, h.cell)
	for range 3 {
		for _, s := range []*Sync{h.a, h.b} {
			if err := s.SyncState(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
	}
	h.lapse()
	took, err := h.continuerB().Take(t.Context(), h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	carried, err := os.ReadFile(filepath.Join(took.Taken.Cell.Root, ".cell", "team.json"))
	if err != nil || string(carried) != string(cursor) {
		t.Fatalf("continuation changed legacy cursor: %s %v", carried, err)
	}
	for path, want := range before {
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != want {
			t.Fatal("sharing-off continuation changed file", path, err)
		}
	}
	for _, s := range []*Sync{h.a, h.b} {
		vault, err := keys.Open(s.Home)
		if err != nil {
			t.Fatal(err)
		}
		for _, scope := range []string{"home:keys:teams", "home:windows:team-traffic"} {
			entries, err := vault.Entries(scope)
			if err != nil || len(entries) != 0 {
				t.Fatal("sharing-off continuation carried teams", entries, err)
			}
		}
		for _, path := range []string{filepath.Join(s.Home, "teams-sync.ledger"), teams.TrafficMirrorPath(s.Home, "alpha")} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("sharing-off continuation created sharing file", path, err)
			}
		}
	}
}

// F2 backs up the exact local file before the first carrier round and never rewrites that copy.
func TestFirstTeamSharingRoundBacksUpLocalTeamsOnce(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	for i, m := range []*keyMachine{a, b} {
		if err := teams.Update(m.profile, func(f *teams.File) error {
			f.Teams = []teams.Team{{ID: fmt.Sprintf("team-%d", i), Name: "harbor"}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		m.Carry = []vaultsync.Carrier{syncedTeams(m.profile, m.profile, fmt.Sprintf("device-%d", i), nil)}
	}
	before, _ := os.ReadFile(teams.Path(b.profile))
	a.push(t)
	b.pull(t)
	backup := filepath.Join(b.profile, "teams.before-sharing.json")
	got, err := os.ReadFile(backup)
	if err != nil || string(got) != string(before) {
		t.Fatalf("first merge backup: %s %v", got, err)
	}
	info, _ := os.Stat(backup)
	f, err := teams.Load(b.profile)
	if err != nil || len(f.Teams) != 2 {
		t.Fatal("merge did not apply after backup", f, err)
	}
	if err := teams.Update(b.profile, func(f *teams.File) error { f.Teams[0].Name = "edited"; return nil }); err != nil {
		t.Fatal(err)
	}
	b.push(t)
	a.pull(t)
	b.pull(t)
	after, _ := os.ReadFile(backup)
	next, _ := os.Stat(backup)
	if string(after) != string(before) || !next.ModTime().Equal(info.ModTime()) {
		t.Fatal("second round rewrote original backup")
	}
	// A preexisting copy also wins if an interrupted first round left no ledger.
	if err := os.Remove(filepath.Join(b.profile, "teams-sync.ledger")); err != nil {
		t.Fatal(err)
	}
	b.pull(t)
	after, _ = os.ReadFile(backup)
	if string(after) != string(before) {
		t.Fatal("retry overwrote backup")
	}
}

// R2/F2 preserves the first former appointment through guarded membership removal and recapture.
func TestMergedMemberRemovalKeepsFormerManagerAppointment(t *testing.T) {
	for _, former := range []string{"", "local-manager", "earlier"} {
		t.Run("former="+former, func(t *testing.T) {
			dir := t.TempDir()
			previous := []teams.Member{{Key: "earlier", SyncID: "device:a:earlier", Word: "earlier manager"}}
			if err := teams.Update(dir, func(f *teams.File) error {
				f.Teams = []teams.Team{{ID: "alpha", Name: "harbor", Manager: "local-manager", FormerManager: former, FormerMembers: previous, Members: []teams.Member{{Key: "local-manager", SyncID: "device:a:manager", Word: "manager"}}}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			p := teamKeys{dir, dir, "a", nil}
			before, err := p.Values()
			if err != nil {
				t.Fatal(err)
			}
			if err := p.ApplyIf(map[string]string{"alpha/member/device:a:manager": ""}, before); err != nil {
				t.Fatal(err)
			}
			f, err := teams.Snapshot(dir)
			if err != nil {
				t.Fatal(err)
			}
			got := f.Teams[0]
			want, portable := former, before["alpha/field/former_manager"]
			if want == "" {
				want, portable = "local-manager", before["alpha/field/manager"]
			}
			if got.Manager != "" || got.FormerManager != want {
				t.Fatalf("merge replaced the former appointment: %+v", got)
			}
			after, err := p.Values()
			if err != nil {
				t.Fatal(err)
			}
			if after["alpha/field/former_manager"] != portable {
				t.Fatal("former appointment lost its portable identity", after)
			}
			if former == "earlier" && len(got.FormerMembers) != 1 {
				t.Fatal("occupied history gained the cleared appointment", got.FormerMembers)
			}
		})
	}
}

// R2/F2 keeps the demoted root's active and former appointments through guarded apply.
func TestMergedRootDemotionKeepsFormerManagerAppointment(t *testing.T) {
	for _, former := range []string{"", "earlier"} {
		dir := t.TempDir()
		if err := teams.Update(dir, func(f *teams.File) error {
			f.Teams = []teams.Team{{ID: "b", Name: teams.RootName, Root: true, Manager: "active", FormerManager: former, FormerMembers: []teams.Member{{Key: "earlier", SyncID: "device:b:earlier"}}, Members: []teams.Member{{Key: "active", SyncID: "device:b:active", Word: "manager"}}}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		p := teamKeys{dir, dir, "b", nil}
		before, err := p.Values()
		if err != nil {
			t.Fatal(err)
		}
		if err := p.ApplyIf(map[string]string{"a/exists": "true", "a/field/name": teamValue(teams.RootName), "a/field/root": "true"}, before); err != nil {
			t.Fatal(err)
		}
		f, err := teams.Snapshot(dir)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := f.Team("b")
		if got.Root || got.Manager != "active" || got.FormerManager != former {
			t.Fatalf("demotion changed appointments: %+v", got)
		}
	}
}

// R5/F2 refuses incoming changes until backup succeeds and reports retries on the sync notice path once.
func TestTeamBackupFailureWaitsAndNotifiesOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_HOME", dir)
	if err := teams.Update(dir, func(f *teams.File) error { f.Teams = []teams.Team{{ID: "local", Name: "harbor"}}; return nil }); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(teams.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "teams.before-sharing.json")
	// A symlink loop deterministically refuses even privileged processes.
	if err := os.Symlink(filepath.Base(teams.Path(dir)), filepath.Join(dir, "original-teams")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(teams.Path(dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("original-teams", teams.Path(dir)); err != nil {
		t.Fatal(err)
	}
	p := teamKeys{dir, dir, "local", nil}
	teams.SharingError(dir, errors.New("an earlier independent sharing failure"))
	var notices []string
	stop := teams.WatchSharingErrors("", func(line string) { notices = append(notices, line) })
	defer stop()
	for i := 0; i < 2; i++ {
		err := p.ApplyIf(map[string]string{"incoming/exists": "true"}, nil)
		if err == nil {
			t.Fatal("incoming team applied without a backup")
		}
		teams.SharingError(dir, err)
	}
	if len(notices) != 2 || !strings.Contains(notices[1], "teams.before-sharing.json") {
		t.Fatalf("backup notice was lost or repeated: %v", notices)
	}
	if _, err := os.Lstat(backup); !os.IsNotExist(err) {
		t.Fatal("failed backup left a copy", err)
	}
	if err := os.Remove(teams.Path(dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(teams.Path(dir), before, 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.ApplyIf(map[string]string{"incoming/exists": "true"}, nil); err != nil {
		t.Fatal(err)
	}
	f, err := teams.Snapshot(dir)
	if err != nil || len(f.Teams) != 2 {
		t.Fatal("retry did not apply after backup recovered", f, err)
	}
	got, err := os.ReadFile(backup)
	if err != nil || !bytes.Equal(got, before) {
		t.Fatal("recovery backup lost local teams", string(got), err)
	}
}
