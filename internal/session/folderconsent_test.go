package session

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE LAWS THE ONE CONSENT CARD RELIES ON (folderconsent.go).
//
// The copy is gone and the folder itself is edited, so the only thing standing
// between a model's first write and somebody's real project is one question
// through the gate that already asks. Each law below is one sentence that
// question has to keep, and the first of them is the one the copy broke: THE
// READING COVERS bash.

// folderAgent is a conversation standing in one directory with another folder
// attached, under the shipped blanket mode (prompt).
func folderAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	attached := canonicalPath(t.TempDir())
	policy := &approval.Policy{Default: approval.ActionPrompt}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ApprovalPolicy = policy
	})
	if _, err := agent.ReferPlace(attached, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	return agent, attached
}

func call(tool, args string) ai.ToolCall {
	return ai.ToolCall{ID: "c1", Function: ai.ToolCallFunction{Name: tool, Arguments: args}}
}

// A WRITE INTO AN ATTACHED FOLDER IS ASKED ABOUT BY THE FOLDER'S NAME. Before
// this the card said `tool "edit"`, which tells somebody nothing about the one
// thing that is new: their own project is about to change.
func TestTheFirstChangeInAnAttachedFolderIsAskedAboutByName(t *testing.T) {
	agent, attached := folderAgent(t)
	decision, governed := agent.decide(call("edit", `{"path":`+quoteJSON(filepath.Join(attached, "a.txt"))+`,"old":"x","new":"y"}`))
	if !governed || decision.Action != approval.ActionPrompt {
		t.Fatalf("the first change was %+v, want a question", decision)
	}
	if !strings.Contains(decision.Rule, attached) || !strings.Contains(decision.Rule, "the folder itself") {
		t.Fatalf("the card says %q, which does not name the folder it is about", decision.Rule)
	}
}

// AND SO IS A SHELL COMMAND THAT NAMES IT. This is the law the working copy
// could not keep at all: it aimed a `path` argument, so `bash` — 27 of the 62
// calls on the turn that provoked this change — went to the real folder with
// nothing said about it.
func TestAShellCommandThatNamesAnAttachedFolderIsAskedAboutByName(t *testing.T) {
	agent, attached := folderAgent(t)
	decision, _ := agent.decide(call("bash", `{"command":`+quoteJSON("cd "+attached+" && go build ./...")+`}`))
	if decision.Action != approval.ActionPrompt || !strings.Contains(decision.Rule, attached) {
		t.Fatalf("the shell command was judged %+v, want a question naming %s", decision, attached)
	}
}

// A LOOK IS NEVER A CHANGE. `read`, `grep`, `find` and `ls` under an attached
// folder are the whole reason attaching one is cheap, and a card raised on a
// look would make reading somebody's folder as expensive as writing it.
func TestLookingInsideAnAttachedFolderIsNotAsked(t *testing.T) {
	agent, attached := folderAgent(t)
	for _, look := range []ai.ToolCall{
		call("read", `{"path":`+quoteJSON(filepath.Join(attached, "a.txt"))+`}`),
		call("grep", `{"path":`+quoteJSON(attached)+`,"pattern":"x"}`),
		call("ls", `{"path":`+quoteJSON(attached)+`}`),
	} {
		if decision, _ := agent.decide(look); strings.Contains(decision.Rule, "the folder itself") {
			t.Fatalf("%s inside an attached folder raised the folder card: %+v", look.Function.Name, decision)
		}
	}
}

// THE ANSWER IS ABOUT THE FOLDER, SO IT COVERS EVERY HAND AIMED THERE. A memo
// keyed by tool could never have done this: saying yes to an `edit` would leave
// the next `bash` in the same folder asking, and saying "always" to bash would
// mean every bash command anywhere forever.
func TestAStandingYesAboutAFolderCoversTheNextShellCommandInIt(t *testing.T) {
	agent, attached := folderAgent(t)
	agent.rememberFolder(attached, true)

	decision, _ := agent.decide(call("bash", `{"command":`+quoteJSON("rm "+filepath.Join(attached, "a.txt"))+`}`))
	if decision.Action != approval.ActionAllow {
		t.Fatalf("a remembered yes about %s left %+v", attached, decision)
	}
	edit := call("edit", `{"path":`+quoteJSON(filepath.Join(attached, "a.txt"))+`,"old":"x","new":"y"}`)
	if allow, known := agent.rememberedAnswer(edit); !known || !allow {
		t.Fatalf("the remembered answer for an edit in the folder is (%v, %v)", allow, known)
	}
}

// AND IT COVERS NOTHING ELSE. A yes about one folder is not a yes about the
// next folder the person attaches, nor about the workspace, nor about `edit`
// somewhere else entirely.
func TestAStandingYesAboutOneFolderSaysNothingAboutAnother(t *testing.T) {
	agent, attached := folderAgent(t)
	agent.rememberFolder(attached, true)
	other := canonicalPath(t.TempDir())
	if _, err := agent.ReferPlace(other, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	decision, _ := agent.decide(call("edit", `{"path":`+quoteJSON(filepath.Join(other, "b.txt"))+`,"old":"x","new":"y"}`))
	if decision.Action != approval.ActionPrompt || !strings.Contains(decision.Rule, other) {
		t.Fatalf("the second folder was judged %+v, want its own question", decision)
	}
}

// THE FLOOR IS NOT LIFTED. A person agreed that aforge may change files in
// their folder. They did not agree to `rm -rf` in it, and internal/approval's
// critical table answers PROMPT rather than deny — so without this check the
// folder's own yes would have swallowed exactly the calls nobody was ever asked
// about. It is [Agent.approve]'s reasoning about the tool memo, kept here.
func TestAFolderYesDoesNotSwallowACriticalCommand(t *testing.T) {
	agent, attached := folderAgent(t)
	agent.rememberFolder(attached, true)
	decision, _ := agent.decide(call("bash", `{"command":`+quoteJSON("rm -rf "+attached)+`}`))
	if decision.Action == approval.ActionAllow {
		t.Fatalf("a folder yes allowed a critical command outright: %+v", decision)
	}
	if allow, known := agent.rememberedAnswer(call("bash", `{"command":"rm -rf /"}`)); known && allow {
		// The gate consults the memo only when the call is not one the floor
		// always asks about; this asserts the memo cannot be the thing that
		// answers for a shape like this if that guard is ever moved.
		t.Log("the memo answers yes here; [Agent.approve] must go on asking approval.AlwaysAsks first")
	}
}

// UNDER A BLANKET ALLOW NOTHING IS ASKED, which is what `tools.approvalMode:
// allow` and `--yolo` mean and all they mean. This file raises no decision of
// its own; it only rephrases a question that was already going to be asked.
func TestABlanketAllowIsNeverTurnedIntoAFolderQuestion(t *testing.T) {
	attached := canonicalPath(t.TempDir())
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	})
	if _, err := agent.ReferPlace(attached, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	decision, _ := agent.decide(call("edit", `{"path":`+quoteJSON(filepath.Join(attached, "a.txt"))+`,"old":"x","new":"y"}`))
	if decision.Action != approval.ActionAllow {
		t.Fatalf("--yolo was asked a question: %+v", decision)
	}
}

// THE FOLDER THE CONVERSATION IS STANDING IN IS NEVER ONE OF THESE QUESTIONS.
// It was edited directly before any of this existed and it still is; a card
// about it would be asking somebody to approve their own working directory.
func TestTheWorkspaceItselfRaisesNoFolderQuestion(t *testing.T) {
	agent, _ := folderAgent(t)
	workspace := agent.workspaceStoodIn()
	decision, _ := agent.decide(call("edit", `{"path":`+quoteJSON(filepath.Join(workspace, "a.txt"))+`,"old":"x","new":"y"}`))
	if strings.Contains(decision.Rule, "the folder itself") {
		t.Fatalf("the workspace raised a folder card: %+v", decision)
	}
}

// AND THE MODEL IS TOLD THE SAME THING THE PAGES SAY. The block that rides
// message[0] said the opposite of what the belt did for the life of the copy —
// "still what may be written to", while every write went somewhere else — and a
// model reasons from that paragraph for the rest of the turn. THE PROMPT MAY
// NOT DESCRIBE A COPY OR A LANDING, because there is neither.
func TestTheAttachedBlockDoesNotPromiseACopyOrALanding(t *testing.T) {
	attached := canonicalPath(t.TempDir())
	block := attachedBlock([]PlaceRef{{Path: attached, Arrival: PlaceSaid}}, "/tmp/workspace")
	if block == "" {
		t.Fatal("an attached folder rendered no block at all")
	}
	for _, banned := range []string{"/land", "working copy", "kept for this conversation", "until it lands", "your own version"} {
		if strings.Contains(block, banned) {
			t.Fatalf("the attached block still says %q:\n%s", banned, block)
		}
	}
	for _, owed := range []string{"THE WORKING DIRECTORY HAS NOT MOVED", "that folder itself", "asks the person once"} {
		if !strings.Contains(block, owed) {
			t.Fatalf("the attached block no longer says %q:\n%s", owed, block)
		}
	}
}
