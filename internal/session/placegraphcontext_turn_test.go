package session

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// TestPlaceGraphAcrossTurns follows one conversation through the turn boundary
// and reopening, because separate checks cannot prove the same membership
// change preserves the prompt cache and leaves a durable explanation.
// It covers PL-009 (Places 6e) and the persisted note from PL-122 (Places 6f).
func TestPlaceGraphAcrossTurns(t *testing.T) {
	f := newPlaceFixture(t)
	brand := filepath.Join(t.TempDir(), "brand-voice.md")
	if err := os.WriteFile(brand, []byte("Use short sentences."), 0o600); err != nil {
		t.Fatal(err)
	}
	marketing := f.place(t, "Marketing", placegraph.Context{
		Instructions: "Write in the brand voice.",
		Sources: []placegraph.Source{{
			ID: "brand", Kind: placegraph.SourceFile, Ref: brand,
			Label: "brand-voice.md", AddedBy: placegraph.AddedByYou,
		}},
	})
	var agent *Agent
	var receiptID string
	answer := func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			// The provider callback puts the change inside the first turn without
			// a clock or a race with Submit's opening refresh.
			_, receipt, err := f.store.AddChat(agent.id, marketing.ID, placegraph.AddedByYou)
			if err != nil {
				return nil, err
			}
			receiptID = receipt.ID
			return toolResponse("read-brand", "read", `{"path":"brand-voice.md"}`), nil
		},
		answer, answer, answer,
	}}
	agent, journal := placedAgent(t, f, completer)

	first := turn(t, agent, completer, "Draft the release notes.")
	firstEnd := completer.requests()
	if firstEnd-first != 2 || receiptID == "" {
		t.Fatal("turn 1 must add membership and continue with a second provider request")
	}
	for i := first; i < firstEnd; i++ {
		sys := requestSystem(completer.request(i))
		if strings.Contains(sys, placeGraphHeading) || strings.Contains(sys, "Marketing") || strings.Contains(sys, brand) {
			t.Fatalf("turn 1 request %d received membership added during that turn:\n%s", i-first, sys)
		}
	}

	second := completer.request(turn(t, agent, completer, "Revise the notes."))
	for _, want := range []string{placeGraphHeading, "## Instructions from Marketing", "Write in the brand voice.", brand} {
		if !strings.Contains(requestSystem(second), want) {
			t.Fatalf("turn 2 message[0] lacks %q:\n%s", want, requestSystem(second))
		}
	}
	const note = "Now also using Marketing: brand-voice.md"
	noteSent := false
	for _, message := range second[1:] {
		if message.Role == "user" && strings.Contains(messageText(message), note) {
			noteSent = true
		}
	}
	if !noteSent {
		t.Fatal("turn 2 did not tell the provider what membership changed")
	}

	third := completer.request(turn(t, agent, completer, "Finalize the notes."))
	// Compare the whole wire message, including its role and content shape,
	// because identical extracted text alone does not prove cache stability.
	secondBytes, err := json.Marshal(second[0])
	if err != nil {
		t.Fatal(err)
	}
	thirdBytes, err := json.Marshal(third[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(secondBytes, thirdBytes) {
		t.Fatalf("unchanged turn 3 message[0] differs:\nturn 2: %s\nturn 3: %s", secondBytes, thirdBytes)
	}
	if completer.requests() != 4 {
		t.Fatalf("three turns should consume exactly four scripted requests, got %d", completer.requests())
	}

	assertNote := func(where string, entries []DisplayEntry) {
		t.Helper()
		count := 0
		for _, entry := range entries {
			if entry.Role == "aside" && entry.AsideKind == NoteKindPlaces && entry.Text == note {
				count++
				if len(entry.UndoReceipts) != 1 || entry.UndoReceipts[0] != receiptID {
					t.Fatalf("%s: membership note lost its undo receipt: %v", where, entry.UndoReceipts)
				}
			}
		}
		if count != 1 {
			t.Fatalf("%s: expected one %q note, got %d", where, note, count)
		}
	}
	assertNote("live", agent.Transcript())
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	assertNote("reopened", reopen(t, journal).Transcript())
}
