package stream

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func TestFourChatsBirthAPlaceAndThreeDoNot(t *testing.T) {
	t.Parallel()
	var inbox []placeorg.Arrival
	for i := 0; i < 4; i++ {
		inbox = append(inbox, arrival(string(rune('a'+i)), []float32{1, 0}))
	}
	for i := 0; i < 3; i++ {
		inbox = append(inbox, arrival(string(rune('x'+i)), []float32{0, 1}))
	}
	ops, err := New().Births(context.Background(), placeorg.NewTree(), inbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Kind != placeorg.OpBirth || !ops[0].Proposed || len(ops[0].Members) != 4 {
		t.Fatalf("ops %+v", ops)
	}
	for _, id := range ops[0].Members {
		if id[0] == 'x' {
			t.Fatalf("a chat from the other group was included: %v", ops[0].Members)
		}
	}
}

func TestAProbeDoesNotCountTowardABirth(t *testing.T) {
	t.Parallel()
	var inbox []placeorg.Arrival
	for i := 0; i < 4; i++ {
		inbox = append(inbox, arrival(string(rune('a'+i)), []float32{1, 0}))
	}
	s := New()
	s.Kinds = map[string]placeorg.Kind{"a": placeorg.KindProbe}
	ops, err := s.Births(context.Background(), placeorg.NewTree(), inbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 0 {
		t.Fatalf("a probe made a place: %+v", ops)
	}
}

func TestTheGardenChatsBirthOnePlaceAndThreeDoNot(t *testing.T) {
	chats := loadRealChats(t)
	vecs := loadGardenVectors(t)
	var inbox []placeorg.Arrival
	group := map[string]string{}
	for _, c := range chats {
		v, ok := vecs[c.ID]
		if !ok {
			t.Fatalf("no vector for %s", c.ID)
		}
		inbox = append(inbox, placeorg.Arrival{
			Chat:   placeorg.Chat{ID: c.ID, Title: c.Title, Replies: 1},
			Vector: v,
		})
		group[c.ID] = c.Group
	}
	ops, err := New().Births(context.Background(), placeorg.NewTree(), inbox)
	if err != nil {
		t.Fatal(err)
	}
	var garden, wifi int
	var singles int
	born := map[string]bool{}
	for _, op := range ops {
		if op.Kind != placeorg.OpBirth || !op.Proposed || op.Name != "" || op.Parent != "" {
			t.Fatalf("op %+v", op)
		}
		gs := map[string]int{}
		for _, id := range op.Members {
			gs[group[id]]++
			born[id] = true
		}
		if gs["garden"] == 5 && len(op.Members) == 5 {
			garden++
		}
		if gs["wifi"] == 5 && len(op.Members) == 5 {
			wifi++
		}
	}
	if garden != 1 || wifi != 1 || len(ops) != 2 {
		t.Fatalf("garden %d wifi %d ops %d", garden, wifi, len(ops))
	}
	for _, c := range chats {
		if c.Group == "single" {
			singles++
			if born[c.ID] {
				t.Fatalf("single chat %s was born into a place", c.ID)
			}
		}
	}
	if singles != 3 {
		t.Fatalf("singles %d", singles)
	}
}

type realChat struct {
	Group     string `json:"group"`
	ID        string `json:"chatId"`
	Title     string `json:"title"`
	Line      string `json:"line"`
	Discussed string `json:"discussed"`
}

func loadRealChats(t *testing.T) []realChat {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "placegraph", "testdata", "real-chats-2026-10-09.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Chats []realChat `json:"chats"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	return file.Chats
}

func loadGardenVectors(t *testing.T) map[string][]float32 {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "garden-emb.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		IDs     []string    `json:"ids"`
		Vectors [][]float64 `json:"vectors"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.IDs) != len(file.Vectors) {
		t.Fatalf("ids %d vectors %d", len(file.IDs), len(file.Vectors))
	}
	out := map[string][]float32{}
	for i, id := range file.IDs {
		v := make([]float32, len(file.Vectors[i]))
		for j, x := range file.Vectors[i] {
			v[j] = float32(x)
		}
		out[id] = v
	}
	return out
}
