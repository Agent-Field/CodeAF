package exec

// The leaf's own closing, held to igel s14's shape.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	graphstore "github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// igelConfigs is the module igel's s14 run rewrote, minus everything that is
// not the point: three public names bound at the top level, which is exactly
// what `verify.PublicSurface` reads and exactly what that run deleted.
const igelConfigs = `init_file_path = "igel.yaml"
res_path = "results"
temp_post_req_data_path = "temp_post_req.json"
`

// igelConfigsWithoutTheName is what the run left behind: the module still there,
// the name gone, and every import of it broken.
const igelConfigsWithoutTheName = `def build_paths():
    init_file_path = "igel.yaml"
    res_path = "results"
    temp_post_req_data_path = "temp_post_req.json"
    return init_file_path, res_path, temp_post_req_data_path
`

// igelWorkspace stages that module in a workspace, with a store and a node to
// journal against.
func igelWorkspace(t *testing.T) (*Workspace, *graphstore.Store) {
	t.Helper()
	space := workspace(t)
	if err := os.WriteFile(filepath.Join(space.Root(), "configs.py"), []byte(igelConfigs), 0o644); err != nil {
		t.Fatal(err)
	}
	history, err := graphstore.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	if err := history.Splice(graphstore.RootID, graphstore.Subtree{Nodes: []graphstore.NodeSpec{{
		ID: "task-2", Brief: "persist the feature schema", Stage: 1,
	}}}, graphstore.Provenance{Origin: graphstore.OriginUser, Intent: "persist the feature schema"}); err != nil {
		t.Fatal(err)
	}
	return space, history
}

// selfCloses reads back every closing this run journaled.
func selfCloses(t *testing.T, history *graphstore.Store) []graphstore.LeafSelfClose {
	t.Helper()
	events, err := history.Events(0, 400)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	var closings []graphstore.LeafSelfClose
	for _, event := range events {
		if event.Kind != graphstore.EventLeafSelfClose {
			continue
		}
		var record graphstore.LeafSelfClose
		if err := json.Unmarshal(event.Payload, &record); err != nil {
			t.Fatalf("decode closing: %v", err)
		}
		closings = append(closings, record)
	}
	return closings
}

// sawTheNote reports whether the note ever reached the model, and what it said.
func sawTheNote(client *scriptedCompleter) (string, bool) {
	for _, messages := range client.seen {
		for _, message := range messages {
			for _, part := range message.Content {
				if message.Role == "user" && strings.Contains(part.Text, "Before this lands") {
					return part.Text, true
				}
			}
		}
	}
	return "", false
}

// A LEAF THAT DELETED A PUBLIC NAME IS ASKED ABOUT IT BEFORE ANYBODY BUYS A
// ROUND, AND WHEN IT PUTS THE NAME BACK IT LANDS CLEAN.
//
// This is igel s14 exactly, with the one thing that was missing. That run's
// repair leaf removed `init_file_path`, `res_path` and
// `temp_post_req_data_path` from `igel/configs.py`, landed, and the finding
// reached a gate — which bought another COLD leaf, which deleted something
// else. All twenty-four hidden tests failed on
// `cannot import name 'temp_post_req_data_path'`. The worker that could fix it
// for one turn was still standing when the photograph was taken.
func TestALeafRestoresThePublicNameItDeletedBeforeItLands(t *testing.T) {
	space, history := igelWorkspace(t)
	client := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("c1", "write", `{"path":"configs.py","text":`+jsonText(igelConfigsWithoutTheName)+`}`)},
		nil, // the leaf believes it is finished
		{call("c2", "write", `{"path":"configs.py","text":`+jsonText(igelConfigs)+`}`)},
	}}
	linear := NewLinear(client, space, nil, 10, 1_000_000, time.Minute).WithStore(history)
	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 1, StoreNodeID: "task-2", Brief: "rework configs.py so the schema persists",
	})
	if err != nil {
		t.Fatal(err)
	}

	note, asked := sawTheNote(client)
	if !asked {
		t.Fatal("the leaf deleted three public names and was never told before it landed")
	}
	for _, name := range []string{"init_file_path", "res_path", "temp_post_req_data_path"} {
		if !strings.Contains(note, name) {
			t.Errorf("the note does not name %s — a kind alone sends a worker to run a suite:\n%s",
				name, note)
		}
	}
	if !strings.Contains(note, "removed public names") {
		t.Errorf("the note does not say what the finding is:\n%s", note)
	}
	if len(outcome.Removed) != 0 {
		t.Fatalf("the leaf put the names back and still landed with a finding: %v", outcome.Removed)
	}
	if len(outcome.Standing) != 0 {
		t.Fatalf("the leaf settled its own finding and still landed holding it: %+v", outcome.Standing)
	}
	if outcome.Stop != StopDone {
		t.Fatalf("the leaf landed as %q — a close is not an ending", outcome.Stop)
	}
	if outcome.Verdict == "" {
		t.Error("the verdict was cleared for the close and never written again")
	}

	closings := selfCloses(t, history)
	if len(closings) != 1 || !closings[0].Closed {
		t.Fatalf("journaled closings = %+v, want one that was taken", closings)
	}
	if len(closings[0].Kinds) != 1 || closings[0].Kinds[0] != SelfCloseLostNames {
		t.Errorf("the row does not say what kind it was: %+v", closings[0])
	}
	if len(closings[0].Names) != 3 {
		t.Errorf("the row does not name what it was about: %+v", closings[0].Names)
	}
	if closings[0].Turns == 0 {
		t.Error("the row does not say how far the leaf had got when it read its own work")
	}
}

// AND A LEAF THAT CANNOT PUT IT BACK IS ASKED ONCE. The second reading is the
// floor: it lands, the finding rides the outcome, and the gate weighs it exactly
// as it did before this mechanism existed.
func TestALeafThatCannotCloseItsFindingLandsWithItAfterOneClose(t *testing.T) {
	space, history := igelWorkspace(t)
	client := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("c1", "write", `{"path":"configs.py","text":`+jsonText(igelConfigsWithoutTheName)+`}`)},
		nil, // finished, and asked
		nil, // still finished, and not asked again
	}}
	linear := NewLinear(client, space, nil, 10, 1_000_000, time.Minute).WithStore(history)
	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 1, StoreNodeID: "task-2", Brief: "rework configs.py so the schema persists",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, asked := sawTheNote(client); !asked {
		t.Fatal("the leaf was never told what its own reading found")
	}
	if len(outcome.Removed) == 0 {
		t.Fatal("the finding was closed out of existence rather than landed with")
	}
	closings := selfCloses(t, history)
	if len(closings) != 2 {
		t.Fatalf("journaled closings = %+v, want the close and the landing after it", closings)
	}
	if !closings[0].Closed {
		t.Errorf("the first row does not say the leaf was asked: %+v", closings[0])
	}
	if closings[1].Closed {
		t.Errorf("the leaf was asked twice about one kind: %+v", closings[1])
	}
	if !strings.Contains(closings[1].Why, "already closed") {
		t.Errorf("the floor row does not say why it stood: %q", closings[1].Why)
	}
}

// A LEAF WITH NOTHING LEFT OF ITS OWN METER LANDS WITH THE FINDING, and the row
// says which meter said no. There is no grant here that is not already the
// leaf's, so an exhausted leaf simply has none.
func TestASpentMeterLandsTheFindingWithNoClose(t *testing.T) {
	space, history := igelWorkspace(t)
	client := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("c1", "write", `{"path":"configs.py","text":`+jsonText(igelConfigsWithoutTheName)+`}`)},
	}}
	// Two turns granted and two turns taken: the write, and the answer.
	linear := NewLinear(client, space, nil, 2, 1_000_000, time.Minute).WithStore(history)
	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 1, StoreNodeID: "task-2", Brief: "rework configs.py so the schema persists",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, asked := sawTheNote(client); asked {
		t.Fatal("a leaf with no room left was handed more work")
	}
	if len(outcome.Removed) == 0 {
		t.Fatal("the finding did not reach the outcome")
	}
	if len(outcome.Standing) != 1 {
		t.Fatalf("the leaf landed holding %+v, want its one closing finding", outcome.Standing)
	}
	if fact := outcome.Standing[0].Fact; !strings.Contains(fact, "temp_post_req_data_path") ||
		!strings.Contains(fact, "removed public names") {
		t.Errorf("the standing finding does not name the measured fact: %q", fact)
	}
	closings := selfCloses(t, history)
	if len(closings) != 1 || closings[0].Closed {
		t.Fatalf("journaled closings = %+v, want one declined", closings)
	}
	if !strings.Contains(closings[0].Why, "turns were spent") {
		t.Errorf("the row does not name the meter that said no: %q", closings[0].Why)
	}
}

// AND THE ROOM IS WHAT THE LEAF HAS NOT SPENT, on every meter that bounds it and
// on no meter that does not.
func TestTheRoomForACloseIsWhatTheLeafHasLeft(t *testing.T) {
	spentTurns := &Outcome{Turns: 10, Usage: Usage{PromptTokens: 100}}
	if room := RoomLeft(spentTurns, 10, 1_000_000, time.Minute, time.Second); room.Left {
		t.Error("a leaf that used every turn it was granted was offered another")
	}
	if room := RoomLeft(spentTurns, 20, 1_000_000, time.Minute, time.Second); !room.Left ||
		room.Turns != 10 {
		t.Errorf("room = %+v, want the ten turns it did not use", room)
	}
	spentTokens := &Outcome{Turns: 2, Usage: Usage{PromptTokens: 900, CompletionTokens: 200}}
	if room := RoomLeft(spentTokens, 20, 1_000, time.Minute, time.Second); room.Left ||
		!strings.Contains(room.Why, "budget") {
		t.Errorf("room = %+v, want a spent budget", room)
	}
	// A BELT THAT DOES NOT METER SOMETHING IS NOT A LEAF THAT RAN OUT OF IT. The
	// bare loop has no turn cap and no token ceiling, and reading its zeros as
	// exhaustion would switch the whole mechanism off for that belt.
	unmetered := &Outcome{Turns: 40, Usage: Usage{PromptTokens: 500_000}}
	if room := RoomLeft(unmetered, 0, 0, time.Minute, time.Second); !room.Left {
		t.Errorf("room = %+v, want a belt with no turn or token meter to still have room", room)
	}
	if room := RoomLeft(unmetered, 0, 0, NoWall, 0); !room.Left {
		t.Errorf("room = %+v, want a loop with no clock at all to still have room", room)
	}
	if room := RoomLeft(unmetered, 0, 0, 30*time.Second, time.Minute); room.Left ||
		!strings.Contains(room.Why, "clock") {
		t.Errorf("room = %+v, want a clock inside its own landing reserve to say no", room)
	}
	// AND A LEAF THAT WAS TOLD TO LAND IS NOT OFFERED MORE WORK BY ARITHMETIC.
	told := &Outcome{Turns: 1, Exhausted: StopOverrun}
	if room := RoomLeft(told, 20, 1_000_000, time.Minute, time.Second); room.Left {
		t.Errorf("room = %+v, want a handed-back leaf to have none", room)
	}
}

// THE FOUR FINDINGS A LEAF CAN RAISE AGAINST ITSELF ARE THE FOUR THE GATE RAISES
// WITH NO CITATION TO WEIGH, and each is named out loud rather than described.
func TestEveryFindingALeafRaisesAgainstItselfNamesWhatItIsAbout(t *testing.T) {
	found := SelfCloseFindings(&Outcome{
		Removed:    []string{"results_path"},
		Unbound:    []string{"temp_post_req_data_path (igel/servers/fastapi_server.py:1, imported)"},
		OwnFailing: []string{"test_fit"},
		Regressed:  []string{"test_predict"},
	})
	if len(found) != 4 {
		t.Fatalf("findings = %+v, want all four", found)
	}
	note := selfCloseNote(found)
	for _, name := range []string{"results_path", "temp_post_req_data_path", "test_fit", "test_predict"} {
		if !strings.Contains(note, name) {
			t.Errorf("the note does not name %s:\n%s", name, note)
		}
	}
	for _, finding := range found {
		if finding.Fact == "" || !strings.HasPrefix(finding.Sentence, finding.Fact+".") {
			t.Errorf("the instruction was not composed from its handover fact: %+v", finding)
		}
	}
	if !strings.Contains(note, "4 things") {
		t.Errorf("the note does not say how many findings there are:\n%s", note)
	}
	// NO CLAIM IS NOT A CLEAN BILL, and it is also not a finding: a worker with
	// nothing measured against it must never be held back.
	if found := SelfCloseFindings(&Outcome{Removed: []string{"", "  "}}); len(found) != 0 {
		t.Errorf("an empty reading raised %+v", found)
	}
	if found := SelfCloseFindings(nil); len(found) != 0 {
		t.Errorf("a leaf with no outcome raised %+v", found)
	}
}

// jsonText is JSON string quoting for the scripted tool arguments above.
func jsonText(text string) string {
	encoded, err := json.Marshal(text)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
