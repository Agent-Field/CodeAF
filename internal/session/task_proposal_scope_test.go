package session

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// L-LOOP2 C1a, C6: admission quotes, evidence and journal pointers cannot
// reintroduce the conversation through the root, a child or a check's opening.
func TestProposedRunDocumentsExcludeEveryConversationContextSection(t *testing.T) {
	spec := taskSpec{
		title: "Create red.txt", brief: "Write red into red.txt",
		deliverable: "red.txt", acceptance: "red.txt contains red",
		request: "call propose_task three times: Create red.txt, Create green.txt, Create blue.txt",
		origin:  taskOrigin{journal: "conversation-original.jsonl", line: 12},
		admission: AdmissionContext{
			Version:  AdmissionContextVersion,
			Quotes:   []AdmissionQuote{{Speaker: admissionPerson, Text: "Create green.txt", Source: "conversation-quotes.jsonl"}},
			Evidence: []AdmissionHandle{{Tool: "bash", Input: "Create blue.txt", Source: "conversation-evidence.jsonl", Result: "conversation-result.txt"}},
		},
	}
	description := proposalBrief(spec, nil)
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the run", "root", spec.title, description)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	children, err := store.AddMany([]plandb.TaskSpec{{ID: "child", Title: "Write red bytes", Description: "Write red into red.txt"}, {ID: "check", Title: "Check red bytes", Description: "Read red.txt", Role: plandb.RoleCheck}})
	if err != nil {
		t.Fatal(err)
	}
	documents := []string{description, store.Task("root").Description}
	for _, task := range append(children, store.Task("root")) {
		documents = append(documents, BeltWorkerBrief(store, task, task.ID == "root", true, "Children finished their assigned work.", "MEMORY\nKeep project edits small."))
	}
	for _, doc := range documents {
		for _, forbidden := range []string{"propose_task", "three times", "Create green.txt", "Create blue.txt", "conversation-original.jsonl", "conversation-quotes.jsonl", "conversation-evidence.jsonl", "conversation-result.txt"} {
			if strings.Contains(doc, forbidden) {
				t.Errorf("conversation context %q leaked:\n%s", forbidden, doc)
			}
		}
		if !strings.Contains(doc, "Write red into red.txt") {
			t.Errorf("assignment missing:\n%s", doc)
		}
	}
}
