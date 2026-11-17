package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A DEPENDANT STARTS WITH WHAT IT BUILDS ON (lane L7, DESIGN §11.1–§11.2). The
// work-loop audit watched every dependant of a launch plan spend four to seven
// calls finding the release notes. Here an agent's task without a brief is
// refused, a task it waits on becomes its input by itself, done without a
// hand-off note is refused, and the dependant's opening words carry the
// release notes' text and the note, cut to the budget when they are long.
func TestADependantStartsWithItsInputsWordsAndTheHandoff(t *testing.T) {
	o := newWSOrg(t)
	ws, _, _, _ := o.orgScene(t)
	ctx := context.Background()
	d := &agentDoor{h: o.h, id: ws.ID, asker: "mia"}
	work := func(tool, args string) map[string]any {
		t.Helper()
		line, err := d.Work(ctx, tool, json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s %s: %v", tool, args, err)
		}
		var out map[string]any
		_ = json.Unmarshal([]byte(line), &out)
		return out
	}
	refused := func(tool, args, want string) {
		t.Helper()
		if _, err := d.Work(ctx, tool, json.RawMessage(args)); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s %s = %v, want a refusal saying %q", tool, args, err, want)
		}
	}
	const brief = `"goal":"Tell subscribers what 2.4 brings","deliverable":"A document in Files","acceptance":["Leads with the shared shopping list","Under 200 words"]`

	refused("ws_task_create", `{"title":"Launch email","request":"Write it."}`, "goal, deliverable, acceptance")
	refused("ws_task_create", `{"title":"Launch email","request":"Write it.",`+brief+`,"inputs":[{"kind":"file","id":"fl_nope"}]}`, `no file "fl_nope"`)

	notes := work("ws_file_create", `{"name":"Release notes for the website","markdown":"# Pantry 2.4\n\nShared shopping lists, and recipe import no longer drops fractions."}`)
	notesID, _ := notes["file"].(map[string]any)["id"].(string)
	if notesID == "" {
		t.Fatalf("ws_file_create answered %v", notes)
	}
	made := work("ws_task_create", `{"title":"Release notes","request":"Write them.",`+brief+`}`)
	relID := made["task"].(map[string]any)["id"].(string)
	work("ws_task_update", `{"task":"`+relID+`","state":"running"}`)
	refused("ws_task_update", `{"task":"`+relID+`","state":"done","results":[{"file":"`+notesID+`"}]}`, "set handoff with done")
	work("ws_task_update", `{"task":"`+relID+`","state":"done","results":[{"file":"`+notesID+`"}],"check":"Both changes, plain words","handoff":"Notes cover the shared list and the import fix. Meal planning is left out on purpose."}`)

	email := work("ws_task_create", `{"title":"Launch email","request":"Write the launch email.",`+brief+`,"constraints":["No meal planning"],"waiting_on":["`+relID+`"]}`)
	emailID := email["task"].(map[string]any)["id"].(string)
	got, err := d.task(ctx, emailID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Brief == nil || len(got.Brief.Inputs) != 1 || got.Brief.Inputs[0] != (factory.TaskBriefInput{Kind: factory.TaskInputTask, ID: relID, Why: "waited on"}) {
		t.Fatalf("the task it waits on is not its input: %+v", got.Brief)
	}

	packet, used := o.h.taskPacket(ctx, d, emailID, "Launch email\n\nWrite the launch email.")
	for _, want := range []string{
		"Goal: Tell subscribers what 2.4 brings",
		"- Leads with the shared shopping list",
		"- No meal planning",
		`## Result of the task "Release notes" (waited on)`,
		"Its hand-off note: Notes cover the shared list and the import fix.",
		"## Its result file: Release notes for the website",
		"recipe import no longer drops fractions",
	} {
		if !strings.Contains(packet, want) {
			t.Errorf("the packet does not say %q:\n%s", want, packet)
		}
	}
	if !strings.Contains(strings.Join(used, "|"), "Release notes for the website") {
		t.Errorf("the packet names what it used as %v", used)
	}

	// A long input is cut to the budget, and says how to read on.
	long := strings.Repeat("A line of the research the team gathered.\n", 4000)
	big := work("ws_file_create", `{"name":"Research dump","markdown":`+quoteJSON(long)+`}`)
	bigID := big["file"].(map[string]any)["id"].(string)
	wide := work("ws_task_create", `{"title":"Summary","request":"Summarise it.",`+brief+`,"inputs":[{"kind":"file","id":"`+bigID+`","why":"what to summarise"}]}`)
	packet, _ = o.h.taskPacket(ctx, d, wide["task"].(map[string]any)["id"].(string), "Summary")
	if len(packet) > taskPacketBytes+4<<10 || !strings.Contains(packet, "[Cut here for length. ws_file_read "+bigID) {
		t.Fatalf("a long input was not cut to the budget: %d bytes", len(packet))
	}
}

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
