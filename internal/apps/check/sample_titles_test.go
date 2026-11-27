package check

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestCheckRejectsNumberedPlaceholderTitles(t *testing.T) {
	for _, title := range []string{"Title example 2", "SAMPLE-37", "Customer Test 008", "placeholder 3 5", "Lorem ipsum 12"} {
		def := schema.Definition{Files: map[string][]byte{
			schema.AppFile:    []byte(`{"id":"work","version":1,"kit":2,"name":"Work","records":{"task":{"title":"subject","fields":{"subject":{"type":"text"}}}},"capabilities":{"records":{"task":"rw"}}}`),
			schema.SampleFile: []byte(`{"records":{"task":[{"id":"00000000000000000000000001","subject":"` + title + `"}]}}`),
		}}
		findings := sampleTitleFindings(def)
		if len(findings) != 1 || findings[0].Path != "/records/task/0/subject" || findings[0].Code != "placeholder_title" {
			t.Fatalf("title %q produced %v", title, findings)
		}
	}
	for _, title := range []string{"Blue Harbor | Invoice 318", "Test the new access flow", "Quarterly venue review"} {
		if PlaceholderTitle(title) {
			t.Fatalf("believable domain title was rejected: %q", title)
		}
	}
}
