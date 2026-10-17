package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

var _ appwire.OrdersMadePager = (*appBackend)(nil)

func madeOrder(t *testing.T, store *automation.Store, title, crewBelongsTo string, team *automation.OriginTeam) automation.Automation {
	t.Helper()
	a, err := store.Create(automation.Automation{
		Title: title, Crew: crewBelongsTo, Schedule: automation.Schedule{Every: "1h", Zone: "UTC"},
		Action: automation.Action{Do: "do " + title}, Workspace: t.TempDir(),
		Origin: automation.Origin{SessionID: "s", Transcript: "/t/" + title, Team: team},
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// THE CREW'S ORDERS ARE THE ONES WHOSE RECORDED CREATION TEAM IS ITS ID: not
// where an order belongs, not who manages the crew now, and an untagged order is
// counted as unknown instead of vanishing — all across the store reopening.
func TestAppOrdersMadeInReadsTheRecordedTeamOnly(t *testing.T) {
	backend, _, _, crew := crewBackendOn(t, &fakeConversation{})
	dir := filepath.Join(t.TempDir(), "automations")
	store, err := automation.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	backend.orderStore = func() *automation.Store { return store }
	here := &automation.OriginTeam{Known: true, ID: crew}
	made := madeOrder(t, store, "made-here", "", here)
	// Belongs here by the legacy field, but was made nowhere we can prove.
	madeOrder(t, store, "belongs-only", crew, nil)
	madeOrder(t, store, "ordinary", crew, &automation.OriginTeam{Known: true})
	madeOrder(t, store, "elsewhere", "", &automation.OriginTeam{Known: true, ID: "otherteam1"})

	// The manager moves. Nothing about the rows changes.
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		return f.SetManager(crew, "/state/v3/places/proj/b0b0b0b0/transcript.jsonl")
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = automation.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	res, err := backend.PageOrdersMadeIn(context.Background(), appwire.OrdersMadeAsk{Crew: crew})
	if err != nil || len(res.Orders) != 1 || res.Orders[0].ID != made.ID || res.Next != "" {
		t.Fatalf("page = %+v, %v", res, err)
	}
	if res.Orders[0].LastRun != nil || res.Orders[0].Asking != nil {
		t.Fatalf("a page row carried per-row reads: %+v", res.Orders[0])
	}
	// "belongs-only" has no team recorded at all: unknown. "ordinary" is known none.
	if res.Unknown != 1 {
		t.Fatalf("unknown = %d, want 1 (the order whose creation team was never recorded)", res.Unknown)
	}
}

// A CREW THAT DOES NOT EXIST IS NOT_FOUND, A CLOSED ONE STILL READS, AND A BAD
// ASK NEVER TOUCHES THE STORE.
func TestAppOrdersMadeInCrewGuards(t *testing.T) {
	backend, _, store := ordersBackend(t)
	backend.profileDir = t.TempDir()
	if _, err := backend.PageOrdersMadeIn(context.Background(), appwire.OrdersMadeAsk{Crew: "nosuchcrew1"}); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("a missing crew answered %v", err)
	}
	if _, err := backend.PageOrdersMadeIn(context.Background(), appwire.OrdersMadeAsk{Crew: "../x"}); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("a bad crew answered %v", err)
	}
	id := teamstore.NewID()
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: id, Name: "old", Made: backend.now()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	madeOrder(t, store, "history", "", &automation.OriginTeam{Known: true, ID: id})
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error { return f.Close(id, backend.now(), "done") }); err != nil {
		t.Fatal(err)
	}
	res, err := backend.PageOrdersMadeIn(context.Background(), appwire.OrdersMadeAsk{Crew: id})
	if err != nil || len(res.Orders) != 1 {
		t.Fatalf("a closed crew read %+v, %v", res, err)
	}
}

// THE PAGE IS BOUNDED AND ITS CURSOR WALKS THE REST WITHOUT REPEATS.
func TestAppOrdersMadeInPagesWithoutRepeats(t *testing.T) {
	backend, _, _, crew := crewBackendOn(t, &fakeConversation{})
	store, err := automation.Open(filepath.Join(t.TempDir(), "automations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	backend.orderStore = func() *automation.Store { return store }
	for _, title := range []string{"a", "b", "c", "d", "e"} {
		madeOrder(t, store, title, "", &automation.OriginTeam{Known: true, ID: crew})
	}
	seen := map[string]bool{}
	cursor, pages := "", 0
	for {
		res, err := backend.PageOrdersMadeIn(context.Background(), appwire.OrdersMadeAsk{Crew: crew, Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if len(res.Orders) > 2 {
			t.Fatalf("page of %d over limit 2", len(res.Orders))
		}
		for _, o := range res.Orders {
			if seen[o.ID] {
				t.Fatalf("%s came twice", o.ID)
			}
			seen[o.ID] = true
		}
		if res.Next == "" {
			break
		}
		cursor = res.Next
		if pages > 5 {
			t.Fatal("the walk never ended")
		}
	}
	if len(seen) != 5 || pages != 3 {
		t.Fatalf("saw %d orders over %d pages, want 5 over 3", len(seen), pages)
	}
}

// THE REGISTERED WIRE: hello names orders.made_in, and a real request over
// Serve is answered with the recorded rows and the unknown count.
func TestAppOrdersMadeInOverTheRegisteredWire(t *testing.T) {
	backend, _, _, crew := crewBackendOn(t, &fakeConversation{})
	store, err := automation.Open(filepath.Join(t.TempDir(), "automations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	backend.orderStore = func() *automation.Store { return store }
	made := madeOrder(t, store, "wired", "", &automation.OriginTeam{Known: true, ID: crew})
	madeOrder(t, store, "legacy", "", nil)

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})
	server := appwire.NewServer(outW)
	go func() {
		defer close(done)
		defer outW.Close()
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, backend)
	}()
	t.Cleanup(func() { _ = inW.Close(); _ = outR.Close(); <-done })
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	if !scanner.Scan() {
		t.Fatal("no hello")
	}
	if !strings.Contains(scanner.Text(), `"`+appwire.FeatureOrdersMadeIn+`"`) {
		t.Fatalf("hello does not name %s: %s", appwire.FeatureOrdersMadeIn, scanner.Text())
	}
	encoded, _ := json.Marshal(map[string]any{"id": 1, "method": "orders.made_in", "params": map[string]any{"crew": crew}})
	if _, err := inW.Write(append(encoded, '\n')); err != nil {
		t.Fatal(err)
	}
	for scanner.Scan() {
		var reply struct {
			ID     int                      `json:"id"`
			Result appwire.OrdersMadeResult `json:"result"`
			Error  *appwire.Error           `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if reply.ID != 1 {
			continue
		}
		if reply.Error != nil || len(reply.Result.Orders) != 1 || reply.Result.Orders[0].ID != made.ID || reply.Result.Unknown != 1 {
			t.Fatalf("reply = %+v", reply)
		}
		return
	}
	t.Fatalf("no reply: %v", scanner.Err())
}
