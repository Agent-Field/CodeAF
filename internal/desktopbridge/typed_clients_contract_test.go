package desktopbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/council"
	"github.com/Agent-Field/codeaf/internal/session"
)

// These goldens keep the renderer contracts tied to real handlers and Go wire
// types. The fixture clock and identifiers are deterministic, without sleeps.
func TestTypedDecisionClientWireFixtures(t *testing.T) {
	rig := newPlacesRig(t)
	place := rig.mk("Release")
	base := "/places/" + place + "/knows"
	got := map[string][]byte{}
	capture := func(name, method, path, body string, status int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, rig.srv.URL+"/api/engine"+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+placesToken)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != status {
			t.Fatalf("%s: status=%d body=%s err=%v", name, res.StatusCode, data, err)
		}
		got[name] = data
		return data
	}
	capture("knows-empty", "GET", base, "", 200)
	var added knowsMutation
	data := capture("knows-add", "POST", base, `{"text":"Run tests"}`, 200)
	if err := json.Unmarshal(data, &added); err != nil || added.Line == nil {
		t.Fatalf("add: %s", data)
	}
	capture("knows-list", "GET", base, "", 200)
	line := base + "/" + added.Line.ID
	capture("knows-edit", "PATCH", line, `{"text":"Run focused tests"}`, 200)
	capture("knows-conflict", "POST", base, `{"text":"Skip tests","supersedes":"`+added.Line.ID+`"}`, 200)
	rig.advance(60 * 24 * time.Hour)
	capture("knows-confirm", "POST", line+"/still-true", `{"yes":true}`, 200)
	capture("knows-remove", "DELETE", line, `{}`, 200)
	capture("stale", "POST", base, `{"text":"Run tests","ifRevision":0}`, 409)
	capture("councils-empty", "GET", "/councils", "", 200)
	capture("council-refusal", "POST", "/councils/c1/steer", `{"text":"Wait"}`, 409)
	capture("decisions-unavailable", "GET", "/places/"+place+"/decisions", "", 501)
	// The nonempty council golden is marshalled by the same wire adapter as both
	// council routes, with no temporary session path leaking into the fixture.
	c := councilToItem(council.Council{ID: "c1", Places: [2]string{"p1", "p2"}, Topic: "Launch", ChatID: "chat1", Label: "Marketing with Software", Turns: 1, Cap: 6, Spend: 0.01, CapUSD: 0.25, State: council.StateRunning, OpenedAt: placesEpoch}, "")
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	got["council"] = append(data, '\n')
	b, sid, _, book := planFixture(t)
	step := session.PlanCardStep{Kind: session.PlanStepStop, Target: session.PlanTarget{Task: "t9"}, Text: "Stop task"}
	if _, err := book.Propose(context.Background(), session.Plan{ID: "plan1", ReachesBeyond: true, Steps: []session.PlanCardStep{step}}); err != nil {
		t.Fatal(err)
	}
	plan := "/api/engine/sessions/" + sid + "/plan/plan1/"
	for _, verb := range []string{"edit", "go"} {
		body := "{}"
		if verb == "edit" {
			body = `{"steps":[{"kind":"stop","target":{"task":"t9"},"text":"Stop task"}]}`
		}
		w := request(b, "POST", plan+verb, body)
		if w.Code != 200 {
			t.Fatalf("plan %s: %d %s", verb, w.Code, w.Body.String())
		}
		got["plan-"+verb] = w.Body.Bytes()
	}
	if _, err := book.Propose(context.Background(), session.Plan{ID: "plan2", ReachesBeyond: true, Steps: []session.PlanCardStep{step}}); err != nil {
		t.Fatal(err)
	}
	plan = "/api/engine/sessions/" + sid + "/plan/plan2/"
	for _, verb := range []string{"cancel", "go"} {
		w := request(b, "POST", plan+verb, "{}")
		status := 200
		if verb == "go" {
			status = 409
		}
		if w.Code != status {
			t.Fatalf("plan %s: %d %s", verb, w.Code, w.Body.String())
		}
		name := "plan-cancel"
		if verb == "go" {
			name = "plan-refusal"
		}
		got[name] = w.Body.Bytes()
	}
	dir := "../../desktop/src/features/decisions/fixtures"
	for name, data := range got {
		path := filepath.Join(dir, name+".json")
		if os.Getenv("UPDATE_DECISIONS_FIXTURES") == "1" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(want, data) {
			t.Errorf("%s changed; regenerate with UPDATE_DECISIONS_FIXTURES=1", name)
		}
	}
}
