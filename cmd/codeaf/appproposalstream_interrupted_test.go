package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestAppCrewProposalKeepsCompleteFinalLineAfterStreamStops(t *testing.T) {
	lines := []string{
		`{"t":"name","name":"Checks","goal":"Check the plan"}`,
		`{"t":"member","handle":"review","brief":"Review the plan","reports_to":"manager"}`,
	}
	conv := &streamingProposalConversation{proposingConversation: &proposingConversation{fakeConversation: &fakeConversation{}}, chunks: []string{lines[0] + "\n" + lines[1][:25], lines[1][25:]}, stop: errors.New("stream disconnected")}
	backend, rec, agent := proposingBackend(t, conv.proposingConversation)
	backend.mu.Lock()
	backend.agents[agent.ID].live.conn.Conv = conv
	backend.mu.Unlock()
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	at := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var got []string
	for i, event := range rec.events[:at] {
		if event != "crew.proposal.delta" {
			continue
		}
		raw, err := json.Marshal(rec.data[i])
		if err != nil {
			t.Fatal(err)
		}
		var delta appwire.CrewProposalDelta
		if err := json.Unmarshal(raw, &delta); err != nil {
			t.Fatal(err)
		}
		if delta.Agent != agent.ID || delta.Seq != len(got)+1 {
			t.Fatalf("delta = %s", raw)
		}
		got = append(got, string(delta.Line))
	}
	if !reflect.DeepEqual(got, lines) {
		t.Fatalf("deltas = %q, want %q", got, lines)
	}
	ask := rec.data[at].(appwire.AskOpened).Ask
	want := []appwire.ProposalHelper{{Handle: "review", Brief: "Review the plan", ReportsTo: "manager"}}
	if ask.Proposal == nil || !reflect.DeepEqual(ask.Proposal.Helpers, want) || !strings.Contains(ask.Detail, "Stopped after 1 helper") {
		t.Fatalf("ask = %+v", ask)
	}
}
