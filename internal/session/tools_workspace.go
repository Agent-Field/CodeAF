package session

// tools_workspace.go hands the belt the four verbs that act on the WORKSPACE'S
// OWN HISTORY — the files, as they were — rather than on the conversation.
//
// THEY ARE furrow's, AND furrow IS SOMEBODY ELSE'S PROGRAM. It is a separate
// tool the person installs themselves (Agent-Field/furrow, Apache-2.0), it is
// written in another language, and the only thing this tree does with it is run
// its command line. internal/furrow owns every fact about that; this file owns
// one decision, which is whether the verbs are on the belt at all.
//
// AND THAT DECISION IS THE ABSENCE LAW, IN ITS SHARPEST FORM. A model told it
// can restore a workspace will plan a whole reply around being able to — it
// will offer to try something risky, promise the person their .env is
// recoverable, and only discover at the call that nothing is there. So a
// machine with no furrow on it, or a folder furrow is not watching, is handed
// NO VERB: [furrow.Tools] answers nil and this appends nothing. There is no
// second check to remember and no tool that exists in order to fail.
//
// WHY THE NAMES ARE `workspace_` AND NOT `rewind_`. aforge already has a
// rewind, and it is an edit of the CONVERSATION that touches no file on disk
// (rewind.go). These four move bytes. Two families that both meant "put it
// back how it was" would be one word doing two jobs, and the first person to
// confuse them would lose work — so the two share no vocabulary at all.

import (
	"context"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/furrow"
)

// workspaceTools is the family, or nothing.
//
// THE DETECTION IS PAID HERE AND NOT ON THE DRAW PATH. This runs once per belt
// assembly — a turn, not a frame — and internal/furrow keeps its own answer for
// a while behind that, so the cost is one short-lived subprocess per workspace
// per half-minute rather than one per question the screen asks. A seam that
// shelled out while a terminal was repainting would be the surface stopping to
// ask another program a question, which is the mistake host.go's standing seam
// already documents at length.
func (a *Agent) workspaceTools() []bare.Tool {
	return furrow.Tools(context.Background(), a.config.Workspace)
}
