package pair

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/teams"
)

type relayTeamsRig struct {
	a, b     *syncsetup.Sync
	requests *atomic.Int64
}

// Pair through an actual loopback mailbox, then use the authenticated directory
// and sealed store on that same relay. Neither side shares a home or vault file.
func newRelayTeamsRig(t *testing.T) relayTeamsRig {
	t.Helper()
	celltest.Available(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("CODEAF_PROFILE_DIR", "")
	aHome, bHome := t.TempDir(), t.TempDir()
	t.Setenv("CODEAF_HOME", aHome)
	own, err := identity.Ensure(aHome)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	dirs := map[string]directory.Directory{}
	stores := map[string]blobstore.Store{}
	auth := reqsign.AuthenticateAt(time.Now)
	// Both exact and subtree paths must use one mailbox's generation and state.
	box := pairbox.NewMemory(pairbox.DefaultLimits, nil)
	boxHandler := pairbox.Handler(box, pairbox.SocketPeer)
	root := http.NewServeMux()
	root.Handle(pairbox.Path, boxHandler)
	root.Handle(pairbox.Path+"/", boxHandler)
	root.Handle("/v1/dir/", directory.Handler(auth, func(id string) (directory.Directory, error) {
		mu.Lock()
		defer mu.Unlock()
		if dirs[id] == nil {
			dirs[id] = directory.NewMemory(time.Now)
		}
		return dirs[id], nil
	}))
	root.Handle("/v1/store/", blobstore.Handler(auth, func(id string) (blobstore.Store, error) {
		mu.Lock()
		defer mu.Unlock()
		if stores[id] == nil {
			stores[id] = blobstore.NewMemory()
		}
		return stores[id], nil
	}))
	requests := new(atomic.Int64)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); root.ServeHTTP(w, r) }))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	t.Setenv(syncsetup.URLVar, server.URL)
	t.Setenv(syncsetup.IntervalVar, "")
	route := Mailbox{Box: pairbox.NewHTTP(server.URL, server.Client()), URL: server.URL, Host: "127.0.0.1"}
	ui := newScreen()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := Offer(ctx, route, Grant{Identity: own, SyncURL: server.URL}, ui); done <- err }()
	code := ui.nextCode(t)
	if _, err := Join(ctx, route, Joining{Home: bHome, Label: "desktop"}, code.Shown(), newScreen()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// Offer's CLI normally ends solo; the fixture uses the same explicit door.
	if err := identity.EndSolo(aHome); err != nil {
		t.Fatal(err)
	}
	open := func(home string) *syncsetup.Sync {
		s, ok, err := syncsetup.Open(home)
		if err != nil || !ok {
			t.Fatalf("open: %v %v", ok, err)
		}
		return s
	}
	a, b := open(aHome), open(bHome)
	for i, s := range []*syncsetup.Sync{a, b} {
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, []string{"laptop", "desktop"}[i]); err != nil {
			t.Fatal(err)
		}
	}
	return relayTeamsRig{a, b, requests}
}
func (r relayTeamsRig) round(t *testing.T) {
	t.Helper()
	for _, s := range []*syncsetup.Sync{r.a, r.b, r.a, r.b} {
		if err := s.SyncState(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}
func relayTeamEdit(t *testing.T, s *syncsetup.Sync, fn func(*teams.File)) {
	t.Helper()
	if err := teams.Update(s.Home, func(f *teams.File) error { fn(f); return nil }); err != nil {
		t.Fatal(err)
	}
}
func relayTeamRead(t *testing.T, s *syncsetup.Sync) *teams.File {
	t.Helper()
	f, err := teams.Load(s.Home)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func relayTeamCreate(t *testing.T, r relayTeamsRig) {
	t.Helper()
	var members []teams.Member
	for _, title := range []string{"storage", "network"} {
		c, err := cell.Create(r.a.Home, cell.Options{Class: cell.HostBound})
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(c.Root, cell.TranscriptPath)
		if err := os.WriteFile(file, []byte("journal\n"), 0600); err != nil {
			t.Fatal(err)
		}
		members = append(members, teams.Member{Key: file, File: file, Word: title, JoinedAt: time.Unix(100, 0)})
	}
	relayTeamEdit(t, r.a, func(f *teams.File) {
		f.Teams = append(f.Teams, teams.Team{ID: "alpha", Name: "alpha", Members: members, Made: time.Unix(10, 0).UTC()})
	})
	r.round(t)
}

func TestTeamsPairCreateRenameAndOfflineDeleteWinsOverLaterRename(t *testing.T) {
	r := newRelayTeamsRig(t)
	relayTeamCreate(t, r)
	a, b := relayTeamRead(t, r.a), relayTeamRead(t, r.b)
	if len(b.Teams) != 1 || b.Teams[0].Name != "alpha" || len(b.Teams[0].Members) != 2 {
		t.Fatalf("received %+v", b)
	}
	for i, m := range b.Teams[0].Members {
		if m.Handle != a.Teams[0].Members[i].Handle {
			t.Fatal("handles differ")
		}
		if _, remote := teams.RemoteCell(m.Key); !remote || m.File != "" {
			t.Fatal("foreign path escaped into local opener")
		}
	}
	relayTeamEdit(t, r.b, func(f *teams.File) { f.Teams[0].Name = "renamed" })
	r.round(t)
	if got := relayTeamRead(t, r.a); len(got.Teams) != 1 || got.Teams[0].Name != "renamed" {
		t.Fatal("rename did not replace old name")
	}
	relayTeamEdit(t, r.a, func(f *teams.File) { f.Teams = nil })
	if err := r.a.SyncState(t.Context()); err != nil {
		t.Fatal(err)
	}
	relayTeamEdit(t, r.b, func(f *teams.File) { f.Teams[0].Name = "offline rename after delete" })
	r.round(t)
	if len(relayTeamRead(t, r.a).Teams) != 0 || len(relayTeamRead(t, r.b).Teams) != 0 {
		t.Fatal("offline rename resurrected team")
	}
}

func TestTeamsConcurrentMembersKeepBothHandlesAndOrderInEitherSyncOrder(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "a-first", true: "b-first"}[reverse], func(t *testing.T) {
			r := newRelayTeamsRig(t)
			relayTeamCreate(t, r)
			for i, s := range []*syncsetup.Sync{r.a, r.b} {
				relayTeamEdit(t, s, func(f *teams.File) {
					f.Teams[0].Members = append(f.Teams[0].Members, teams.Member{Key: []string{"x", "y"}[i], Word: []string{"lexer", "parser"}[i], JoinedAt: time.Unix(200, int64(i))})
				})
			}
			if reverse {
				r.a, r.b = r.b, r.a
			}
			r.round(t)
			a, b := relayTeamRead(t, r.a).Teams[0], relayTeamRead(t, r.b).Teams[0]
			portable := func(t teams.Team) []string {
				var out []string
				for _, m := range t.Members {
					out = append(out, m.Word+"/"+m.Handle)
				}
				return out
			}
			if len(a.Members) != 4 || !reflect.DeepEqual(portable(a), portable(b)) {
				t.Fatalf("concurrent membership: %+v %+v", a, b)
			}
		})
	}
}

// Both homes use a real paired relay. Only the display mirror receives the
// other writer's window; an older local-log reader therefore sees none of it.
func TestPairedTrafficMirrorsOnceWithoutWritingTheReceivingLog(t *testing.T) {
	r := newRelayTeamsRig(t)
	relayTeamCreate(t, r)
	for _, kind := range []string{teams.KindDirective, teams.KindStart, teams.KindQuestion, teams.KindStop} {
		if err := teams.AppendTraffic(r.a.Home, "alpha", teams.Entry{Kind: kind, From: teams.FromManager, To: "storage", Text: "history " + kind}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := teams.AppendTrafficID(r.a.Home, "alpha", teams.Entry{Kind: teams.KindQuestion, From: "storage", To: teams.ToManager, Text: "question"})
	if err != nil {
		t.Fatal(err)
	}
	if err := teams.AppendTraffic(r.a.Home, "alpha", teams.Entry{Kind: teams.KindAnswer, From: teams.FromManager, To: "storage", Text: "answer", Reply: first, Answers: first}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(teams.TrafficPath(r.a.Home, "alpha"))
	for range 5 {
		r.round(t)
	}
	local, err := teams.ReadTraffic(r.b.Home, "alpha", "", 0)
	if err != nil || len(local) != 0 {
		t.Fatalf("remote instruction in delivery log: %+v %v", local, err)
	}
	if _, err := os.Stat(teams.TrafficPath(r.b.Home, "alpha")); !os.IsNotExist(err) {
		t.Fatal("receiving local log was created")
	}
	display, err := teams.ReadHistory(r.b.Home, "alpha", 0)
	if err != nil || len(display) != 6 {
		t.Fatalf("mirror: %+v %v", display, err)
	}
	if display[5].Reply != display[4].ID || display[5].Answers != display[4].ID {
		t.Fatal("mirror lost its thread")
	}
	for _, line := range display {
		if line.ID != line.Origin {
			t.Fatal("mirror id is not portable")
		}
	}
	after, _ := os.ReadFile(teams.TrafficPath(r.a.Home, "alpha"))
	if !bytes.Equal(before, after) {
		t.Fatal("local bytes changed")
	}
}

func TestCrewSeatPinRouteAndProvidersOffFollowPairedComputers(t *testing.T) {
	r := newRelayTeamsRig(t)
	// ApplyPreferences is the same guarded profile transaction used by sync;
	// this fixture avoids requiring a live model catalog to admit a fake pin.
	changes := map[string]string{config.CrewSeatKey(crewroute.Worker): `"stub/worker"`, config.KeyCrewRouteWorker: `"stub/worker@stub-provider"`, config.KeyCrewProvidersOff: `["off-provider"]`}
	if err := config.ApplyPreferences(r.a.Home, changes); err != nil {
		t.Fatal(err)
	}
	r.round(t)
	got, err := config.ReadPreferences(r.b.Home)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range changes {
		var actual, wanted any
		if err := json.Unmarshal([]byte(got[key]), &actual); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(value), &wanted); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, wanted) {
			t.Fatalf("crew setting %s did not travel", key)
		}
	}
}

func TestTeamsReplaceMovesLocalTeamsAsideAndKeepsTraffic(t *testing.T) {
	home := t.TempDir()
	if err := teams.Update(home, func(f *teams.File) error { f.Teams = []teams.Team{{ID: "alpha", Name: "local"}}; return nil }); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(teams.Path(home))
	if err := teams.AppendTraffic(home, "alpha", teams.Entry{Kind: teams.KindNote, From: teams.FromYou, To: teams.ToRoom, Text: "keep history"}); err != nil {
		t.Fatal(err)
	}
	traffic, _ := os.ReadFile(teams.TrafficPath(home, "alpha"))
	if err := os.WriteFile(filepath.Join(home, "teams-sync.ledger"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "teams-traffic-sync.ledger"), []byte(`"2026-01-01T00:00:00Z"`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := replaceState(home, home); err != nil {
		t.Fatal(err)
	}
	aside, _ := filepath.Glob(teams.Path(home) + ".unreadable-*")
	if len(aside) != 1 {
		t.Fatal("local teams were not moved aside")
	}
	kept, _ := os.ReadFile(aside[0])
	after, _ := os.ReadFile(teams.TrafficPath(home, "alpha"))
	if !bytes.Equal(before, kept) || !bytes.Equal(traffic, after) {
		t.Fatal("replace discarded local history")
	}
	if _, err := os.Stat(filepath.Join(home, "teams-traffic-sync.ledger")); !os.IsNotExist(err) {
		t.Fatal("replace retained Traffic rate clock")
	}
	if _, err := os.Stat(filepath.Join(home, "teams-sync.ledger")); !os.IsNotExist(err) {
		t.Fatal("ledger survived replace")
	}
}

func TestTeamsConcurrentParentLoopHandleCollisionAndDeletedManagerConverge(t *testing.T) {
	r := newRelayTeamsRig(t)
	relayTeamCreate(t, r)
	relayTeamEdit(t, r.a, func(f *teams.File) { f.Teams = append(f.Teams, teams.Team{ID: "beta", Name: "beta"}) })
	r.round(t)
	relayTeamEdit(t, r.a, func(f *teams.File) {
		i := teams.Index(f.Teams, "alpha")
		f.Teams[i].Parent = "beta"
		f.Teams[i].Manager = f.Teams[i].Members[0].Key
		f.Teams[i].Members = append(f.Teams[i].Members, teams.Member{Key: "x", Word: "lexer", Handle: "shared", JoinedAt: time.Unix(200, 0)})
	})
	relayTeamEdit(t, r.b, func(f *teams.File) {
		i := teams.Index(f.Teams, "beta")
		f.Teams[i].Parent = "alpha"
		i = teams.Index(f.Teams, "alpha")
		f.Teams[i].Members = f.Teams[i].Members[1:]
		f.Teams[i].Members = append(f.Teams[i].Members, teams.Member{Key: "y", Word: "parser", Handle: "shared", JoinedAt: time.Unix(201, 0)})
	})
	r.round(t)
	a, b := relayTeamRead(t, r.a), relayTeamRead(t, r.b)
	for _, f := range []*teams.File{a, b} {
		alpha, _ := f.Team("alpha")
		beta, _ := f.Team("beta")
		if alpha.Parent != "" || beta.Parent != "alpha" || alpha.Manager != "" || len(alpha.Members) != 3 {
			t.Fatalf("merged laws: %+v", f.Teams)
		}
		handles := map[string]bool{}
		for _, member := range alpha.Members {
			if handles[member.Handle] {
				t.Fatal("duplicate handle survived")
			}
			handles[member.Handle] = true
			if member.Word == "lexer" && member.Handle != "shared" {
				t.Fatal("earlier join lost handle")
			}
		}
	}
	for i := range a.Teams {
		if a.Teams[i].ID != b.Teams[i].ID || a.Teams[i].Parent != b.Teams[i].Parent {
			t.Fatal("parent repair differed")
		}
		for j := range a.Teams[i].Members {
			am, bm := a.Teams[i].Members[j], b.Teams[i].Members[j]
			if am.Word != bm.Word || am.Handle != bm.Handle || am.Home != bm.Home {
				t.Fatal("membership repair differed")
			}
		}
	}
}

func TestTeamsIdleProfileRoundSharesEditsWithoutPublishingAChat(t *testing.T) {
	r := newRelayTeamsRig(t)
	r.a.Interval = 25 * time.Millisecond
	r.b.Interval = 25 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{}, 2)
	for _, s := range []*syncsetup.Sync{r.a, r.b} {
		go func() { s.RunState(ctx); done <- struct{}{} }()
	}
	defer func() { cancel(); <-done; <-done }()
	wait := func(want string) {
		t.Helper()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			f := relayTeamRead(t, r.b)
			if want == "" && len(f.Teams) == 0 {
				return
			}
			if len(f.Teams) == 1 && f.Teams[0].Name == want {
				return
			}
			select {
			case <-deadline.C:
				t.Fatalf("idle round did not reach %q", want)
			case <-tick.C:
			}
		}
	}
	relayTeamEdit(t, r.a, func(f *teams.File) { f.Teams = []teams.Team{{ID: "alpha", Name: "alpha"}} })
	wait("alpha")
	relayTeamEdit(t, r.a, func(f *teams.File) { f.Teams[0].Name = "renamed while idle" })
	wait("renamed while idle")
	relayTeamEdit(t, r.a, func(f *teams.File) { f.Teams = nil })
	wait("")
}

func TestUnpairedTeamsAndTrafficKeepOriginalBytesAndMakeNoRequest(t *testing.T) {
	r := newRelayTeamsRig(t)
	home := t.TempDir()
	if _, err := identity.EnsureSolo(home); err != nil {
		t.Fatal(err)
	}
	s, ok, err := syncsetup.Open(home)
	if err != nil || !ok {
		t.Fatalf("unpaired setup: %v %v", ok, err)
	}
	cursorPath := filepath.Join(home, "chat", ".cell", "team.json")
	if err := os.MkdirAll(filepath.Dir(cursorPath), 0700); err != nil {
		t.Fatal(err)
	}
	cursors := []byte(`{"read":{"alpha":"000000000007"}}`)
	if err := os.WriteFile(cursorPath, cursors, 0600); err != nil {
		t.Fatal(err)
	}
	beforeRequests := r.requests.Load()
	// These wire shapes predate relay teams. Fixed timestamps make the byte
	// comparison independent of clocks and random team ids.
	for _, name := range []string{"alpha", "renamed"} {
		relayTeamEdit(t, s, func(f *teams.File) {
			f.Teams = []teams.Team{{ID: "alpha", Name: name, Made: time.Unix(10, 0).UTC(), Members: []teams.Member{{Key: "chat", File: "/local/chat", Where: "/local", Word: "storage", Handle: "storage"}}}}
		})
		before, _ := os.ReadFile(teams.Path(home))
		var expected any
		legacy := `{"version":2,"teams":[{"id":"alpha","name":"` + name + `","parent":"","members":[{"key":"chat","file":"/local/chat","where":"/local","word":"storage","handle":"storage"}],"manager":"","made":"1970-01-01T00:00:10Z"}]}`
		// Compare both semantic shape and byte identity across an idle sync round.
		if err := json.Unmarshal(before, &expected); err != nil {
			t.Fatal(err)
		}
		var oldShape any
		if err := json.Unmarshal([]byte(legacy), &oldShape); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(expected, oldShape) {
			t.Fatalf("unpaired shape changed: %s", before)
		}
		if err := s.SyncState(t.Context()); err != nil {
			t.Fatal(err)
		}
		after, _ := os.ReadFile(teams.Path(home))
		if !bytes.Equal(before, after) {
			t.Fatal("unpaired sync changed teams bytes")
		}
	}
	relayTeamEdit(t, s, func(f *teams.File) {
		f.Teams[0].Members = append(f.Teams[0].Members, teams.Member{Key: "second", Word: "network"})
	})
	if err := teams.AppendTraffic(home, "alpha", teams.Entry{At: time.Unix(11, 0).UTC(), Kind: teams.KindNote, From: teams.FromYou, To: teams.ToRoom, Text: "original bytes"}); err != nil {
		t.Fatal(err)
	}
	traffic, _ := os.ReadFile(teams.TrafficPath(home, "alpha"))
	golden := "{\"id\":\"000000000001\",\"at\":\"1970-01-01T00:00:11Z\",\"kind\":\"note\",\"from\":\"you\",\"to\":\"room\",\"text\":\"original bytes\"}\n"
	if string(traffic) != golden {
		t.Fatalf("unpaired traffic bytes changed: %s", traffic)
	}
	relayTeamEdit(t, s, func(f *teams.File) { f.Teams = nil })
	if err := s.SyncState(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(teams.TrafficPath(home, "alpha"))
	if !bytes.Equal(traffic, after) || r.requests.Load() != beforeRequests {
		t.Fatal("unpaired sync touched history or made a request")
	}
	afterCursors, _ := os.ReadFile(cursorPath)
	if !bytes.Equal(afterCursors, cursors) {
		t.Fatal("unpaired sync changed chat cursors")
	}
	if _, err := os.Stat(filepath.Join(home, "teams-sync.ledger")); !os.IsNotExist(err) {
		t.Fatal("unpaired teams ledger created")
	}
}

func TestTrafficMirrorsKeepConcurrentLocalSequenceCollisionsSeparate(t *testing.T) {
	r := newRelayTeamsRig(t)
	relayTeamCreate(t, r)
	for i, s := range []*syncsetup.Sync{r.a, r.b} {
		id, err := teams.AppendTrafficID(s.Home, "alpha", teams.Entry{Kind: teams.KindNote, From: teams.FromYou, To: teams.ToRoom, Text: []string{"left", "right"}[i]})
		if err != nil || id != "000000000001" {
			t.Fatalf("fixture did not mint colliding ids: %s %v", id, err)
		}
	}
	for range 3 {
		r.round(t)
	}
	for _, s := range []*syncsetup.Sync{r.a, r.b} {
		lines, err := teams.ReadHistory(s.Home, "alpha", 0)
		if err != nil || len(lines) != 2 {
			t.Fatalf("concurrent origin union: %+v %v", lines, err)
		}
		texts := map[string]bool{}
		imported := 0
		for _, line := range lines {
			texts[line.Text] = true
			if line.Origin != "" {
				imported++
			}
		}
		if !texts["left"] || !texts["right"] || imported != 1 {
			t.Fatal("one origin overwrote the other")
		}
	}
}

// Saturating many teams must not make the shared object grow with local log
// size. Compare the actual encoded Traffic entries and the encrypted file.
func TestPairedTrafficAcrossTenTeamsRespectsEveryBudgetAndQuietRoundsWriteNothing(t *testing.T) {
	r := newRelayTeamsRig(t)
	for _, s := range []*syncsetup.Sync{r.a, r.b} {
		if err := teams.Update(s.Home, func(f *teams.File) error {
			for i := 0; i < 10; i++ {
				f.Teams = append(f.Teams, teams.Team{ID: fmt.Sprintf("team-%02d", i), Name: fmt.Sprintf("team-%02d", i)})
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 10; i++ {
			for j := 0; j < 80; j++ {
				if err := teams.AppendTraffic(s.Home, fmt.Sprintf("team-%02d", i), teams.Entry{At: time.Unix(int64(i*100+j+1), 0), Kind: teams.KindNote, From: teams.FromYou, To: teams.ToRoom, Text: strings.Repeat("界", 1800)}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	r.round(t)
	for _, s := range []*syncsetup.Sync{r.a, r.b} {
		v, err := keys.Open(s.Home)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := v.Entries("home:windows:team-traffic")
		if err != nil {
			t.Fatal(err)
		}
		totals := map[string]int{}
		recent := map[string]bool{}
		for _, e := range entries {
			_, device, _ := strings.Cut(e.Name, "/")
			encoded, _ := json.Marshal(e)
			totals[device] += len(encoded)
			if strings.HasPrefix(e.Name, "team-09/") {
				recent[device] = true
			}
			if strings.HasPrefix(e.Name, "team-00/") {
				t.Fatal("least-active team displaced a recent window")
			}
			var lines []teams.Entry
			if err := json.Unmarshal([]byte(e.Value), &lines); err != nil {
				t.Fatal(err)
			}
			if len(lines) > teams.TrafficSyncLines || len(e.Value) > teams.TrafficSyncBytes || len(lines) == 0 {
				t.Fatal("window over bound", e.Name)
			}
			for _, line := range lines {
				if len(line.Text) > teams.TrafficSyncTextBytes+len(" …") || !strings.HasSuffix(line.Text, " …") {
					t.Fatal("long line omitted or not shortened")
				}
			}
		}
		if len(recent) != 2 {
			t.Fatal("newest team did not receive the device budget", recent)
		}
		if len(totals) != 2 {
			t.Fatal("missing writer", totals)
		}
		total := 0
		for _, n := range totals {
			if n > teams.TrafficSyncDeviceBytes {
				t.Fatalf("device share %d", n)
			}
			total += n
		}
		if total > teams.TrafficSyncDeviceBytes*2 {
			t.Fatal("aggregate budget exceeded")
		}
	}
	beforeA, _ := os.ReadFile(filepath.Join(r.a.Home, "vault.enc"))
	beforeB, _ := os.ReadFile(filepath.Join(r.b.Home, "vault.enc"))
	r.round(t)
	afterA, _ := os.ReadFile(filepath.Join(r.a.Home, "vault.enc"))
	afterB, _ := os.ReadFile(filepath.Join(r.b.Home, "vault.enc"))
	if !bytes.Equal(beforeA, afterA) || !bytes.Equal(beforeB, afterB) {
		t.Fatal("unchanged round rewrote vault")
	}
}

func TestPairedMemberLocationUsesTheCurrentHolderEvenWithALocalTranscript(t *testing.T) {
	r := newRelayTeamsRig(t)
	relayTeamCreate(t, r)
	f, _ := teams.Snapshot(r.a.Home)
	member := f.Teams[0].Members[0]
	c, err := cell.OpenAt(filepath.Dir(filepath.Dir(member.File)), filepath.Base(filepath.Dir(filepath.Dir(member.File))))
	if err != nil {
		t.Fatal(err)
	}
	created, err := r.a.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: strings.Repeat("1", 64), Class: string(cell.HostBound)})
	if err != nil {
		t.Fatal(err)
	}
	if name, err := r.b.TeamComputer(t.Context(), teams.Member{Key: teams.RemoteKey(c.ID), SyncID: "cell:" + c.ID}); err != nil || name != "laptop" {
		t.Fatal(name, err)
	}
	_, err = r.b.Dir.Acquire(t.Context(), c.ID, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: r.a.Device.ID(), Fence: created.Cell.Lease.Fence}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.a.Rows(t.Context()); err != nil {
		t.Fatal(err)
	}
	name, err := r.a.TeamComputer(t.Context(), member)
	if err != nil || name != "desktop" {
		t.Fatalf("stale transcript hid remote holder: %q %v", name, err)
	}
}

// Display failures cannot hold unrelated profile changes behind the mirror.
func TestUnwritableTrafficMirrorStillSharesChangedPreferences(t *testing.T) {
	r := newRelayTeamsRig(t)
	relayTeamCreate(t, r)
	path := teams.TrafficMirrorPath(r.a.Home, "alpha")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	changes := map[string]string{config.KeyCrewRouteWorker: `"fixture-route"`}
	if err := config.ApplyPreferences(r.a.Home, changes); err != nil {
		t.Fatal(err)
	}
	r.round(t)
	got, err := config.ReadPreferences(r.b.Home)
	if err != nil || got[config.KeyCrewRouteWorker] != changes[config.KeyCrewRouteWorker] {
		t.Fatal("mirror blocked preference", got, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := teams.AppendTraffic(r.b.Home, "alpha", teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: teams.ToRoom, Text: "retry mirror"}); err != nil {
		t.Fatal(err)
	}
	r.round(t)
	lines, err := teams.ReadHistory(r.a.Home, "alpha", 0)
	if err != nil || len(lines) != 1 || lines[0].Text != "retry mirror" {
		t.Fatal("mirror did not retry", lines, err)
	}
}

func TestDamagedTeamsStillSharesChangedPreferences(t *testing.T) {
	r := newRelayTeamsRig(t)
	relayTeamCreate(t, r)
	if err := os.WriteFile(teams.Path(r.a.Home), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	changes := map[string]string{config.KeyCrewRouteWorker: `"fixture-route"`}
	if err := config.ApplyPreferences(r.a.Home, changes); err != nil {
		t.Fatal(err)
	}
	r.round(t)
	got, err := config.ReadPreferences(r.b.Home)
	if err != nil || got[config.KeyCrewRouteWorker] != changes[config.KeyCrewRouteWorker] {
		t.Fatal("damaged team blocked unrelated state", got, err)
	}
}
