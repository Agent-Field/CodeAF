package clientrun_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/directory"
	"golang.org/x/crypto/nacl/box"
)

func (e *engine) Entries(id, before string, limit int) ([]appwire.Entry, bool, error) {
	rows := make([]appwire.Entry, 60)
	for i := range rows {
		rows[i] = appwire.Entry{ID: fmt.Sprint(i), Agent: id, Kind: appwire.EntryAnswer, Text: strings.Repeat("x", e.entryBytes)}
	}
	start := max(0, len(rows)-limit)
	return rows[start:], start > 0, nil
}

func (g *rig) slot(t *testing.T, method string) []byte {
	t.Helper()
	var slot struct{ Sealed []byte }
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+clientrun.SlotName(method, g.s.dev.ID()), nil, &slot, 200)
	if len(slot.Sealed) > clientproto.MaxSlotBytes {
		t.Fatalf("sealed slot has %d bytes", len(slot.Sealed))
	}
	plain, ok := box.OpenAnonymous(nil, slot.Sealed, g.pub, g.priv)
	if !ok {
		t.Fatal("cannot open slot")
	}
	return plain
}

func TestEntrySnapshotsKeepNewestWholeEntriesWithinSlot(t *testing.T) {
	for _, size := range []int{10, 4000, 70000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			g := newRig(t)
			g.e.entryBytes = size
			_, err := g.r.Pair(context.Background(), clientrun.Pairing{Cert: g.client.Cert(), BoxKey: g.pub[:], Reads: []string{"agents.entries"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.r.Step(context.Background()); err != nil {
				t.Fatal(err)
			}
			var got []struct {
				Agent  string
				Result appwire.EntriesResult
			}
			if err := json.Unmarshal(g.slot(t, "agents.entries"), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Agent != chat {
				t.Fatalf("scope: %+v", got)
			}
			entries := got[0].Result.Entries
			if !got[0].Result.More {
				t.Fatal("missing older-entry marker")
			}
			if size == 10 && len(entries) != 40 {
				t.Fatalf("got %d entries, want 40", len(entries))
			}
			if size == 4000 && (len(entries) == 0 || len(entries) >= 40) {
				t.Fatalf("did not trim to slot: %d", len(entries))
			}
			if size == 70000 && len(entries) != 0 {
				t.Fatal("split oversized entry")
			}
			for i, row := range entries {
				if row.ID != fmt.Sprint(60-len(entries)+i) || len(row.Text) != size {
					t.Fatalf("not a whole newest suffix: %s (%d bytes)", row.ID, len(row.Text))
				}
			}
		})
	}
}

func (e *engine) ListTasks(agent string) ([]appwire.Task, error) {
	return []appwire.Task{{ID: chat + ":1", Agent: chat, Irreversible: e.taskIrreversible}, {ID: otherChat + ":1", Agent: otherChat}}, nil
}

func TestTaskSnapshotsPartitionRowsByHeldChat(t *testing.T) {
	g := newRig(t)
	_, err := g.r.Pair(context.Background(), clientrun.Pairing{Cert: g.client.Cert(), BoxKey: g.pub[:], Reads: []string{"tasks.list"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var got []struct {
		Agent  string
		Result appwire.TasksResult
	}
	if err := json.Unmarshal(g.slot(t, "tasks.list"), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Agent != chat || len(got[0].Result.Tasks) != 1 || got[0].Result.Tasks[0].ID != chat+":1" {
		t.Fatalf("unleased task leaked: %+v", got)
	}
}

func TestPhoneWritesPairAndRespectChatOwnership(t *testing.T) {
	for _, method := range []string{"inbox.answer", "tasks.start"} {
		t.Run(method, func(t *testing.T) {
			g := newRig(t)
			grant := g.pair(t, method)
			id := otherChat + ":1"
			if method == "inbox.answer" {
				id = otherChat + "/consent:1"
			}
			params, _ := json.Marshal(map[string]string{"id": id, "option": "allow", "batch_item": "safe"})
			payload, _ := json.Marshal(clientrun.Call{Method: method, Params: params})
			sealed, err := box.SealAnonymous(nil, payload, (*[32]byte)(grant.BoxKey), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			g.request(t, g.client, "POST", "/v1/clients/"+g.client.dev.ID()+"/queue", map[string]any{"number": 1, "at": g.now.UnixMilli(), "method": method, "target": g.s.dev.ID(), "sealed": sealed}, nil, 204)
			if err := g.r.Step(context.Background()); err == nil || !strings.Contains(err.Error(), "not leased") {
				t.Fatalf("wrong ownership refusal: %v", err)
			}
		})
	}
}

func (e *engine) AnswerInbox(id, option, text, why, scope string) (time.Time, error) {
	e.inboxAnswers++
	return time.Time{}, nil
}
func (e *engine) ActOnTask(id, action, verdict, text string) error { e.taskStarts++; return nil }

func TestPhoneInboxAndTaskWritesRequireFaceIDForIrreversibleWork(t *testing.T) {
	for _, method := range []string{"inbox.answer", "tasks.start"} {
		for _, faceID := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", method, faceID), func(t *testing.T) {
				g := newRig(t)
				g.e.asks = []appwire.Ask{{ID: "consent:1", Agent: chat, Irreversible: true}, {ID: "safe", Agent: chat}}
				g.e.taskIrreversible = true
				grant := g.pair(t, method)
				id := chat + ":1"
				if method == "inbox.answer" {
					id = chat + "/consent:1"
				}
				params, _ := json.Marshal(map[string]string{"id": id, "option": "allow", "batch_item": "safe"})
				payload, _ := json.Marshal(clientrun.Call{Method: method, Params: params, FaceID: faceID})
				sealed, err := box.SealAnonymous(nil, payload, (*[32]byte)(grant.BoxKey), rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				g.request(t, g.client, "POST", "/v1/clients/"+g.client.dev.ID()+"/queue", map[string]any{"number": 1, "at": g.now.UnixMilli(), "method": method, "target": g.s.dev.ID(), "sealed": sealed}, nil, 204)
				err = g.r.Step(context.Background())
				if faceID {
					if err != nil || g.e.inboxAnswers+g.e.taskStarts != 1 {
						t.Fatalf("Face ID call: %v, executions %d", err, g.e.inboxAnswers+g.e.taskStarts)
					}
				} else {
					if err == nil || !strings.Contains(err.Error(), "Face ID") || g.e.inboxAnswers+g.e.taskStarts != 0 {
						t.Fatalf("unconfirmed call: %v", err)
					}
				}
			})
		}
	}
}

func TestSnapshotsIncludeRealEngineChatClasses(t *testing.T) {
	g := newRig(t)
	if _, err := g.dir.Create(context.Background(), otherChat, directory.CellInit{Head: strings.Repeat("2", 64), Class: "host-bound"}); err != nil {
		t.Fatal(err)
	}
	g.pair(t, "asks.answer")
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var got []struct{ Agent string }
	if err := json.Unmarshal(g.slot(t, "asks.list"), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Agent != otherChat {
		t.Fatalf("real engine chat missing: %+v", got)
	}
}
