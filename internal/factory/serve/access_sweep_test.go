package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

func contractGrantExpiry(t *testing.T, o *wsOrg, path, subject string, expiry time.Time) acl.Grant {
	t.Helper()
	body := personGrant(subject, "view")
	body.ExpiresAt = &expiry
	var grant acl.Grant
	o.do(t, "santosh", "POST", path+"/grants", -1, body, 201, &grant)
	return grant
}

func contractSweep(t *testing.T, o *wsOrg) {
	t.Helper()
	if err := o.h.sweepGrants(o.now); err != nil {
		t.Fatal(err)
	}
}

func TestContractL1SweepCoalescesLapsesAndTellsTheirAudienceOnce(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	chat := contractChat(t, o, ws, "")
	path := "/v1/resources/chat/" + chat.ID
	contractGrantExpiry(t, o, path, "abir", o.now.Add(time.Minute))
	contractGrantExpiry(t, o, path, "mia", o.now.Add(2*time.Minute))
	before := len(contractResourceEvents(t, o, chat.ID))
	o.now = o.now.Add(3 * time.Minute)
	contractSweep(t, o)
	after := contractResourceEvents(t, o, chat.ID)
	// CONTRACT CHANGE (org-follow A4): a chat shared with two people is still
	// private, so the operator is not told of its lapses either.
	if len(after) != before+1 || !after[before].itemAudience.Equal(acl.Audience{
		Persons: []string{"abir", "mia", "santosh"},
	}) {
		t.Fatalf("lapse events before %d, after %+v", before, after)
	}
	contractSweep(t, o)
	if got := len(contractResourceEvents(t, o, chat.ID)); got != len(after) {
		t.Fatalf("another event was emitted: got %d, want %d", got, len(after))
	}
}

func TestContractL2StartupCatchesDowntimeAndSkipsRemovedGrants(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	chat := contractChat(t, o, ws, "")
	removed := contractChat(t, o, ws, "")
	expiry := o.now.Add(time.Minute)
	contractGrantExpiry(t, o, "/v1/resources/chat/"+chat.ID, "abir", expiry)
	path := "/v1/resources/chat/" + removed.ID
	g := contractGrantExpiry(t, o, path, "mia", expiry)
	o.do(t, "santosh", "DELETE", path+"/grants/"+g.ID, -1, nil, 204, nil)
	contractSweep(t, o)
	o.h.Close()
	o.now = expiry.Add(time.Minute)
	restartAccessOrg(t, o)
	events := contractResourceEvents(t, o, chat.ID)
	if len(events) != 2 || !slices.Contains(events[1].Persons, "abir") {
		t.Fatalf("startup events: %+v", events)
	}
	if events := contractResourceEvents(t, o, removed.ID); len(events) != 2 {
		t.Fatalf("removed grant emitted a lapse event: %+v", events)
	}
	restartAccessOrg(t, o)
	if events := contractResourceEvents(t, o, chat.ID); len(events) != 2 {
		t.Fatalf("startup repeated a lapse event: %+v", events)
	}
}

func TestContractL3ExpiryUsesEachResourcesExistingEvent(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	const canvas = "cv_demo01234567"
	if _, err := o.st.EditCanvases(func(all factory.OrgCanvases) (bool, error) {
		all[canvas] = factory.OrgCanvas{ID: canvas, Owner: factory.WorkstreamOwner{Kind: "person", ID: "santosh"}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	chat := contractChat(t, o, ws, "")
	expiry := o.now.Add(time.Minute)
	for _, path := range []string{accessWorkstreamPath(ws), "/v1/resources/canvas/" + canvas, "/v1/resources/chat/" + chat.ID} {
		contractGrantExpiry(t, o, path, "abir", expiry)
	}
	prev, _, err := live.Read(o.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	o.now = expiry
	contractSweep(t, o)
	events, _, err := live.Read(o.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range events[len(prev):] {
		var body struct {
			ID string `json:"id"`
			acl.Audience
		}
		if err := json.Unmarshal(e.Data, &body); err != nil || !slices.Contains(body.Persons, "abir") {
			t.Fatalf("wire event: %s, %v", e.Data, err)
		}
		got[body.ID] = e.Kind
	}
	if len(events)-len(prev) != 3 || got[ws] != api.EventWorkstreamChanged ||
		got[canvas] != api.EventCanvasChanged || got[chat.ID] != eventResourceChanged {
		t.Fatalf("lapse events: %+v", got)
	}
}

func TestContractL4SweepKeepsRowsAndDoesNotTouchOtherGrants(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	chat := contractChat(t, o, ws, "")
	path := "/v1/resources/chat/" + chat.ID
	expiry := o.now.Add(time.Minute)
	expired := contractGrantExpiry(t, o, path, "abir", expiry)
	future := contractGrantExpiry(t, o, path, "mia", expiry.Add(time.Minute))
	var permanent acl.Grant
	o.do(t, "santosh", "POST", path+"/grants", -1, personGrant("santosh", "view"), 201, &permanent)
	other := contractChat(t, o, ws, "")
	o.do(t, "santosh", "POST", "/v1/resources/chat/"+other.ID+"/grants", -1, personGrant("mia", "view"), 201, nil)
	o.now = expiry
	contractSweep(t, o)
	rows, err := o.st.GrantsOn("chat", chat.ID)
	if err != nil || len(rows) != 3 || !slices.ContainsFunc(rows, func(g acl.Grant) bool { return g.ID == expired.ID && !g.Live(o.now) }) {
		t.Fatalf("rows = %+v, err %v", rows, err)
	}
	var listed api.ResourceGrants
	o.do(t, "santosh", "GET", path+"/grants", -1, nil, 200, &listed)
	for id, want := range map[string]bool{expired.ID: false, future.ID: true, permanent.ID: true} {
		if found := slices.ContainsFunc(listed.Grants, func(g api.GrantTuple) bool { return g.ID == id && g.Live }); found != want {
			t.Fatalf("grant %s live = %v, list %+v", id, found, listed)
		}
	}
	if events := contractResourceEvents(t, o, other.ID); len(events) != 1 {
		t.Fatalf("non-expiring grant unexpectedly announced: %+v", events)
	}
}

func TestContractL5CloseStopsTheGrantSweepTimer(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	contractGrantExpiry(t, o, accessWorkstreamPath(ws), "abir", o.now.Add(time.Minute))
	s := &o.h.grantSweeper
	s.mu.Lock()
	timer := s.timer
	s.mu.Unlock()
	if timer == nil {
		t.Fatal("grant sweep has no armed timer")
	}
	o.h.Close()
	o.h.scheduleGrantSweep()
	s.mu.Lock()
	running := s.timer != nil || !s.closed
	s.mu.Unlock()
	if running || timer.Stop() {
		t.Fatal("Close left a grant sweep timer live")
	}
}

func TestContractL6AccessExpiresBeforeSweepAndWatermarkOnlyFollowsEvents(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	chat := contractChat(t, o, ws, "")
	path := "/v1/resources/chat/" + chat.ID
	now := o.now
	for _, notFuture := range []time.Time{now, now.Add(-time.Second)} {
		g := personGrant("abir", "view")
		g.ExpiresAt = &notFuture
		w := o.raw(t, "santosh", "POST", path+"/grants", g)
		if w.Code != 422 || !strings.Contains(w.Body.String(), "an expiry must be later than now") {
			t.Fatalf("past expiry = %d %s", w.Code, w.Body)
		}
	}
	expiry := now.Add(time.Minute + time.Nanosecond)
	contractGrantExpiry(t, o, path, "abir", expiry)
	o.do(t, "abir", "GET", path+"/transcript", -1, nil, 200, nil)
	contractSweep(t, o)
	o.now = expiry
	o.do(t, "abir", "GET", path+"/transcript", -1, nil, 404, nil)
	o.now = expiry.Truncate(time.Millisecond).Add(time.Millisecond)
	before, err := o.st.GrantSweepWatermark()
	if err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(o.st.Root(), live.File)
	backup := eventPath + ".contract-backup"
	if err := os.Rename(eventPath, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(eventPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := o.h.sweepGrants(o.now); err == nil {
		t.Fatal("sweep reported success even though the event could not be written")
	}
	if mark, err := o.st.GrantSweepWatermark(); err != nil || mark != before {
		t.Fatalf("watermark changed after failed emission: %d, %v", mark, err)
	}
	if err := os.Remove(eventPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, eventPath); err != nil {
		t.Fatal(err)
	}
	contractSweep(t, o)
	if events := contractResourceEvents(t, o, chat.ID); len(events) != 2 || !slices.Contains(events[1].Persons, "abir") {
		t.Fatalf("lapse did not reach its earlier audience: %+v", events)
	}
	want := o.now.UnixMilli()
	o.now = now
	contractSweep(t, o)
	if mark, err := o.st.GrantSweepWatermark(); err != nil || mark != want {
		t.Fatalf("watermark after time moved backward = %d, %v, want %d", mark, err, want)
	}
}
