package clusterstate_test

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

func frameHintCommand(t *testing.T, body []byte, now int64) cs.Command {
	t.Helper()
	proof := cs.V1Request{Method: "POST", URI: "/v1/dir/cells/chat/publish", IdentityKey: actorKey("root").Public().(ed25519.PublicKey), Certificate: joinCert(t, "a"), At: strconv.FormatInt(now, 10), Body: body}
	proof.Signature = ed25519.Sign(actorKey("a"), cs.V1SigningMessage(proof.Method, proof.URI, proof.At, body))
	command, err := cs.V1Command("test", now, proof)
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func TestFrameHintsSignedV1PublishAppliesAndKeepsStrictFields(t *testing.T) {
	hints := []cs.FrameHint{{ID: strings.Repeat("a", 64), Bytes: 42}}
	body, err := json.Marshal(cs.Publish{Fence: 1, OldHead: "old", Head: "new", FrameHints: hints})
	if err != nil {
		t.Fatal(err)
	}
	command := frameHintCommand(t, body, 1000)
	a := newApplication(t)
	a.put(cs.DeviceKey(command.Actor), cs.Device{V: 1})
	a.put(cs.CellKey("chat"), cs.Created(cs.CellInit{Head: "old"}, command.Actor, 999))
	result := a.evaluate(command).Result
	if result.Code != cs.OK || result.View == nil || result.View.Cell.Head != "new" || !reflect.DeepEqual(result.View.Cell.FrameHints, hints) {
		t.Fatalf("publish = %+v", result)
	}
	if got := a.cell(); !reflect.DeepEqual(got.FrameHints, hints) {
		t.Fatalf("persisted hints = %v", got.FrameHints)
	}
	proof := *command.V1
	proof.Body = append(append([]byte(nil), body[:len(body)-1]...), []byte(`,"unknown":true}`)...)
	proof.Signature = ed25519.Sign(actorKey("a"), cs.V1SigningMessage(proof.Method, proof.URI, proof.At, proof.Body))
	if _, err := cs.V1Command("test", 1000, proof); !errors.Is(err, cs.ErrCodec) {
		t.Fatalf("unknown field = %v", err)
	}
	clear := []byte(`{"fence":1,"old_head":"new","head":"last"}`)
	result = a.evaluate(frameHintCommand(t, clear, 1001)).Result
	if result.Code != cs.OK || result.View.Cell.Head != "last" || len(result.View.Cell.FrameHints) != 0 {
		t.Fatalf("absent hints kept stale metadata: %+v", result)
	}
}

func TestFrameHintsInvalidSetsAreOmittedWhilePublishMovesHead(t *testing.T) {
	one := cs.FrameHint{ID: strings.Repeat("a", 64), Bytes: 42}
	for name, hints := range map[string][]cs.FrameHint{
		"too many":             {one, {ID: strings.Repeat("b", 64), Bytes: 1}, {ID: strings.Repeat("c", 64), Bytes: 1}},
		"non hex":              {{ID: strings.Repeat("g", 64), Bytes: 1}},
		"duplicate":            {one, one},
		"unsafe integer bytes": {{ID: one.ID, Bytes: math.MaxInt64}},
		"over budget":          {{ID: one.ID, Bytes: cs.FrameHintBudget}},
		"probe exceeds budget": {{ID: one.ID, Bytes: cs.FrameHintBudget - 2}, {ID: strings.Repeat("b", 64), Bytes: 1}},
		"zero bytes":           {{ID: one.ID, Bytes: 0}},
		"negative bytes":       {{ID: one.ID, Bytes: -1}},
	} {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(cs.Publish{Fence: 1, OldHead: "old", Head: "new", FrameHints: hints})
			if err != nil {
				t.Fatal(err)
			}
			command := frameHintCommand(t, body, 1000)
			a := newApplication(t)
			a.put(cs.DeviceKey(command.Actor), cs.Device{V: 1})
			a.put(cs.CellKey("chat"), cs.Created(cs.CellInit{Head: "old", FrameHints: []cs.FrameHint{one}}, command.Actor, 999))
			result := a.evaluate(command).Result
			if result.Code != cs.OK || result.View == nil || result.View.Cell.Head != "new" || len(result.View.Cell.FrameHints) != 0 || len(a.cell().FrameHints) != 0 {
				t.Fatalf("invalid metadata refused head or survived: %+v", result)
			}
		})
	}
}

func TestFrameHintsCommandAndRowRoundTrip(t *testing.T) {
	hints := []cs.FrameHint{{ID: strings.Repeat("a", 64), Bytes: cs.FrameHintBudget - 1}}
	for _, change := range []cs.Mutation{
		{Create: &cs.CreateCell{Cell: "chat", Init: cs.CellInit{Head: "new", FrameHints: hints}}},
		{Publish: &cs.PublishCell{Cell: "chat", Publish: cs.Publish{Head: "new", FrameHints: hints}}},
	} {
		command := cs.Command{Schema: cs.SchemaVersion, Cluster: "test", Actor: "a", Session: "s", Sequence: 1, RequestDigest: "digest", Change: change}
		raw, err := cs.EncodeCommand(command)
		if err != nil {
			t.Fatal(err)
		}
		got, err := cs.DecodeCommand(raw)
		if err != nil || !reflect.DeepEqual(command, got) {
			t.Fatalf("command round trip = %+v, %v", got, err)
		}
	}
	source := cs.Created(cs.CellInit{Head: "new", FrameHints: hints}, "a", 1000)
	hints[0].Bytes = 1
	if source.FrameHints[0].Bytes != cs.FrameHintBudget-1 {
		t.Fatal("create aliased hint input")
	}
	row := cs.Row{Schema: cs.SchemaVersion, Key: "chat", Cell: &source}
	raw, err := cs.EncodeRow(row)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cs.DecodeRow(raw)
	if err != nil || !reflect.DeepEqual(row, got) {
		t.Fatalf("row round trip = %+v, %v", got, err)
	}
	got.Cell.FrameHints[0].Bytes = 2
	if source.FrameHints[0].Bytes != cs.FrameHintBudget-1 {
		t.Fatal("decoded row aliased source")
	}
}

func TestFrameHintsAloneDoNotMoveVisibleVersion(t *testing.T) {
	a := newApplication(t)
	first := a.run("a", 1000, cs.Mutation{Create: &cs.CreateCell{Cell: "chat", Init: cs.CellInit{Head: "head"}}})
	next := a.run("a", 1000, cs.Mutation{Publish: &cs.PublishCell{Cell: "chat", Publish: cs.Publish{Fence: 1, OldHead: "head", Head: "head", FrameHints: []cs.FrameHint{{ID: strings.Repeat("a", 64), Bytes: 42}}}}})
	if next.Code != cs.OK || next.Version != first.Version || len(a.cell().FrameHints) != 1 {
		t.Fatalf("advisory hints changed listing version: first=%+v next=%+v", first, next)
	}
}
