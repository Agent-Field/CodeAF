package clientrun_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/nacl/box"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientrun"
)

func (e *engine) CrewList(bool) ([]appwire.Crew, error) {
	return []appwire.Crew{{ID: "local", Manager: chat}, {ID: "foreign", Manager: otherChat}}, nil
}
func (e *engine) CrewGet(id string) (appwire.Crew, error) {
	if id == "local" {
		return appwire.Crew{ID: id, Manager: chat}, nil
	}
	return appwire.Crew{ID: id, Manager: otherChat}, nil
}
func (e *engine) CrewItems(id string) ([]appwire.CrewItem, error) {
	return []appwire.CrewItem{{ID: "item", Crew: id}}, nil
}
func (e *engine) CrewLog(id, after string, limit int) ([]appwire.LogLine, string, error) {
	return []appwire.LogLine{{ID: "line", Crew: id, Text: "hello"}}, "line", nil
}

func TestCrewSnapshotsOnlyPublishCrewsHeldThroughTheirManager(t *testing.T) {
	g := newRig(t)
	_, err := g.r.Pair(context.Background(), clientrun.Pairing{Cert: g.client.Cert(), BoxKey: g.pub[:], Reads: []string{"crews.list", "crews.get", "crews.items", "crews.log"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"crews.list", "crews.get", "crews.items", "crews.log"} {
		var rows []struct {
			Crew   string
			Result json.RawMessage
		}
		if err = json.Unmarshal(g.slot(t, method), &rows); err != nil {
			t.Fatal(err)
		}
		if method == "crews.list" {
			var result appwire.CrewsResult
			if len(rows) != 1 {
				t.Fatalf("list: %s", g.slot(t, method))
			}
			_ = json.Unmarshal(rows[0].Result, &result)
			if len(result.Crews) != 1 || result.Crews[0].ID != "local" {
				t.Fatalf("foreign crew leaked: %+v", result)
			}
		} else if len(rows) != 1 || rows[0].Crew != "local" {
			t.Fatalf("crew scope: %+v", rows)
		}
	}
}

func (e *engine) CrewMessage(p appwire.CrewMessage) (appwire.Entry, error) {
	e.answers++
	return appwire.Entry{Agent: chat}, nil
}
func (e *engine) CrewCreate(p appwire.CrewCreate) (appwire.Crew, error) {
	e.created++
	return appwire.Crew{ID: "local", Manager: chat}, nil
}
func (e *engine) CrewDecide(packet, option, text, reason string) (appwire.Decision, error) {
	e.answers++
	return appwire.Decision{ID: packet}, nil
}

func (g *rig) queueCrew(t *testing.T, grant clientrun.Grant, method string, params any, face bool) {
	t.Helper()
	raw, _ := json.Marshal(params)
	payload, _ := json.Marshal(clientrun.Call{Method: method, Params: raw, FaceID: face})
	sealed, err := box.SealAnonymous(nil, payload, (*[32]byte)(grant.BoxKey), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	target := g.s.dev.ID()
	if method == "crews.create" {
		target = "any"
	}
	g.request(t, g.client, "POST", "/v1/clients/"+g.client.dev.ID()+"/queue", map[string]any{"number": 1, "at": g.now.UnixMilli(), "method": method, "target": target, "sealed": sealed}, nil, 204)
}

func TestQueuedCrewWritesValidateActualOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, method string
		params       any
		allowed      bool
	}{
		{"local message", "crews.message", appwire.CrewMessage{Crew: "local", Text: "hi"}, true},
		{"foreign message", "crews.message", appwire.CrewMessage{Crew: "foreign", Text: "hi"}, false},
		{"nonmember", "crews.message", appwire.CrewMessage{Crew: "local", To: otherChat, Text: "hi"}, false},
		{"local create", "crews.create", appwire.CrewCreate{Name: "local", Agents: []string{chat}, Manager: chat}, true},
		{"foreign member", "crews.create", appwire.CrewCreate{Name: "local", Agents: []string{chat, otherChat}, Manager: chat}, false},
		{"foreign parent", "crews.create", appwire.CrewCreate{Name: "local", Agents: []string{chat}, Manager: chat, Parent: "foreign"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newRig(t)
			grant := g.pair(t, tc.method)
			g.queueCrew(t, grant, tc.method, tc.params, false)
			err := g.r.Step(context.Background())
			if tc.allowed {
				if err != nil || g.e.answers+g.e.created != 1 {
					t.Fatalf("allowed: %v", err)
				}
			} else if err == nil || g.e.answers+g.e.created != 0 {
				t.Fatalf("foreign mutation executed: %v", err)
			}
		})
	}
}

func TestQueuedCrewDecisionChecksThePacketAskAndFaceID(t *testing.T) {
	for _, face := range []bool{false, true} {
		t.Run(fmt.Sprint(face), func(t *testing.T) {
			g := newRig(t)
			g.e.asks = []appwire.Ask{{ID: "crew:packet", Agent: chat, Irreversible: true, Subkind: appwire.SubkindCrewDecision, Subject: &appwire.AskSubject{Kind: appwire.SubjectCrewDecision, Ref: "packet"}}}
			grant := g.pair(t, "crews.decide")
			g.queueCrew(t, grant, "crews.decide", map[string]string{"packet": "packet", "option": "yes", "agent": otherChat}, face)
			err := g.r.Step(context.Background())
			if face {
				if err != nil || g.e.answers != 1 {
					t.Fatalf("confirmed: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "Face ID") || g.e.answers != 0 {
				t.Fatalf("unconfirmed: %v", err)
			}
		})
	}
}

func (e *engine) CrewStart(p appwire.CrewCreate) (appwire.CrewStartResult, error) {
	a := appwire.Agent{ID: otherChat}
	if p.BeforeStart != nil {
		if err := p.BeforeStart(a); err != nil {
			return appwire.CrewStartResult{}, err
		}
	}
	e.created++
	return appwire.CrewStartResult{Crew: appwire.Crew{ID: "started", Manager: a.ID}, Start: appwire.CrewStarted{Manager: a.ID}}, nil
}

func TestQueuedCrewStartLeasesItsNewManager(t *testing.T) {
	g := newRig(t)
	grant := g.pair(t, "crews.create")
	g.queueCrew(t, grant, "crews.create", appwire.CrewCreate{Name: "new", StartManager: true, ManagerStartsMembers: true, RequestID: "phone-new"}, false)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	listing, err := g.dir.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := listing.Cells[otherChat]; !ok || c.Lease.Device != g.s.dev.ID() {
		t.Fatalf("manager was not leased: %+v", c)
	}
	receipts := g.r.Receipts()
	if len(receipts) != 1 || receipts[0].Agent != otherChat || receipts[0].Status != "done" {
		t.Fatalf("manager receipt: %+v", receipts)
	}
}

func TestQueuedCrewDecisionRejectsForeignAndMissingPackets(t *testing.T) {
	for _, agent := range []string{otherChat, ""} {
		name := "foreign"
		if agent == "" {
			name = "missing"
		}
		t.Run(name, func(t *testing.T) {
			g := newRig(t)
			if agent != "" {
				g.e.asks = []appwire.Ask{{ID: "crew:packet", Agent: agent, Subject: &appwire.AskSubject{Kind: appwire.SubjectCrewDecision, Ref: "packet"}}}
			}
			grant := g.pair(t, "crews.decide")
			g.queueCrew(t, grant, "crews.decide", map[string]string{"packet": "packet", "option": "yes", "agent": chat}, true)
			if err := g.r.Step(context.Background()); err == nil || g.e.answers != 0 {
				t.Fatalf("foreign or absent packet executed: %v", err)
			}
		})
	}
}
