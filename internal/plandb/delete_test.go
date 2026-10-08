package plandb

import "testing"

func TestDeleteCancelsHardDependentsButKeepsSuggestedWork(t *testing.T) {
	s := planOpen(t, "")
	planAdd(t, s,
		TaskSpec{ID: "source", Title: "Source"},
		TaskSpec{ID: "hard", Title: "Hard", Dependencies: []Dependency{{TaskID: "source", Kind: DepBlocks}}},
		TaskSpec{ID: "later", Title: "Later", Dependencies: []Dependency{{TaskID: "hard", Kind: DepBlocks}}},
		TaskSpec{ID: "soft", Title: "Soft", Dependencies: []Dependency{{TaskID: "source", Kind: DepSuggests}}},
	)
	if _, err := s.DeleteSubtree("source"); err != nil {
		t.Fatal(err)
	}
	if s.Task("source") != nil {
		t.Fatal("deleted prerequisite retained")
	}
	for _, id := range []string{"hard", "later"} {
		if task := s.Task(id); task == nil || task.Status != StatusCancelled || task.Error != "dependency was deleted" {
			t.Fatalf("%s was allowed to run without its prerequisite: %+v", id, task)
		}
	}
	if task := s.Task("soft"); task == nil || terminal(task.Status) || len(task.Dependencies) != 0 {
		t.Fatalf("advice cancelled independent work: %+v", task)
	}
	if _, err := s.Claim("soft", "worker"); err != nil {
		t.Fatalf("independent work cannot start: %v", err)
	}
}
