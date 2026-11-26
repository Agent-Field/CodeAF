package data

import "testing"

func TestAIReviewRejectsAStaleBatchBeforeAnyWrite(t *testing.T) {
	s, _ := openCRM(t)
	typeName := "deal"
	person := Scope{}
	rows, err := s.Create(person, typeName, []map[string]any{{"name": "First"}, {"name": "Second"}})
	if err != nil {
		t.Fatal(err)
	}
	first := rows.Rows[0]["id"].(string)
	second := rows.Rows[1]["id"].(string)
	proposals := []AIEffect{
		{ID: "p1", Record: first, Revision: 1, Set: map[string]any{"likely": "Likely"}},
		{ID: "p2", Record: second, Revision: 2, Set: map[string]any{"likely": "Likely"}},
	}
	wantCode(t, s.ReviewAI(person, "queue", typeName, "accept", proposals), CodeStale)
	got, err := s.Get(person, typeName, first, false)
	if err != nil || got["likely"] != nil {
		t.Fatalf("first record was changed: %v, %v", got, err)
	}
	proposals[1].Revision = 1
	for range 2 {
		if err := s.ReviewAI(person, "queue", typeName, "accept", proposals); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{first, second} {
		got, err := s.Get(person, typeName, id, false)
		if err != nil || got["likely"] != "Likely" || got["_rev"] != int64(2) {
			t.Fatalf("accepted row = %v, %v", got, err)
		}
	}

}
