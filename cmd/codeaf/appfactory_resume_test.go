package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── the local factory survives the app closing ─────────────────────────────

// A second engine over the same app home and registry file: the app opened
// again. It shares the registry's folder and nothing in memory.
func reopened(t *testing.T, hub *appFactoryHub) (*factoryWire, *setupGitHub) {
	t.Helper()
	again := newAppFactoryHub()
	dir := hub.dir()
	again.dir = func() string { return dir }
	w := newFactoryWire(t, again)
	gh := &setupGitHub{}
	offlineLocal(w.backend, gh, t)
	return w, gh
}

// attached saves rows a person attached by address, as the app does after it
// has read the registry.
func attached(t *testing.T, hub *appFactoryHub, rows ...appFactoryRow) {
	t.Helper()
	if _, err := hub.snapshot(); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if err := hub.saveAttached(row, false); err != nil {
			t.Fatal(err)
		}
	}
}

func jobIDs(q api.Queue) []int {
	out := make([]int, len(q.Jobs))
	for i, j := range q.Jobs {
		out[i] = j.ID
	}
	return out
}

// THE REGRESSION: a person finished setup, quit, and opened the app again, and
// "no factory yet" was all it showed. Real loopback services, two lifecycles,
// no fake launch: the connection id, the queue and the checkout are the same,
// the address and bearer are this launch's, and nothing ran.
func TestLocalFactoryComesBackQuietlyAfterTheAppRestarts(t *testing.T) {
	s := newLocalSetup(t)
	remote := appFactoryRow{ID: appFactoryID("https://team.example"), URL: "https://team.example", Name: "Team", Mode: "team"}
	solo := appFactoryRow{ID: appFactoryID("https://solo.example"), URL: "https://solo.example", Name: "Solo elsewhere", Mode: "solo"}
	attached(t, s.backend.factoryHub(), remote, solo)
	st := s.start()
	s.setup("factory.setup_connect", map[string]any{"source": "token", "token": setupSecret})
	s.setup("factory.setup_select", map[string]any{"repos": []string{"acme/api"}})
	s.until("checkouts", func(v appwire.FactorySetupView) bool { return v.State == "checkouts" })
	gitCheckout(t, filepath.Join(s.userHm, "code", "acme-api"), "acme/api")
	s.setup("factory.setup_clone", map[string]any{"repo": "acme/api", "folder": "~/code/acme-api"})
	s.until("api found", func(v appwire.FactorySetupView) bool { return repoOf(v, "acme/api").Clone == "found" })
	s.setup("factory.setup_done", map[string]any{})
	var before api.Queue
	s.must("factory.queue", map[string]any{"factory": st.Factory}, &before)
	if len(before.Jobs) != 2 {
		t.Fatalf("queue before: %+v", before.Jobs)
	}
	firstToken := s.backend.local.host.token

	// The app quits: the service, its address and its bearer are gone.
	s.backend.Close()
	if _, err := os.Stat(filepath.Join(v3FactoryRoot(), "serve.json")); err == nil {
		t.Fatal("serve.json survived the engine closing")
	}

	// The app opens again. Nothing is asked for but the list.
	w, gh := reopened(t, s.backend.factoryHub())
	if w.backend.local.running {
		t.Fatal("the factory was served before anything asked for the app's list")
	}
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	states := map[string]appwire.FactoryConnection{}
	for _, c := range list.Factories {
		states[c.ID] = c
	}
	own, ok := states[appLocalID]
	if !ok || own.State != appwire.FactoryOK || own.Mode != "solo" || len(list.Factories) != 3 {
		t.Fatalf("list after restart: %+v", list.Factories)
	}
	if states[remote.ID].URL != remote.URL || states[solo.ID].URL != solo.URL {
		t.Fatalf("a remote attachment changed: %+v", list.Factories)
	}
	if st.Factory != appLocalID {
		t.Fatalf("the id was %q", st.Factory)
	}
	// The same floor: the same jobs, under the same ids, and the same checkout.
	var after api.Queue
	w.must("factory.queue", map[string]any{"factory": appLocalID}, &after)
	if len(after.Jobs) != len(before.Jobs) {
		t.Fatalf("queue after: %+v", after.Jobs)
	}
	for i, id := range jobIDs(before) {
		if jobIDs(after)[i] != id || after.Jobs[i].State != "backlog" {
			t.Fatalf("jobs changed: %v -> %v", jobIDs(before), jobIDs(after))
		}
	}
	var status appwire.FactoryLocalStatus
	w.must("factory.local_status", map[string]any{"caller": "w1"}, &status)
	if status.State != appwire.LocalRunning || status.Authority != appwire.LocalAuthorityApp || status.Factory != appLocalID || !status.Runner {
		t.Fatalf("status after restart: %+v", status)
	}
	host := w.backend.local.host
	if host == nil || !host.quiet || host.token == "" || host.token == firstToken {
		t.Fatal("the resumed service is not quiet, or kept the last launch's bearer")
	}
	setup := func() appwire.FactorySetupView {
		var v appwire.FactorySetupView
		w.must("factory.setup_read", map[string]any{"caller": "w1"}, &v)
		return v
	}
	if r := repoOf(setup(), "acme/api"); r.Clone != "found" || r.Folder != "~/code/acme-api" {
		t.Fatalf("the checkout was not kept: %+v", r)
	}

	// THE RUNNER IS BACK AND NOTHING RAN: every job is still in Backlog, and not
	// one request reached GitHub (the credential does not even exist in the new
	// process, so the poll waits for one).
	var held api.Queue
	w.must("factory.queue", map[string]any{"factory": appLocalID}, &held)
	for _, j := range held.Jobs {
		if j.State != "backlog" {
			t.Fatalf("restart started job %d: %q", j.ID, j.State)
		}
	}
	if seen := gh.seen(); len(seen) != 0 {
		t.Fatalf("restart made GitHub requests: %v", seen)
	}
	data, _ := os.ReadFile(w.backend.factoryHub().path())
	for _, secret := range []string{host.token, firstToken, setupSecret} {
		if secret != "" && strings.Contains(string(data), secret) {
			t.Fatal("the registry file holds a secret")
		}
	}
	w.neverSaid(host.token, firstToken, setupSecret)
}

// A PROFILE THAT NEVER ASKED STAYS UNSTARTED, however many times it lists.
func TestLocalFactoryIsNotResumedForAProfileThatNeverStartedIt(t *testing.T) {
	s := newLocalSetup(t)
	other := appFactoryRow{ID: appFactoryID("https://team.example"), URL: "https://team.example", Mode: "team"}
	attached(t, s.backend.factoryHub(), other)
	var list appwire.FactoryList
	s.must("factory.list", struct{}{}, &list)
	var st appwire.FactoryLocalStatus
	s.must("factory.local_status", map[string]any{"caller": "w1"}, &st)
	if len(list.Factories) != 1 || st.State != appwire.LocalStopped || s.backend.local.running || s.backend.local.server != nil {
		t.Fatalf("list %+v status %+v", list.Factories, st)
	}
	s.mustFail("factory.queue", map[string]any{"factory": appLocalID}, appwire.CodeNotFound)
	if _, err := os.Stat(filepath.Join(v3FactoryRoot(), "serve.json")); err == nil {
		t.Fatal("a factory was served for a profile that never started one")
	}
}

// A ROW FROM A RELEASE BEFORE THIS CHOICE EXISTED IS NOT A CHOICE: it carries no
// Resume mark, so it is taken as the local row but starts nothing.
func TestAnOlderLocalRowIsNotMistakenForTheChoiceToResume(t *testing.T) {
	s := newLocalSetup(t)
	hub := s.backend.factoryHub()
	old := appFactoryRow{ID: appFactoryID("http://127.0.0.1:7"), URL: "http://127.0.0.1:7", Name: appLocalName, Mode: "solo", Local: true}
	hub.mu.Lock()
	hub.rows, hub.loaded = []appFactoryRow{old}, true
	err := hub.save()
	hub.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	w, _ := reopened(t, hub)
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	if w.backend.local.running || len(list.Factories) != 1 || list.Factories[0].ID != appLocalID || list.Factories[0].State == appwire.FactoryOK {
		t.Fatalf("an old row resumed: %+v", list.Factories)
	}
}

// DETACHING THE ROW ENDS THE CHOICE. Whether the service is up when it is
// detached or the app has already quit, the next launch serves nothing.
func TestDetachingTheLocalFactoryStopsItResuming(t *testing.T) {
	s := newLocalSetup(t)
	s.start()
	s.must("factory.detach", map[string]any{"factory": appLocalID}, &struct{}{})
	s.backend.Close()
	w, _ := reopened(t, s.backend.factoryHub())
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	if len(list.Factories) != 0 || w.backend.local.running {
		t.Fatalf("a detached factory came back: %+v", list.Factories)
	}

	// Started, quit, then detached while the app is closed: the same.
	w2 := newFactoryWire(t, w.backend.factoryHub())
	offlineLocal(w2.backend, &setupGitHub{}, t)
	var st appwire.FactoryLocalStatus
	w2.must("factory.local_start", map[string]any{"caller": "w1"}, &st)
	w2.backend.Close()
	w3, _ := reopened(t, w2.backend.factoryHub())
	w3.must("factory.detach", map[string]any{"factory": appLocalID}, &struct{}{})
	w3.must("factory.list", struct{}{}, &list)
	if len(list.Factories) != 0 || w3.backend.local.running {
		t.Fatalf("a factory detached while closed came back: %+v", list.Factories)
	}
}

// ANOTHER CODEAF'S SERVICE IS NEVER DUPLICATED OR SILENTLY JOINED BY A RESUME.
// The election stays theirs; a person's own local_start still joins it.
func TestResumeNeverStartsASecondServiceOrJoinsAnotherAuthority(t *testing.T) {
	s := newLocalSetup(t)
	s.start()
	s.backend.Close()
	other := newFactoryWire(t, testFactoryHub(t))
	offlineLocal(other.backend, &setupGitHub{}, t)
	var first appwire.FactoryLocalStatus
	other.must("factory.local_start", map[string]any{"caller": "o"}, &first)
	if first.Authority != appwire.LocalAuthorityApp {
		t.Fatalf("other: %+v", first)
	}

	w, _ := reopened(t, s.backend.factoryHub())
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	if w.backend.local.running || w.backend.local.server != nil || w.backend.local.host != nil {
		t.Fatal("a resume served or joined while another codeaf holds the factory")
	}
	if target, ok := liveFactoryDiscovery(); !ok || target.url != first.URL {
		t.Fatal("the other codeaf's service was disturbed")
	}
	if len(list.Factories) != 1 || list.Factories[0].State == appwire.FactoryOK {
		t.Fatalf("list: %+v", list.Factories)
	}
	var st appwire.FactoryLocalStatus
	w.must("factory.local_start", map[string]any{"caller": "w1"}, &st)
	if st.Authority != appwire.LocalAuthorityOther || w.backend.local.host != nil {
		t.Fatalf("explicit start: %+v", st)
	}
}

// JOINING ANOTHER CODEAF IS NOT HOSTING. The row it leaves is the logical local
// row, kept at quit and unmarked, so the next launch lists it unreachable,
// serves nothing, and a detach removes it. The bearer of the joined service is
// never kept.
func TestAJoinedOtherCodafRowIsKeptButNeverResumedOrAdopted(t *testing.T) {
	s := newLocalSetup(t)
	other := newFactoryWire(t, testFactoryHub(t))
	offlineLocal(other.backend, &setupGitHub{}, t)
	var first appwire.FactoryLocalStatus
	other.must("factory.local_start", map[string]any{"caller": "o"}, &first)
	var st appwire.FactoryLocalStatus
	s.must("factory.local_start", map[string]any{"caller": "w1"}, &st)
	if st.Authority != appwire.LocalAuthorityOther || st.Factory != appLocalID {
		t.Fatalf("join: %+v", st)
	}
	hub := s.backend.factoryHub()
	rows, _ := hub.snapshot()
	if len(rows) != 1 || !rows[0].Local || rows[0].Resume {
		t.Fatalf("joined row: %+v", rows)
	}
	s.backend.Close()
	if hub.tokenFor(appLocalID) != "" {
		t.Fatal("the joined bearer outlived the engine")
	}
	data, _ := os.ReadFile(hub.path())
	if strings.Contains(string(data), "bearer") || strings.Contains(string(data), "token") {
		t.Fatalf("registry holds a credential: %s", data)
	}
	other.backend.Close()

	w, _ := reopened(t, hub)
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	if w.backend.local.running || len(list.Factories) != 1 || list.Factories[0].ID != appLocalID || list.Factories[0].State == appwire.FactoryOK {
		t.Fatalf("joined row resumed or lost: %+v", list.Factories)
	}
	w.must("factory.detach", map[string]any{"factory": appLocalID}, &struct{}{})
	w.must("factory.list", struct{}{}, &list)
	if len(list.Factories) != 0 {
		t.Fatalf("detach left %+v", list.Factories)
	}
}

// A legacy registry may hold the local row beside a hand-attached row at the
// same address: only our local marker can be discarded; the manual row survives.
func TestLoadMakesOneRowPerOriginAndNeverInventsResume(t *testing.T) {
	hub := testFactoryHub(t)
	const origin = "http://127.0.0.1:7"
	hub.mu.Lock()
	hub.rows, hub.loaded = []appFactoryRow{
		{ID: appFactoryID(origin), URL: origin, Name: "dup", Mode: "team"},
		{ID: appFactoryID(origin), URL: origin, Name: appLocalName, Mode: "solo", Local: true, Resume: false},
		{ID: appFactoryID("https://team.example"), URL: "https://team.example", Mode: "team", Resume: true},
	}, true
	err := hub.save()
	hub.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	again := newAppFactoryHub()
	dir := hub.dir()
	again.dir = func() string { return dir }
	rows, err := again.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows: %+v", rows)
	}
	for _, r := range rows {
		if r.Resume || (r.Local && r.ID != appLocalID) || (!r.Local && r.ID == appLocalID) {
			t.Fatalf("row: %+v", r)
		}
	}
	var manual bool
	for _, row := range rows {
		manual = manual || (row.ID == appFactoryID(origin) && row.URL == origin && row.Name == "dup" && row.Mode == "team" && !row.Local)
	}
	if !manual {
		t.Fatal("loading discarded or changed the manually attached connection")
	}
}

func TestJoiningAnAlreadyAttachedLocalAuthorityKeepsItsManualConnection(t *testing.T) {
	useLocalHome(t)
	owner := newFactoryWire(t, testFactoryHub(t))
	offlineLocal(owner.backend, &setupGitHub{}, t)
	var first appwire.FactoryLocalStatus
	owner.must("factory.local_start", map[string]any{"caller": "owner"}, &first)
	hub := testFactoryHub(t)
	manual := appFactoryRow{ID: appFactoryID(first.URL), URL: first.URL, Name: "My attached factory", Mode: "solo"}
	attached(t, hub, manual)
	joiner := newFactoryWire(t, hub)
	var second appwire.FactoryLocalStatus
	joiner.must("factory.local_start", map[string]any{"caller": "joiner"}, &second)
	if second.Authority != appwire.LocalAuthorityOther || second.Factory != manual.ID {
		t.Fatalf("joined authority: %+v", second)
	}
	joiner.backend.Close()
	rows, err := hub.snapshot()
	if err != nil || len(rows) != 1 || rows[0] != manual {
		t.Fatalf("manual connection changed: %+v %v", rows, err)
	}
}

func TestOwnedLocalAddressCannotOverwriteAManuallyAttachedRow(t *testing.T) {
	hub := testFactoryHub(t)
	const origin = "http://127.0.0.1:7543"
	manual := appFactoryRow{ID: appFactoryID(origin), URL: origin, Name: "My attached factory", Mode: "team"}
	attached(t, hub, manual)
	err := hub.showLocal(appLocalID, origin, true)
	if err == nil {
		t.Fatal("owned start overwrote a manually attached address")
	}
	rows, readErr := hub.snapshot()
	if readErr != nil || len(rows) != 1 || rows[0] != manual {
		t.Fatalf("manual connection changed: %+v %v", rows, readErr)
	}
}

func TestLocalStartAfterDetachRestoresAuthenticatedReads(t *testing.T) {
	s := newLocalSetup(t)
	before := s.start()
	s.must("factory.detach", map[string]any{"factory": before.Factory}, &struct{}{})
	after := s.start()
	if after.Factory != before.Factory {
		t.Fatalf("local identity changed after reconnect: %q -> %q", before.Factory, after.Factory)
	}
	var queue api.Queue
	s.must("factory.queue", map[string]any{"factory": after.Factory}, &queue)
	if s.backend.factoryHub().tokenFor(after.Factory) == "" {
		t.Fatal("reconnect did not restore its held bearer")
	}
}

func TestJoiningAListedLocalOriginUsesTheVerifiedCallersSession(t *testing.T) {
	stub, _ := accessStub(t)
	hub := testFactoryHub(t)
	local := appFactoryRow{ID: appLocalID, URL: stub.srv.URL, Name: "Nearby factory", Mode: "team", Local: true}
	attached(t, hub, local)
	const prior = "local-operator-fixture-bearer-credential"
	hub.hold(appLocalID, prior, 0)
	w := newFactoryWire(t, hub)
	params := map[string]any{"caller": "window-a", "link": "codeaf://factory/join?url=" + stub.srv.URL + "&code=invite-fixture-secret"}
	var access appwire.FactoryAccess
	w.must("factory.access_check", params, &access)
	w.must("factory.access_sign_in", params, &access)
	w.must("factory.access_check", params, &access)
	if access.State != "joined" || access.Factory.ID != appLocalID {
		t.Fatalf("join changed the logical factory: %+v", access)
	}
	token := hub.tokenFor(appLocalID)
	if token == prior || !strings.HasPrefix(token, "session-grant-secret-") {
		t.Fatal("the verified caller's grant did not replace the earlier operator credential")
	}
	rows, err := hub.snapshot()
	if err != nil || len(rows) != 1 || rows[0] != local {
		t.Fatalf("join changed or duplicated the saved connection: %+v %v", rows, err)
	}
	for _, line := range w.lines {
		if strings.Contains(line, token) {
			t.Fatal("the completed caller grant reached the app wire")
		}
	}
}
