package serve

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A solo response drops the assignee fact with the other people fields.
func TestSoloJobDropsTheAssigneeFact(t *testing.T) {
	job := api.FromItem(factory.Item{ID: 1, Num: 1, Kind: factory.KindIssue, Assignee: "ann", SourceAssignees: []string{"ann", "bob"}})
	(&Handler{}).soloJob(&job, factory.Item{}, "ann")
	raw, _ := json.Marshal(job)
	if strings.Contains(string(raw), "ann") || strings.Contains(string(raw), "bob") || strings.Contains(string(raw), `"assignees"`) {
		t.Fatalf("people leaked: %s", raw)
	}
}
