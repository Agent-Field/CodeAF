package clusterstate_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

// r1.2
func TestR12LegacySourceEnvelopeRetainsServerFields(t *testing.T) {
	key := actorKey("root")
	cfg := cs.LegacyConfig{Cluster: "upgrade", Generation: 1, Identity: cs.IdentityID(key.Public().(ed25519.PublicKey)), SigningKey: key.Public().(ed25519.PublicKey), EffectsKey: actorKey("effects").Public().(ed25519.PublicKey), Anchors: []uint64{1, 2, 3}}
	init := cs.CellInit{Head: "saved", Title: ""}
	base, _ := json.Marshal(init)
	init.Title = strings.Repeat("x", cs.MaxV1BodyBytes-len(base)-len(`,"title":""`))
	body, _ := json.Marshal(init)
	if len(body) != cs.MaxV1BodyBytes {
		t.Fatalf("body=%d", len(body))
	}
	cell := cs.Created(init, "dev_"+strings.Repeat("a", 32), 1000)
	raw, _ := json.Marshal(cell)
	row, err := cs.LegacyRow(cfg, "cells", "chat", raw, 0)
	if err != nil {
		t.Fatalf("accepted body cannot migrate after server fields: source=%d: %v", len(raw), err)
	}
	var got cs.Cell
	if cs.Decode(row.Data, &got) != nil || !reflect.DeepEqual(got, cell) {
		t.Fatal("lossy source conversion")
	}
	cell.Title = strings.Repeat("x", cs.MaxV1BodyBytes+1)
	raw, _ = json.Marshal(cell)
	saved := bytes.Clone(raw)
	if _, err := cs.LegacyRow(cfg, "cells", "chat", raw, 0); err == nil || !bytes.Equal(saved, raw) {
		t.Fatal("impossible source row accepted or changed")
	}
}

// r1.4
func TestR14CapacityRefusalAtContentCeilingCommitsAndRetries(t *testing.T) {
	for _, spare := range []uint64{0, 1} {
		t.Run(fmt.Sprint(spare), func(t *testing.T) {
			a := newApplication(t)
			var size uint64
			for k, b := range a.rows {
				size += cs.RecordBytes(k, b)
			}
			a.meta.Ceiling = size + spare
			command, err := cs.Sign(cs.Command{Schema: cs.SchemaVersion, Cluster: "test", Actor: "a", Session: "new-session", Sequence: 1, Now: 1000, Change: cs.Mutation{SetVault: &cs.VaultCAS{New: strings.Repeat("v", 4000)}}}, actorKey("a"))
			if err != nil {
				t.Fatal(err)
			}
			first := a.evaluate(command)
			if first.Result.Code != cs.Capacity || a.rows[cs.SessionKey("a", "new-session")] == nil {
				t.Fatal("capacity was not persisted", first.Result)
			}
			retry := a.evaluate(command)
			if !reflect.DeepEqual(first.Result, retry.Result) || len(retry.Writes) != 0 {
				t.Fatal("retry changed the capacity result")
			}
			command.Sequence = 2
			command.Ack = 1
			command.Change = cs.Mutation{AdvanceTime: &cs.AdvanceTime{}}
			command, err = cs.Sign(command, actorKey("a"))
			if err != nil {
				t.Fatal(err)
			}
			if next := a.evaluate(command); next.Result.Code != cs.OK {
				t.Fatal("next committed entry cannot apply", next.Result)
			}
		})
	}
}

// r1.5
func TestR15RefusalBookkeepingRemainsBounded(t *testing.T) {
	a := newApplication(t)
	for k, b := range a.rows {
		a.meta.Ceiling += cs.RecordBytes(k, b)
	}
	a.meta.Ceiling -= cs.DefaultApplicationBytes
	original := a.meta.Ceiling
	for i := 0; i < cs.MaxRetainedSessions; i++ {
		command, err := cs.Sign(cs.Command{Schema: cs.SchemaVersion, Cluster: "test", Actor: "a", Session: fmt.Sprintf("%d-%s", i, strings.Repeat("s", 1000)), Sequence: 1, Now: 1000, Change: cs.Mutation{SetVault: &cs.VaultCAS{New: "growth"}}}, actorKey("a"))
		if err != nil {
			t.Fatal(err)
		}
		refusal := a.evaluate(command)
		if refusal.Result.Code != cs.Capacity || len(refusal.Writes) == 0 {
			t.Fatalf("session %d did not persist refusal: %v", i, refusal.Result)
		}
		command.Sequence++
		command.Ack = 1
		command.Change = cs.Mutation{CloseSession: &cs.CloseSession{}}
		command, err = cs.Sign(command, actorKey("a"))
		if err != nil {
			t.Fatal(err)
		}
		if closed := a.evaluate(command); closed.Result.Code != cs.OK {
			t.Fatal("cannot close refusal session", closed.Result)
		}
	}
	if a.meta.Ceiling != original {
		t.Fatal("content ceiling changed")
	}
	var size uint64
	for k, b := range a.rows {
		size += cs.RecordBytes(k, b)
	}
	// The bounded result headers and membership initialization fit within one
	// existing evaluation envelope plus one command envelope per live session.
	if size > original+cs.MaxEvaluationBytes+cs.MaxLiveSessions*cs.MaxCommandBytes {
		t.Fatalf("bookkeeping grew to %d", size)
	}
	extra := a.prepareBare("b", 1000, cs.Mutation{AdvanceTime: &cs.AdvanceTime{}})
	if a.evaluate(extra).Result.Code != cs.Capacity {
		t.Fatal("retained session cap changed")
	}
}
