package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// E-R1: sync.get reports the relay setting without contacting it.
func TestAppRelayER1(t *testing.T) {
	_, _, w := appDeviceBackend(t, t.TempDir())
	for _, tc := range []struct {
		value, source string
		off           bool
	}{
		{"http://127.0.0.1:1", "environment", false}, {"off", "environment", true}, {"", "default", false},
	} {
		t.Setenv(syncsetup.URLVar, tc.value)
		var got struct {
			Sync struct {
				Relay *struct {
					URL, Source string
					Off         bool
				}
			}
		}
		raw, _ := json.Marshal(machineCall[any](t, w, "sync.get", struct{}{}))
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.Sync.Relay == nil || got.Sync.Relay.Source != tc.source || got.Sync.Relay.Off != tc.off || !tc.off && tc.value != "" && got.Sync.Relay.URL != tc.value {
			t.Fatalf("%q: %s", tc.value, raw)
		}
	}
}

// E-P3: cancellation is terminal and recoverable on the wire.
func TestAppPairEP3(t *testing.T) {
	celltest.Available(t)
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	_, rec, w := appDeviceBackend(t, t.TempDir())
	started := machineCall[appwire.PairResult](t, w, "pair.start", appwire.PairStartParams{Mode: "offer_code"}).Pairing
	if started.Lines == nil {
		t.Fatal("pair lines must be an array")
	}
	waiting := e2PairWait(t, w, started.ID, "waiting")
	if _, err := w.call("pair.start", appwire.PairStartParams{Mode: "offer_code"}); err == nil || err.Code != appwire.CodeBusy {
		t.Fatalf("second pairing: %+v", err)
	}
	if waiting.Code == "" {
		t.Fatal("no code")
	}
	got := machineCall[appwire.PairResult](t, w, "pair.cancel", appwire.IDParams{ID: started.ID}).Pairing
	if got.State != "cancelled" {
		t.Fatalf("cancel: %+v", got)
	}
	last := machineCall[appwire.PairGetResult](t, w, "pair.get", struct{}{}).Pairing
	if last == nil || last.ID != started.ID || last.State != "cancelled" {
		t.Fatalf("last: %+v", last)
	}
	n := 0
	rec.mu.Lock()
	for i, event := range rec.events {
		if event == appwire.EventPairChanged && rec.data[i].(appwire.PairResult).Pairing.State == "cancelled" {
			n++
		}
	}
	rec.mu.Unlock()
	if n != 1 {
		t.Fatalf("final events: %d", n)
	}
	_, _, other := appDeviceBackend(t, t.TempDir())
	attempt := machineCall[appwire.PairResult](t, other, "pair.start", appwire.PairStartParams{Mode: "use_code", Code: waiting.Code}).Pairing
	e2PairWait(t, other, attempt.ID, "failed")
	next := machineCall[appwire.PairResult](t, w, "pair.start", appwire.PairStartParams{Mode: "offer_code"}).Pairing
	previous := machineCall[appwire.PairGetResult](t, w, "pair.get", appwire.PairGetParams{ID: started.ID}).Pairing
	if previous == nil || previous.State != "cancelled" {
		t.Fatalf("previous operation lost: %+v", previous)
	}
	machineCall[appwire.PairResult](t, w, "pair.cancel", appwire.IDParams{ID: next.ID})

}

func e2PairWait(t *testing.T, w *actionWire, id, state string) appwire.Pairing {
	t.Helper()
	var got appwire.Pairing
	appSyncWait(t, "pair "+state, func() bool {
		p := machineCall[appwire.PairGetResult](t, w, "pair.get", appwire.PairGetParams{ID: id}).Pairing
		if p == nil {
			return false
		}
		got = *p
		if got.State == "failed" && state != "failed" {
			t.Fatalf("pair failed: %+v", got)
		}
		return got.State == state
	})
	return got
}

// E-C1: inventory events arrive without another list call; local ids are folder ids.
func TestAppChatsEC1(t *testing.T) {
	celltest.Available(t)
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	dirA, dirB := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(dirA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = identity.Adopt(dirB, id, false); err != nil {
		t.Fatal(err)
	}
	a := appDeviceSync(t, dirA)
	b := appDeviceSync(t, dirB)
	for _, s := range []*syncsetup.Sync{a, b} {
		name := map[string]string{dirA: "A", dirB: "B"}[s.Home]
		if _, err := devname.Set(s.Home, name); err != nil {
			t.Fatal(err)
		}
		if err := syncsetup.PutOwnDevice(context.Background(), s.Dir, s.Identity, s.Device, name); err != nil {
			t.Fatal(err)
		}
	}
	backendA, _, wa := appDeviceBackend(t, dirA)
	_, recB, wb := appDeviceBackend(t, dirB)
	first := machineCall[appwire.ChatsResult](t, wb, "chats.list", struct{}{})
	if len(first.Chats) != 0 {
		t.Fatal(first)
	}
	c, err := cell.CreateIn(filepath.Join(dirA, "v3", "projects", "project"), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	localID := "saved-folder"
	if err := session.SaveMeta(c.Root, session.Meta{ID: localID, Workspace: t.TempDir(), Title: "A chat"}); err != nil {
		t.Fatal(err)
	}
	// The app id deliberately differs from the directory id.
	agent := backendA.agent(localID)
	backendA.locked(func() { agent.transcript = filepath.Join(c.Root, cell.TranscriptPath); agent.view.ID = localID })
	title, err := directory.SealName(directory.MetadataKey(id.CellKey()), "A chat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Dir.Create(context.Background(), c.ID, directory.CellInit{Title: title}); err != nil {
		t.Fatal(err)
	}
	recB.waitFor(t, 10*time.Second, func(name string, data any) bool {
		if name != appwire.EventChatChanged {
			return false
		}
		row := data.(appwire.ChatChanged).Chat
		return row.Cell == c.ID && !row.Here && row.LocalAgent == "" && row.Holder != nil && row.Holder.Name == "A"
	})
	rows := machineCall[appwire.ChatsResult](t, wa, "chats.list", struct{}{})
	if len(rows.Chats) != 1 || !rows.Chats[0].Here || rows.Chats[0].LocalAgent != localID {
		t.Fatalf("local rows: %+v", rows)
	}

	recB.waitFor(t, 10*time.Second, func(name string, data any) bool {
		if name != appwire.EventChatChanged {
			return false
		}
		row := data.(appwire.ChatChanged).Chat
		return row.Cell == c.ID && row.Online != nil && *row.Online
	})
	if err := backendA.Close(); err != nil {
		t.Fatal(err)
	}

	if err := a.Dir.Archive(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	recB.waitFor(t, 10*time.Second, func(name string, data any) bool {
		return name == appwire.EventChatRemoved && data.(appwire.ChatRemoved).Cell == c.ID
	})
}

// E-P1 and E-P2: explicit replacement precedes any exchange; both pairing sides
// answer the same typed app door, with two homes and a loopback relay.
func TestM1M4M8M9M10AppPair(t *testing.T) {
	for _, used := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "merge"}[used], func(t *testing.T) {
			celltest.Available(t)
			relay := e2Relay(t)
			t.Setenv(syncsetup.URLVar, relay.URL)
			dirA, dirB := t.TempDir(), t.TempDir()
			// E-P1 starts with a saved chat on the offering computer.
			source := filepath.Join(dirA, "v3", "projects", "local", "source")
			if err := session.SaveMeta(source, session.Meta{ID: "source", Title: "A saved chat", Workspace: t.TempDir()}); err != nil {
				t.Fatal(err)
			}
			_, recA, wa := appDeviceBackend(t, dirA)
			for _, feature := range []string{appwire.FeatureDevicesPair, appwire.FeatureChatsTake, appwire.FeatureSyncRelay} {
				if !slices.Contains(wa.hello, feature) {
					t.Fatalf("missing feature %s", feature)
				}
			}
			_, recB, wb := appDeviceBackend(t, dirB)
			defer assertAppPairCopyM9(t, recA)
			defer assertAppPairCopyM9(t, recB)
			var kept string
			if used {
				if _, err := identity.EnsureSolo(dirB); err != nil {
					t.Fatal(err)
				}
				kept = filepath.Join(dirB, "v3", "projects", "local", "saved", "meta.json")
				if err := os.MkdirAll(filepath.Dir(kept), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(kept, []byte(`{"id":"saved","title":"keep me"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			offer := machineCall[appwire.PairResult](t, wa, "pair.start", appwire.PairStartParams{Mode: "offer_code", Name: "A"}).Pairing
			waiting := e2PairWait(t, wa, offer.ID, "waiting")
			join := machineCall[appwire.PairResult](t, wb, "pair.start", appwire.PairStartParams{Mode: "use_code", Code: waiting.Code, Name: "B"}).Pairing
			q := e2PairWait(t, wa, offer.ID, "question")
			if q.Question.Kind != "approve" || q.Words == "" {
				t.Fatal(q)
			}
			machineCall[appwire.PairResult](t, wa, "pair.answer", appwire.PairAnswerParams{ID: offer.ID, Question: q.Question.ID, Option: "y"})
			e2PairWait(t, wa, offer.ID, "done")
			done := e2PairWait(t, wb, join.ID, "done")
			if used {
				if raw, err := os.ReadFile(kept); err != nil || !strings.Contains(string(raw), "keep me") {
					t.Fatalf("M1 own chat lost: %s %v", raw, err)
				}
			}

			if !done.Restart {
				t.Fatal("join did not request restart")
			}
			if got := appDeviceList(t, wa); len(got.Devices) != 2 {
				t.Fatalf("A devices: %+v", got)
			}
			if got := appDeviceList(t, wb); len(got.Devices) != 2 {
				t.Fatalf("B devices: %+v", got)
			}
			t.Setenv(syncsetup.URLVar, "")
			relayFact := machineCall[appwire.SyncResult](t, wb, "sync.get", struct{}{}).Sync.Relay
			if relayFact == nil || relayFact.Source != "paired" || relayFact.URL != relay.URL {
				t.Fatalf("joined relay: %+v", relayFact)
			}
		})
	}
}

func assertAppPairCopyM9(t *testing.T, rec *appRecorder) {
	t.Helper()
	allowed := regexp.MustCompile(`^(Pairing shares your chats with another computer\.|On your other computer, choose Pair a computer, then Enter a code from the other computer, and type [0-9-]+\. This code lasts 10 minutes\.|Waiting for approval on your other computer\. It should show: .+\.|Paired with .+\. Your chats can continue on both computers\.|Paired\. Your chats are kept\. Both computers' chats will appear as sharing finishes\.|Cancelled\. Nothing was paired\.|Pair with .+\?|Pair only if your other computer shows the same (three words|check number): .+\.|Pair|Don't pair|Open this pairing link on a computer you already use\.|Compare the check number on both computers: .+\.|Waiting for approval\. This link lasts [0-9]+ minutes\.)$`)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for i, name := range rec.events {
		if name != appwire.EventPairChanged {
			continue
		}
		p := rec.data[i].(appwire.PairResult).Pairing
		if p.Question != nil && p.Question.Kind == "replace_own_chats" {
			t.Error("M8 app asked to replace chats")
		}
		visible := slices.Clone(p.Lines)
		if q := p.Question; q != nil {
			visible = append(visible, q.Title, q.Detail)
			for _, option := range q.Options {
				visible = append(visible, option.Label)
				if !slices.Contains([]string{"Pair", "Don't pair"}, option.Label) {
					t.Errorf("L3 option: %q", option.Label)
				}
			}
		}
		for _, line := range visible {
			if line != "" && !allowed.MatchString(line) {
				t.Errorf("L3 unexpected app copy: %q", line)
			}

			for _, banned := range []string{"codeaf pair", "--replace", "device", "identity", "cell", "lease", "fence", "ledger", "vault"} {
				if strings.Contains(strings.ToLower(line), banned) {
					t.Errorf("L3 terminal copy: %q", line)
				}
			}
		}
	}
}

// E-P4: a wrong code uses the engine's failure sentence and adds no computer.
func TestAppPairEP4(t *testing.T) {
	celltest.Available(t)
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	_, _, wa := appDeviceBackend(t, t.TempDir())
	_, _, wb := appDeviceBackend(t, t.TempDir())
	offer := machineCall[appwire.PairResult](t, wa, "pair.start", appwire.PairStartParams{Mode: "offer_code"}).Pairing
	p := e2PairWait(t, wa, offer.ID, "waiting")
	last := p.Code[len(p.Code)-1]
	wrong := p.Code[:len(p.Code)-1] + string('0'+(last-'0'+1)%10)
	join := machineCall[appwire.PairResult](t, wb, "pair.start", appwire.PairStartParams{Mode: "use_code", Code: wrong}).Pairing
	failed := e2PairWait(t, wb, join.ID, "failed")
	if failed.Error == nil || failed.Error.Message != "That code did not work, or it timed out. Ask for a new one." {
		t.Fatalf("failure: %+v", failed)
	}
	if got := appDeviceList(t, wa); len(got.Devices) != 1 {
		t.Fatal(got)
	}
	machineCall[appwire.PairResult](t, wa, "pair.cancel", appwire.IDParams{ID: offer.ID})
}

// E-P1's link counterparts: the advertisement includes all four modes.
func TestAppPairLinksEP1(t *testing.T) {
	celltest.Available(t)
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	a, recA, wa := appDeviceBackend(t, t.TempDir())
	_, recB, wb := appDeviceBackend(t, t.TempDir())
	defer assertAppPairCopyM9(t, recA)
	defer assertAppPairCopyM9(t, recB)
	if _, err := identity.Ensure(a.relayHome()); err != nil {
		t.Fatal(err)
	}
	request := machineCall[appwire.PairResult](t, wb, "pair.start", appwire.PairStartParams{Mode: "request_link", Name: "B"}).Pairing
	waiting := e2PairWait(t, wb, request.ID, "waiting")
	approve := machineCall[appwire.PairResult](t, wa, "pair.start", appwire.PairStartParams{Mode: "approve_link", Link: waiting.Link}).Pairing
	q := e2PairWait(t, wa, approve.ID, "question")
	machineCall[appwire.PairResult](t, wa, "pair.answer", appwire.PairAnswerParams{ID: approve.ID, Question: q.Question.ID, Option: "y"})
	e2PairWait(t, wa, approve.ID, "done")
	done := e2PairWait(t, wb, request.ID, "done")
	if !done.Restart {
		t.Fatal("link join needs restart")
	}
	if got := appDeviceList(t, wb); len(got.Devices) != 2 {
		t.Fatal(got)
	}
}

// E-C2: even a completed independent copy must not offer an older home-folder
// chat. The original local agent id differs from the directory copy id.
func TestAppChatsEC2(t *testing.T) {
	celltest.Available(t)
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	dirA, dirB := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(dirA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(dirB, id, false); err != nil {
		t.Fatal(err)
	}
	a := appDeviceSync(t, dirA)
	backend, _, w := appDeviceBackend(t, dirB)
	original := filepath.Join(dirB, "v3", "projects", "old", "old-chat")
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(original, session.Meta{ID: "old-chat", Workspace: userHome}); err != nil {
		t.Fatal(err)
	}
	copied := cell.CopyRoot(original)
	c, err := cell.CreateIn(filepath.Dir(copied), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(c.Root, copied); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copied, cell.StateDir, "copy-ready"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	agent := backend.agent("old-chat")
	backend.locked(func() { agent.transcript = filepath.Join(original, "transcript.jsonl") })
	remote := filepath.Base(copied)
	if _, err := a.Dir.Create(context.Background(), remote, directory.CellInit{Head: "saved-head"}); err != nil {
		t.Fatal(err)
	}
	rows := machineCall[appwire.ChatsResult](t, w, "chats.list", struct{}{})
	if len(rows.Chats) != 1 || rows.Chats[0].CanTake || rows.Chats[0].LocalAgent != "old-chat" {
		t.Fatalf("older chat: %+v", rows)
	}
}

// E-P2: joining while an ordinary app turn runs is refused before pairing starts.
func TestAppPairLiveWorkBusyEP2(t *testing.T) {
	celltest.Available(t)
	t.Setenv(syncsetup.URLVar, "off")
	hold := make(chan struct{})
	conv := &fakeConversation{hold: hold}
	var mu sync.Mutex
	opens, releases := 0, 0
	backend := newFakeBackend(newAppRecorder(), fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	defer close(hold)
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "working", "busy"); err != nil {
		t.Fatal(err)
	}
	w := newActionWire(t, backend)
	_, failure := w.call("pair.start", appwire.PairStartParams{Mode: "use_code", Code: "42-123-456"})
	if failure == nil || failure.Code != appwire.CodeBusy || failure.Message != appJoinBusy {
		t.Fatalf("join during work: %+v", failure)
	}
	last := machineCall[appwire.PairGetResult](t, w, "pair.get", struct{}{})
	if last.Pairing != nil {
		t.Fatal("busy join started a pairing")
	}
}

// E-C1: a blocked inventory request cannot hold up the ordinary local list.
func TestAppChatsLocalListDoesNotWaitEC1(t *testing.T) {
	celltest.Available(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	relay := e2WireRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/dir/list" {
				once.Do(func() { close(entered) })
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	dir := t.TempDir()
	if _, err := identity.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	backend, _, _ := appDeviceBackend(t, dir)
	t.Cleanup(func() { close(release) })
	done := make(chan error, 1)
	go func() { _, err := backend.ListAgents(""); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("inventory read did not start")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("local list waited for the blocked relay")
	}
}
