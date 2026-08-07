package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestSentinelClientCallParsesYesAndNo(t *testing.T) {
	for _, test := range []struct {
		response string
		yes      bool
		line     string
	}{
		{response: "yes — a release landed", yes: true, line: "a release landed"},
		{response: "no — nothing changed", line: "nothing changed"},
	} {
		capture := &gateCaptureClient{model: "talk/model", response: test.response}
		settings := config.Config{Model: capture.model}
		client := &liveClient{settings: settings, model: capture.model, client: capture}
		verdict, err := checkSentinel(settings, client)(context.Background(), resident.SentinelPrompt{
			CharterID: "release-watch", Invariant: "Keep release notes current.",
			SentinelHint: "Did a release land?", Evidence: "poll due",
		})
		if err != nil {
			t.Fatal(err)
		}
		if verdict.Yes != test.yes || verdict.Line != test.line {
			t.Fatalf("verdict = %+v", verdict)
		}
		if len(capture.messages) != 2 || capture.messages[0].Content[0].Text != sentinelSystemPrompt ||
			!strings.Contains(capture.messages[1].Content[0].Text, "Keep release notes current.") {
			t.Fatalf("sentinel messages = %+v", capture.messages)
		}
	}
}

func TestWakeCommandRunsOnePassAndExits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wake.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	charter, err := store.NewCharter("wake-command", "Keep release notes current.", store.WatchSpec{
		Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "look for a release", Cadence: time.Hour},
	}, "Did a release land?", store.CharterAction{Template: "Update the release notes"},
		store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 2}, store.CharterActive,
		store.Ratification{Origin: store.OriginUser, SessionID: "wake-session", Evidence: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	if err := graph.PromoteCharter(charter.ID, "test fixture exercises tenured wake behavior", false); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	calls := 0
	builder := func(graph *store.Store, _ string) (*resident.Reconciler, error) {
		return resident.New(graph,
			func(_ context.Context, instruction, _ string) (resident.Compiled, error) {
				return resident.Compiled{Goal: "Re-grounded: " + instruction}, nil
			}, nil,
		).WithWatchEngine(0, func(_ context.Context, _ resident.SentinelPrompt) (resident.SentinelVerdict, error) {
			calls++
			return resident.SentinelVerdict{Yes: true, Line: "release found"}, nil
		}), nil
	}
	var output bytes.Buffer
	if err := runWakeWith([]string{"--db", path}, &output, builder); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(output.String(), "checked 1, fired 1") {
		t.Fatalf("wake output = %q calls=%d", output.String(), calls)
	}

	graph, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range nodes {
		if node.Provenance.CharterID != charter.ID {
			continue
		}
		found = true
		if node.Provenance.Origin != store.OriginTrigger || node.Provenance.Intent != "Re-grounded: "+charter.Action.Template {
			t.Fatalf("wake job provenance = %+v", node.Provenance)
		}
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("wake command did not splice trigger work")
	}

	output.Reset()
	if err := runWakeWith([]string{"--db", path}, &output, builder); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(output.String(), "examined 0") {
		t.Fatalf("second one-pass output = %q calls=%d", output.String(), calls)
	}
}
