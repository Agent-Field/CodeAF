package serve

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// beat is one presence heartbeat from who, answered with who is here.
func (o *wsOrg) beat(t *testing.T, who string, body map[string]any, status int) api.Presence {
	t.Helper()
	var out api.Presence
	if status != 200 {
		o.do(t, who, "POST", "/v1/presence", -1, body, status, nil)
		return out
	}
	o.do(t, who, "POST", "/v1/presence", -1, body, status, &out)
	return out
}

// presenceSeen is every presence event who would be sent, oldest first.
func (o *wsOrg) presenceSeen(t *testing.T, who string) []api.Presence {
	t.Helper()
	events, _, err := live.Read(o.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	var out []api.Presence
	for _, e := range events {
		if e.Kind != api.EventPresence {
			continue
		}
		r := httptest.NewRequest("GET", "/v1/events", nil)
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Authorization", "Bearer "+o.tokens[who])
		if sent, ok := o.h.liveForViewer(r, e); ok {
			var p api.Presence
			if err := json.Unmarshal(sent.Data, &p); err != nil {
				t.Fatal(err)
			}
			out = append(out, p)
		}
	}
	return out
}

func page(id string, kind, part string, editing bool) map[string]any {
	return map[string]any{"item": map[string]string{"kind": kind, "id": id}, "part": part, "editing": editing}
}

// ABIR OPENS A PAGE SANTOSH IS EDITING AND SEES "SANTOSH IS EDITING": the
// heartbeat answers who is here, a beat that changes nothing publishes
// nothing, and a person gone silent for the TTL leaves with an event.
func TestPresenceSaysWhoIsEditingWhichPage(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	o.beat(t, "santosh", page(webhook.ID, "workstream", "pg_retry", true), 200)
	got := o.beat(t, "abir", page(webhook.ID, "workstream", "pg_retry", false), 200)
	want := []api.PresenceHere{{Handle: "abir", Part: "pg_retry"}, {Handle: "santosh", Part: "pg_retry", Editing: true}}
	if got.Item != (api.PresenceItem{Kind: "workstream", ID: webhook.ID}) || !slices.Equal(got.Here, want) {
		t.Fatalf("abir's beat answered %+v", got)
	}
	seen := o.presenceSeen(t, "abir")
	if len(seen) != 2 || !slices.Equal(seen[1].Here, want) {
		t.Fatalf("abir heard %+v", seen)
	}

	// Ten seconds on, both beat again unchanged: no new event.
	o.now = o.now.Add(api.PresenceBeat)
	o.beat(t, "santosh", page(webhook.ID, "workstream", "pg_retry", true), 200)
	o.beat(t, "abir", page(webhook.ID, "workstream", "pg_retry", false), 200)
	if n := len(o.presenceSeen(t, "abir")); n != 2 {
		t.Fatalf("an unchanged beat published: %d events", n)
	}

	// santosh closes his laptop; abir keeps beating. After the TTL the sweep
	// lets abir know santosh left.
	o.now = o.now.Add(api.PresenceTTL - api.PresenceBeat)
	o.beat(t, "abir", page(webhook.ID, "workstream", "pg_retry", false), 200)
	o.now = o.now.Add(api.PresenceBeat)
	o.h.sweepPresence()
	seen = o.presenceSeen(t, "abir")
	if last := seen[len(seen)-1]; !slices.Equal(last.Here, []api.PresenceHere{{Handle: "abir", Part: "pg_retry"}}) {
		t.Fatalf("after santosh went quiet abir heard %+v", last)
	}

	// Leaving says so at once.
	o.beat(t, "abir", map[string]any{"item": map[string]string{"kind": "workstream", "id": webhook.ID}, "left": true}, 200)
	seen = o.presenceSeen(t, "abir")
	if last := seen[len(seen)-1]; len(last.Here) != 0 {
		t.Fatalf("abir's own leaving: %+v", last)
	}
}

// PRESENCE REACHES ONLY THOSE WHO MAY VIEW THE ITEM, and a beat on an item
// the caller cannot view is answered as though it did not exist.
func TestPresenceIsSeenOnlyByThoseWhoMayView(t *testing.T) {
	o := newWSOrg(t)
	_, launch, _, brand := o.orgScene(t)
	o.beat(t, "mia", page(brand.ID, "workstream", "", false), 200)
	o.beat(t, "abir", page(brand.ID, "workstream", "", false), 404)
	o.beat(t, "abir", page(launch.ID, "workstream", "", false), 404)
	o.beat(t, "abir", page("ws_missing", "workstream", "", false), 404)
	o.beat(t, "abir", page(brand.ID, "spaceship", "", false), 422)
	if seen := o.presenceSeen(t, "abir"); len(seen) != 0 {
		t.Fatalf("abir heard about Design's workstream: %+v", seen)
	}
	if seen := o.presenceSeen(t, "mia"); len(seen) != 1 || seen[0].Here[0].Handle != "mia" {
		t.Fatalf("mia heard %+v", seen)
	}
}

// ONE PERSON IN TWO WINDOWS on the same part is one row, editing if either is;
// on two parts, two rows.
func TestPresenceFoldsOnePersonsWindows(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	beat := func(client, part string, editing bool) api.Presence {
		b := page(webhook.ID, "workstream", part, editing)
		b["client"] = client
		return o.beat(t, "santosh", b, 200)
	}
	beat("a", "pg_1", false)
	got := beat("b", "pg_1", true)
	if !slices.Equal(got.Here, []api.PresenceHere{{Handle: "santosh", Part: "pg_1", Editing: true}}) {
		t.Fatalf("two windows on one page: %+v", got.Here)
	}
	got = beat("b", "pg_2", false)
	if len(got.Here) != 2 {
		t.Fatalf("two windows on two pages: %+v", got.Here)
	}
}
