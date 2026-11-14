package blobstore_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

func TestStatsDecodesOptionalStoredTotals(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want []*int64
	}{
		{name: "older relay", body: `{}`, want: make([]*int64, 6)},
		{name: "null is unknown", body: `{"stored_bytes":null,"limit_bytes":null}`, want: make([]*int64, 6)},
		{name: "hosted totals", body: `{"stored_bytes":123,"stored_objects":4,"frames_today":2,"limit_bytes":5368709120,"limit_objects":5000000,"limit_frames_per_day":5000}`,
			want: []*int64{statsInt(123), statsInt(4), statsInt(2), statsInt(5 << 30), statsInt(5_000_000), statsInt(5_000)}},
		{name: "known zeros", body: `{"stored_bytes":0,"stored_objects":0,"frames_today":0,"limit_bytes":0,"limit_objects":0,"limit_frames_per_day":0}`,
			want: []*int64{statsInt(0), statsInt(0), statsInt(0), statsInt(0), statsInt(0), statsInt(0)}},
		{name: "totals without limits", body: `{"stored_bytes":7}`, want: []*int64{statsInt(7), nil, nil, nil, nil, nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/store/stats" {
					t.Errorf("stats request = %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if err := json.Unmarshal([]byte(tc.body), &body); err != nil {
					t.Error(err)
				}
				for name, value := range map[string]int64{"puts": 2, "gets": 3, "has": 4, "bytes_in": 5, "bytes_out": 6} {
					body[name] = value
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer srv.Close()
			got, err := blobstore.NewHTTP(srv.URL, fakeSign("alice"), srv.Client()).Stats(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got.Puts != 2 || got.Gets != 3 || got.Has != 4 || got.BytesIn != 5 || got.BytesOut != 6 {
				t.Fatalf("old counters changed: %+v", got)
			}
			fields := []*int64{got.StoredBytes, got.StoredObjects, got.FramesToday, got.LimitBytes, got.LimitObjects, got.LimitFramesPerDay}
			if !reflect.DeepEqual(fields, tc.want) {
				t.Fatalf("stored totals = %v, want %v", fields, tc.want)
			}
		})
	}
}

// A pointer preserves the distinction between zero and a missing field in wire fixtures.
func statsInt(n int64) *int64 { return &n }

func TestSelfHostedStatsOmitsStoredTotalsAndLimits(t *testing.T) {
	g := newRig(t)
	_, f := frame(t, "one")
	if _, err := g.client("alice").PutFrame(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, g.srv.URL+"/v1/store/stats", nil)
	if err != nil {
		t.Fatal(err)
	}
	fakeSign("alice")(req, nil)
	res, err := g.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got map[string]any
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"puts": float64(1), "gets": float64(0), "has": float64(0), "bytes_in": float64(len(f)), "bytes_out": float64(0)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("self-hosted stats = %v, want request counters only: %v", got, want)
	}
}
