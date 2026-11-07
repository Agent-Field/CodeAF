package codeaftrigger

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// goldenSignature is one signature both clients must make: sdk/typescript's
// verifyWebhook was checked against exactly this vector, so a change to how
// Go signs that breaks it breaks the TypeScript client too.
const goldenSignature = "t=1762430000,sha256=f422f8b27aa249cef7eb2e392d323b41c12d149d3daa64805c948469547e8046"

func TestTheSignatureIsTheGoldenOne(t *testing.T) {
	body := []byte(`{"id":"dlv_1","event":"job.filed"}`)
	if got := Sign("whsec_abc", body, time.Unix(1762430000, 0)); got != goldenSignature {
		t.Fatalf("Sign = %s", got)
	}
	if err := VerifyWebhookAt("whsec_abc", body, goldenSignature, time.Unix(1762430010, 0)); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(VerifyWebhookAt("whsec_abc", body, "sha256=00", time.Unix(1762430010, 0)), ErrSignature) {
		t.Fatal("a header with no timestamp passed")
	}
}

func TestTheClientCallsTheFourPathsAndReadsRefusals(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.EscapedPath()+" "+r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api/intake":
			json.NewEncoder(w).Encode(Filed{ID: 4, Created: true})
		case "/api/intake/inbox:ops/A 1/messages":
			json.NewEncoder(w).Encode(Accepted{Job: 4, Accepted: true})
		case "/api/intake/inbox:ops/A 1":
			json.NewEncoder(w).Encode(Status{Job: 4, State: "running", StepsDone: 1, StepsTotal: 3})
		default:
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"this token files and reads only work from ops"}`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL+"/", "cft_x")
	ctx := context.Background()
	if f, err := c.File(ctx, Entry{Source: "inbox:ops", ExternalID: "A 1", Title: "t"}); err != nil || f.ID != 4 {
		t.Fatalf("file %+v %v", f, err)
	}
	if a, err := c.Message(ctx, "inbox:ops", "A 1", "more"); err != nil || !a.Accepted {
		t.Fatalf("message %+v %v", a, err)
	}
	if s, err := c.Status(ctx, "inbox:ops", "A 1"); err != nil || s.State != "running" || s.StepsTotal != 3 {
		t.Fatalf("status %+v %v", s, err)
	}
	var refusal *Error
	if _, err := c.Status(ctx, "sales", "S1"); !errors.As(err, &refusal) || refusal.Status != 403 || refusal.Message == "" {
		t.Fatalf("refusal %v", err)
	}
	if seen[0] != "POST /api/intake Bearer cft_x" || seen[2] != "GET /api/intake/inbox:ops/A%201 Bearer cft_x" {
		t.Fatalf("calls %v", seen)
	}
}
