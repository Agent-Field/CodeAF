package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// P12: both typed routes name an older factory and retain existing mappings.
func TestFactoryPolicyRefusals(t *testing.T) {
	for _, tc := range []struct {
		status               int
		answer, code, reason string
	}{
		{404, `{"error":"factory endpoint not found"}`, appwire.CodeConflict, "factory_too_old"},
		{405, `{"error":"this endpoint accepts GET"}`, appwire.CodeConflict, "factory_too_old"},
		{403, `{"error":"only the policy's owner or the operator can change it"}`, appwire.CodeForbidden, ""},
		{409, `{"error":"the policy changed","reason":"stale"}`, appwire.CodeStale, "stale"},
		{422, `{"error":"allowance jobs must be distinct positive job ids"}`, appwire.CodeBadParams, ""},
	} {
		for _, put := range []bool{false, true} {
			// Reads are never owner-guarded: only a replacement can meet 403.
			if !put && tc.status != 404 && tc.status != 405 {
				continue
			}
			var sends atomic.Int64
			b := waitsBackend(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.answer))
			})
			var err error
			if put {
				_, err = b.FactoryPolicySet(context.Background(), appwire.FactoryPolicySetParams{Factory: "f", Group: "g"})
			} else {
				_, err = b.FactoryPolicyGet(context.Background(), appwire.FactoryPolicyGetParams{Factory: "f", Group: "g"})
			}
			var wire *appwire.Error
			if !errors.As(err, &wire) || wire.Code != tc.code || sends.Load() != 1 {
				t.Fatalf("status %d, put %t: %v, sends %d", tc.status, put, err, sends.Load())
			}
			if tc.reason != "" {
				detail, _ := wire.Detail.(*appwire.FactoryRefusal)
				if detail == nil || detail.Reason != tc.reason {
					t.Fatalf("detail = %+v", wire.Detail)
				}
			}
		}
	}
}

// P9, P11, P12: one read or replacement carries group, authority and version.
func TestFactoryPolicyRoundTrip(t *testing.T) {
	var gets, puts atomic.Int64
	group := "g / next"
	allowances := []factory.Allowance{{ID: "m2", Jobs: []int{1, 2}, CapUSD: 15, EndsAt: "2026-11-12T12:00:00Z"}}
	b := waitsBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/v1/groups/g%20%2F%20next/policy" || r.Header.Get("Authorization") != "Bearer waits-fixture-bearer-credential" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		if r.Method == "GET" {
			gets.Add(1)
		} else if r.Method == "PUT" {
			puts.Add(1)
			var body struct {
				Stopped    bool                `json:"stopped"`
				Allowances []factory.Allowance `json:"allowances"`
				Version    *int                `json:"version"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.Stopped || body.Version == nil || *body.Version != 7 || !reflect.DeepEqual(body.Allowances, allowances) {
				t.Errorf("body = %+v, %v", body, err)
			}
		} else {
			t.Errorf("method = %s", r.Method)
		}
		_, _ = w.Write([]byte(`{"group":"g / next","version":8,"stopped":true,"spent_usd":12,"allowances":[{"id":"m2","jobs":[1,2],"cap_usd":15,"ends_at":"2026-11-12T12:00:00Z","spent_usd":12,"state":"active"}]}`))
	})
	version := 7
	got, err := b.FactoryPolicySet(context.Background(), appwire.FactoryPolicySetParams{Factory: "f", Group: group, Stopped: true, Allowances: allowances, Version: &version})
	if err != nil || got.Version != 8 || got.SpentUSD != 12 || puts.Load() != 1 || gets.Load() != 0 {
		t.Fatalf("set = %+v, %v", got, err)
	}
	got, err = b.FactoryPolicyGet(context.Background(), appwire.FactoryPolicyGetParams{Factory: "f", Group: group})
	if err != nil || got.Group != group || got.Allowances[0].State != "active" || gets.Load() != 1 || puts.Load() != 1 {
		t.Fatalf("get = %+v, %v", got, err)
	}
	caps, err := b.FactoryCapabilities(context.Background())
	if err != nil || !caps.GroupPolicy || !caps.CreateGroup {
		t.Fatalf("capabilities = %+v, %v", caps, err)
	}
}

// P11: direct backend calls validate before acquiring or sending credentials.
func TestFactoryPolicyBackendBounds(t *testing.T) {
	var sends atomic.Int64
	b := waitsBackend(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); w.WriteHeader(500) })
	for _, p := range []appwire.FactoryPolicySetParams{
		{Factory: "f", Group: ""}, {Factory: "f", Group: strings.Repeat("x", 201)},
		{Factory: "f", Group: "g", Allowances: []factory.Allowance{{ID: "bad id"}}},
	} {
		_, err := b.FactoryPolicySet(context.Background(), p)
		var wire *appwire.Error
		if !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
			t.Fatal(err)
		}
	}
	if sends.Load() != 0 {
		t.Fatalf("invalid requests = %d", sends.Load())
	}
}

// P11, P12: create_group is validated, sent and read with the created job.
func TestFactoryPolicyCreateGroup(t *testing.T) {
	var writes atomic.Int64
	b := waitsBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			writes.Add(1)
			var body struct {
				Group string `json:"group"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Group != "g" {
				t.Errorf("create group = %+v, %v", body, err)
			}
			_, _ = w.Write([]byte(`{"id":1}`))
		} else {
			_, _ = w.Write([]byte(`{"id":"job 1","group":"g","state":"backlog"}`))
		}
	})
	got, err := b.FactoryJobCreate(context.Background(), appwire.FactoryJobCreateParams{Factory: "f", Repo: "owner/api", Title: "job", Group: " g "})
	if err != nil || got.Current == nil || got.Current.Group != "g" || writes.Load() != 1 {
		t.Fatalf("create = %+v, %v", got, err)
	}
	_, err = b.FactoryJobCreate(context.Background(), appwire.FactoryJobCreateParams{Factory: "f", Repo: "owner/api", Title: "job", Group: "g\x00"})
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams || writes.Load() != 1 {
		t.Fatalf("invalid create = %v, %d", err, writes.Load())
	}
}

// P12: older strict create decoders refuse the group instead of dropping it.
func TestFactoryPolicyCreateGroupOlderFactory(t *testing.T) {
	var sends atomic.Int64
	b := waitsBackend(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"error":"json: unknown field \"group\""}`))
	})
	_, err := b.FactoryJobCreate(context.Background(), appwire.FactoryJobCreateParams{Factory: "f", Repo: "owner/api", Title: "job", Group: "g"})
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeConflict || sends.Load() != 1 {
		t.Fatalf("old create = %v", err)
	}
	detail, _ := wire.Detail.(*appwire.FactoryRefusal)
	if detail == nil || detail.Reason != "factory_too_old" {
		t.Fatalf("detail = %+v", wire.Detail)
	}
}
