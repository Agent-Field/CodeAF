package session

// THE NODE ROAD IS NOT TOLD ABOUT A PLAN IT HAS NOT GOT.
//
// The digest and the paragraph that says what to do with it are the plan road's
// alone, and "alone" here has to be checked rather than reasoned about: both are
// rendered from [Config.oneTaskRoad], and a predicate that stopped being read
// would put a picture of rows in front of every sentence a node-road person
// types and tell the model to stop tasks it has no door onto.
//
// So this is the whole of what this change could do to that road, asserted on
// the three surfaces it touches: the block that opens a turn, the paragraph in
// the hand-off facts, and the stop the paragraph names.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

func TestTheNodeRoadIsToldNothingAboutARunsRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a",
		plandb.TaskSpec{ID: "2", Title: "Move the schema"},
	)

	// A STORE ARMED AND AN ENGINE WIRED, and the belt naming the node road. Two
	// of the three halves hold, so this fails the moment the belt stops being
	// one of them rather than only when a plan is absent.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	registerBeltRunEngine(t, &beltRunDouble{})
	armPlanStore(t, agent, path, "chat-a")
	t.Setenv("CODEAF_TASK_BELT", "node")

	if digest := agent.planDigest(); digest != "" {
		t.Fatalf("a node-road turn opened on a picture of rows:\n%s", digest)
	}

	// THE PARAGRAPH IS ABSENT FROM THE PAGE, not softened on it. The node road's
	// hand-off facts are a different fragment entirely, and a sentence telling a
	// model to stop a run's row would be a sentence about a door it has not got.
	page := renderSystem(agent.config)
	for _, unwanted := range []string{"YOUR MESSAGE FROM THE PERSON OPENS WITH ITS ROWS", planDigestHeading, planDigestRule} {
		if strings.Contains(page, unwanted) {
			t.Fatalf("the node road's page carries %q", unwanted)
		}
	}

	// AND THE STOP THE PARAGRAPH NAMES CLAIMS NOTHING HERE, so a token goes on to
	// the session tree's own reader exactly as it did before this change.
	if answer, ok := agent.stopPlanRow("2", ""); ok {
		t.Fatalf("a node-road stop was claimed by the run road: %q", answer)
	}
}

// AND THE PLAN ROAD GETS ALL THREE, so the test above is a statement about the
// road and not about a predicate that has stopped working everywhere.
func TestThePlanRoadGetsTheRowsThePageAndTheStop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "2", Title: "Move the schema"})
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	registerBeltRunEngine(t, &beltRunDouble{})
	armPlanStore(t, agent, path, "chat-a")

	if digest := agent.planDigest(); !strings.Contains(digest, "Move the schema") {
		t.Fatalf("the plan road's turn opened on no rows:\n%s", digest)
	}
	if page := renderSystem(agent.config); !strings.Contains(page, "YOUR MESSAGE FROM THE PERSON OPENS WITH ITS ROWS") {
		t.Fatal("the plan road's page does not say what to do with the rows it is given")
	}
	if _, ok := agent.stopPlanRow("2", ""); !ok {
		t.Fatal("the plan road's stop did not claim a row of its own run")
	}
}
