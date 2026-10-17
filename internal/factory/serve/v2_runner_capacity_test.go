package serve

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// capacityRig is a team factory with two signed-in owners.
type capacityRig struct {
	*peopleV2Fixture
	priya, raj string
}

func capacitySetup(t *testing.T) capacityRig {
	t.Helper()
	f := peopleV2Setup(t, api.FactoryTeam)
	g := &fakeGitHub{t: t}
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubHTTPClient = g.client()
	sign := func(login string) string {
		var created struct {
			Link api.AccessLink `json:"link"`
		}
		f.request(t, "POST", "/v1/access/links", "operator-secret", api.CreateAccessLink{Login: login, Name: login, ExpiresAt: f.now.Add(time.Hour)}, 201, &created)
		parsed, err := url.Parse(created.Link.URL)
		if err != nil {
			t.Fatal(err)
		}
		fragment, _ := url.ParseQuery(parsed.Fragment)
		return signInHandle(t, f.h, g, &f.now, login, fragment.Get("token"))
	}
	return capacityRig{peopleV2Fixture: f, priya: sign("priya"), raj: sign("raj")}
}

func (c capacityRig) attach(t *testing.T, token, owner, machine string, slots int) api.RunnerAttached {
	t.Helper()
	var out api.RunnerAttached
	c.request(t, "POST", "/v1/runners/attach", token, api.RunnerAttach{Runner: machine, Name: machine, Owner: owner, Kind: api.RunnerMac, Slots: slots, Checkouts: []api.Checkout{{Repo: "acme/api", Path: "/code/api"}}, TakesUnassigned: map[string]bool{"*": true}}, 201, &out)
	return out
}

func (c capacityRig) runner(t *testing.T, id string) api.Runner {
	t.Helper()
	var list api.Runners
	c.request(t, "GET", "/v1/runners", "operator-secret", nil, 200, &list)
	for _, r := range list.Runners {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("runner %q is not listed: %+v", id, list)
	return api.Runner{}
}

func TestRunnerSlotsRaiseAndLowerFollowTheOwnersSavedNumber(t *testing.T) {
	c := capacitySetup(t)
	var creds [3]api.RunnerAttached
	for i := range creds {
		creds[i] = c.attach(t, c.priya, "priya", "mbp", 0)
		c.add(t, "priya")
	}
	if creds[0].Slots != api.DefaultRunnerSlots {
		t.Fatalf("attach answered %d slots, want the default %d", creds[0].Slots, api.DefaultRunnerSlots)
	}
	var pulls [3]api.RunnerPull
	for i := 0; i < 2; i++ {
		c.request(t, "GET", "/v1/runners/pull", creds[i].Credential, nil, 200, &pulls[i])
	}
	c.request(t, "GET", "/v1/runners/pull", creds[2].Credential, nil, 204, nil)
	// Raising lets the third lease out at once.
	c.request(t, "PUT", "/v1/runners/mbp", c.priya, api.RunnerSettings{Slots: 3}, 200, nil)
	c.request(t, "GET", "/v1/runners/pull", creds[2].Credential, nil, 200, &pulls[2])
	// Lowering cancels nothing: every held lease keeps renewing and stays listed.
	c.request(t, "PUT", "/v1/runners/mbp", c.priya, api.RunnerSettings{Slots: 1}, 200, nil)
	for i := range pulls {
		c.request(t, "POST", "/v1/runners/leases/"+pulls[i].Lease.ID+"/heartbeat", creds[i].Credential, struct{}{}, 200, nil)
	}
	if got := c.runner(t, "mbp"); got.Slots != 1 || len(got.Jobs) != 3 {
		t.Fatalf("after lowering: %+v", got)
	}
	// And no fourth is handed out while three are still over the cap.
	c.add(t, "priya")
	c.request(t, "GET", "/v1/runners/pull", creds[0].Credential, nil, 204, nil)
}

func TestRunnerSlotsBoundedAndCheckoutsKept(t *testing.T) {
	c := capacitySetup(t)
	c.attach(t, c.priya, "priya", "mbp", 0)
	for _, slots := range []int{0, -1, api.MaxRunnerSlots + 1} {
		c.request(t, "PUT", "/v1/runners/mbp", c.priya, api.RunnerSettings{Slots: slots}, 422, nil)
	}
	c.request(t, "PUT", "/v1/runners/mbp", c.priya, api.RunnerSettings{Slots: api.MaxRunnerSlots}, 200, nil)
	// A body that sends no checkouts at all changes capacity and nothing else.
	c.request(t, "PUT", "/v1/runners/mbp", c.priya, map[string]any{"slots": 3}, 200, nil)
	got := c.runner(t, "mbp")
	if got.Slots != 3 || len(got.Checkouts) != 1 || got.Checkouts[0].Path != "/code/api" {
		t.Fatalf("slots-only PUT: %+v", got)
	}
	c.request(t, "PUT", "/v1/runners/mbp", c.priya, map[string]any{"slots": 3, "checkouts": []api.Checkout{{Repo: "acme/web", Path: "/code/web"}}}, 200, nil)
	if got := c.runner(t, "mbp"); len(got.Checkouts) != 1 || got.Checkouts[0].Repo != "acme/web" {
		t.Fatalf("sent checkouts: %+v", got)
	}
	c.request(t, "PUT", "/v1/runners/mbp", c.priya, map[string]any{"slots": 3, "checkouts": []api.Checkout{}}, 200, nil)
	if got := c.runner(t, "mbp"); len(got.Checkouts) != 0 {
		t.Fatalf("an empty list clears: %+v", got)
	}
}

func TestRunnerSlotsAreOwnerOnly(t *testing.T) {
	c := capacitySetup(t)
	c.attach(t, c.priya, "priya", "mbp", 0)
	for name, token := range map[string]string{"another owner": c.raj, "the operator": "operator-secret"} {
		body := c.request(t, "PUT", "/v1/runners/mbp", token, api.RunnerSettings{Slots: 5}, 404, nil)
		if want := `"runner not found"`; !strings.Contains(body, want) {
			t.Fatalf("%s: %s", name, body)
		}
	}
	if got := c.runner(t, "mbp"); got.Slots != api.DefaultRunnerSlots {
		t.Fatalf("a refused PUT changed capacity: %+v", got)
	}
}

func TestRunnerSelfReadsOnlyTheCallersOwnCapacityWithItsWorkerCredential(t *testing.T) {
	c := capacitySetup(t)
	mine := c.attach(t, c.priya, "priya", "mbp", 0)
	theirs := c.attach(t, c.raj, "raj", "studio", 6)
	c.request(t, "PUT", "/v1/runners/mbp", c.priya, api.RunnerSettings{Slots: 4}, 200, nil)
	raw := c.request(t, "GET", "/v1/runner/self", mine.Credential, nil, 200, nil)
	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 || fields["slots"] != float64(4) {
		t.Fatalf("self body names more than its own capacity: %s", raw)
	}
	if raw := c.request(t, "GET", "/v1/runner/self", theirs.Credential, nil, 200, nil); !strings.Contains(raw, `"slots":6`) {
		t.Fatalf("another runner reads its own number: %s", raw)
	}
	// A person's token, the operator's token and no token are not worker credentials.
	for _, token := range []string{c.priya, "operator-secret", ""} {
		c.request(t, "GET", "/v1/runner/self", token, nil, 401, nil)
	}
}

func TestSavedSlotsSurviveRestartAndReattach(t *testing.T) {
	c := capacitySetup(t)
	first := c.attach(t, c.priya, "priya", "mbp", 0)
	c.request(t, "PUT", "/v1/runners/mbp", c.priya, api.RunnerSettings{Slots: 5}, 200, nil)
	// A restarted worker still pinned to its environment value asks for 2 and
	// is told the saved 5; one pinned above the ceiling is clamped, not refused.
	if again := c.attach(t, c.priya, "priya", "mbp", 2); again.Slots != 5 {
		t.Fatalf("reattach answered %d, want the saved 5", again.Slots)
	}
	if fresh := c.attach(t, c.priya, "priya", "laptop", 99); fresh.Slots != api.MaxRunnerSlots {
		t.Fatalf("an over-ceiling attach answered %d, want %d", fresh.Slots, api.MaxRunnerSlots)
	}
	reopened := New(c.seam, c.cfg)
	defer reopened.Close()
	w := identityRequest(reopened, "GET", "/v1/runner/self", "", first.Credential, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"slots":5`) {
		t.Fatalf("after restart: %d %s", w.Code, w.Body)
	}
	if registry, err := os.ReadFile(filepath.Join(c.st.Root(), "runners.json")); err != nil || strings.Contains(string(registry), first.Credential) {
		t.Fatal("registry missing or exposed a credential")
	}
}

func TestAnOlderWorkerSendingNullCheckoutsAndNoSlotsFieldStillWorks(t *testing.T) {
	c := capacitySetup(t)
	c.attach(t, c.priya, "priya", "mbp", 0)
	// The pre-change client always sent checkouts; the body below is its shape.
	c.request(t, "PUT", "/v1/runners/mbp", c.priya, map[string]any{"slots": 2, "checkouts": []api.Checkout{{Repo: "acme/api", Path: "/code/api"}}}, 200, nil)
	if got := c.runner(t, "mbp"); got.Slots != 2 || len(got.Checkouts) != 1 {
		t.Fatalf("%+v", got)
	}
}

// slotsToLegacy rewrites every saved slots number in the registry to n, the
// way a row written before the ceiling existed would read.
func slotsToLegacy(t *testing.T, root string, n int) {
	t.Helper()
	path := filepath.Join(root, "runners.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows map[string]map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		row["Slots"] = n
		if w, ok := row["Worker"].(map[string]any); ok {
			for k := range w {
				if strings.EqualFold(k, "slots") {
					w[k] = n
				}
			}
		}
	}
	data, _ = json.Marshal(rows)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A runner saved before the ceiling existed may hold 20. After a restart the
// roster, the runner's own read and a reattach must all say the capacity that
// is really granted (the ceiling), and a lease already held is not cancelled.
func TestALegacySavedCapacityAboveTheCeilingIsGrantedTheCeilingEverywhere(t *testing.T) {
	c := capacitySetup(t)
	first := c.attach(t, c.priya, "priya", "mbp", 0)
	c.add(t, "priya")
	var held api.RunnerPull
	c.request(t, "GET", "/v1/runners/pull", first.Credential, nil, 200, &held)
	c.h.Close()
	slotsToLegacy(t, c.st.Root(), 20)

	reopened := New(c.seam, c.cfg)
	defer reopened.Close()
	want := fmt.Sprintf(`"slots":%d`, api.MaxRunnerSlots)
	if w := identityRequest(reopened, "GET", "/v1/runner/self", "", first.Credential, nil); w.Code != 200 || !strings.Contains(w.Body.String(), want) {
		t.Fatalf("self after restart: %d %s, want %s", w.Code, w.Body, want)
	}
	if w := identityRequest(reopened, "GET", "/v1/runners", "", "operator-secret", nil); w.Code != 200 || !strings.Contains(w.Body.String(), want) || strings.Contains(w.Body.String(), `"slots":20`) {
		t.Fatalf("roster after restart: %d %s", w.Code, w.Body)
	}
	if w := identityRequest(reopened, "POST", "/v1/runners/leases/"+held.Lease.ID+"/heartbeat", "{}", first.Credential, nil); w.Code != 200 {
		t.Fatalf("a lease held across the migration was lost: %d %s", w.Code, w.Body)
	}
	var again api.RunnerAttached
	body, _ := json.Marshal(api.RunnerAttach{Runner: "mbp", Name: "mbp", Owner: "priya", Kind: api.RunnerMac, Slots: 20})
	w := identityRequest(reopened, "POST", "/v1/runners/attach", string(body), c.priya, nil)
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &again) != nil || again.Slots != api.MaxRunnerSlots {
		t.Fatalf("reattach: %d %s", w.Code, w.Body)
	}
}

func TestOnlyAWorkerThatSaysItFollowsCapacityIsListedAsLive(t *testing.T) {
	c := capacitySetup(t)
	c.attach(t, c.priya, "priya", "old-box", 0)
	var out api.RunnerAttached
	c.request(t, "POST", "/v1/runners/attach", c.priya, api.RunnerAttach{Runner: "new-box", Name: "new-box", Owner: "priya", Kind: api.RunnerMac, LiveCapacity: true}, 201, &out)
	if c.runner(t, "old-box").LiveCapacity || !c.runner(t, "new-box").LiveCapacity {
		t.Fatal("live_capacity must be true only for the worker that attested it")
	}
	if w := identityRequest(c.h, "GET", "/v1/runners", "", "operator-secret", nil); strings.Count(w.Body.String(), "live_capacity") != 1 {
		t.Fatalf("an unattested runner must not carry the field: %s", w.Body)
	}
}
