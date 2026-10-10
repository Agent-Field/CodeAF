package desktopbridge

import (
	"net/http"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

func suggestionsFor(t *testing.T, rig *placesRig) []placegraph.Suggestion {
	t.Helper()
	var reply struct {
		Suggestions []placegraph.Suggestion `json:"suggestions"`
	}
	if code := rig.do("GET", "/places/suggestions", nil, &reply); code != 200 || reply.Suggestions == nil {
		t.Fatalf("suggestions: %d %+v", code, reply)
	}
	return reply.Suggestions
}

func TestSuggestionsRoutesUseTheDateRuleAndSharedSnooze(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withStale()
	old := rig.mk("Old")
	pinned := rig.mk("Pinned")
	archived := rig.mk("Archived")
	rig.do("POST", "/places/"+pinned+"/pin", nil, nil)
	rig.do("POST", "/places/"+archived+"/archive", nil, nil)
	rig.advance(60*staleDay - time.Nanosecond)
	if got := suggestionsFor(t, rig); len(got) != 0 {
		t.Fatalf("early offer: %+v", got)
	}
	rig.advance(time.Nanosecond)
	if got := suggestionsFor(t, rig); len(got) != 1 || got[0] != (placegraph.Suggestion{Kind: "mergeOrArchive", PlaceID: old}) {
		t.Fatalf("idle offer: %+v", got)
	}
	before, _ := rig.p.Store.Revision()
	var snoozed staleSnoozed
	if code := rig.do("POST", "/places/suggestions/snooze", map[string]string{"placeId": old}, &snoozed); code != 200 || !snoozed.OK || snoozed.PlaceID != old || !snoozed.Until.Equal(rig.clock.Add(30*staleDay)) {
		t.Fatalf("snooze: %d %+v", code, snoozed)
	}
	after, _ := rig.p.Store.Revision()
	if after != before {
		t.Fatal("snooze changed graph revision")
	}
	second := newPlacesRigAt(t, rig.path)
	second.clock = rig.clock
	second.withStale()
	if got := suggestionsFor(t, second); len(got) != 0 || len(second.stale().Places) != 0 {
		t.Fatalf("shared snooze ignored: %+v", got)
	}
	rig.advance(30*staleDay - time.Nanosecond)
	if got := suggestionsFor(t, rig); len(got) != 0 {
		t.Fatalf("snooze ended early: %+v", got)
	}
	rig.advance(time.Nanosecond)
	if got := suggestionsFor(t, rig); len(got) != 1 || got[0].PlaceID != old {
		t.Fatalf("offer did not return: %+v", got)
	}
	if code := rig.do("POST", "/places/"+old+"/visit", nil, nil); code != 200 {
		t.Fatalf("visit: %d", code)
	}
	if got := suggestionsFor(t, rig); len(got) != 0 {
		t.Fatalf("visit ignored: %+v", got)
	}
}

func TestSuggestionsRoutesRejectUnavailableInvalidAndUnauthenticatedRequests(t *testing.T) {
	rig := newPlacesRig(t)
	for _, path := range []string{"/places/suggestions", "/places/suggestions/snooze"} {
		method := "GET"
		if path == "/places/suggestions/snooze" {
			method = "POST"
		}
		if code := rig.do(method, path, nil, nil); code != 404 {
			t.Fatalf("without storage: %s %d", path, code)
		}
		rig.withStale()
		req, _ := http.NewRequest(method, rig.srv.URL+"/api/engine"+path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("without token: %s %d", path, resp.StatusCode)
		}
		rig.p.Stale = nil
	}
	rig.withStale()
	for _, tc := range []struct {
		body map[string]string
		code int
	}{
		{map[string]string{}, 400},
		{map[string]string{"placeId": "missing"}, 404},
		{map[string]string{"placeId": "missing", "extra": "value"}, 400},
	} {
		if code := rig.do("POST", "/places/suggestions/snooze", tc.body, nil); code != tc.code {
			t.Fatalf("body %v: %d, want %d", tc.body, code, tc.code)
		}
	}
}

func TestSuggestionsUseMembershipChatActivity(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withStale()
	talked := rig.mk("Talked")
	quiet := rig.mk("Quiet")
	rig.advance(90 * staleDay)
	rig.setRows(row("c1", "recent", rig.clock.Add(-staleDay)))
	if code := rig.do("POST", "/places/"+talked+"/members", map[string]any{"chats": []string{"c1"}}, nil); code != 200 {
		t.Fatalf("membership: %d", code)
	}
	if got := suggestionsFor(t, rig); len(got) != 1 || got[0].PlaceID != quiet {
		t.Fatalf("membership activity ignored: %+v", got)
	}
}
