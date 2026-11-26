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
		{ID: "p1", Record: first, Revision: 1, Set: map[string]any{"likely": true}},
		{ID: "p2", Record: second, Revision: 2, Set: map[string]any{"likely": true}},
	}
	if err := s.ReviewAI(person, "queue", typeName, "accept", proposals); err == nil {
		t.Fatal("accepted a stale batch")
	}
	got, err := s.Get(person, typeName, first, false)
	if err != nil || got["likely"] == true {
		t.Fatalf("first record was changed: %v, %v", got, err)
	}
}
