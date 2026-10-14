package agent

import (
	"errors"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

// deniedNote answers a permission ask the moment it is raised. The dock has no
// card on which a person could grant a tool the app does not declare, so a
// turn that waited for one would hang; the model is told it is refused and to
// say so in one line instead. Connector approvals never come through here:
// they open an approval item and succeed.
const deniedNote = `Not allowed in this app. Do not retry it or ask for permission. ` +
	`Tell the person in one line what you could not do, for example "I can't run shell commands in this app."`

// isDenied reports a permission refusal: a tool off the belt or a call the
// capability checker refused.
func isDenied(err error) bool {
	var typed *bridge.Error
	return errors.As(err, &typed) && typed.Code == bridge.CodeDenied
}

func deniedFeedback(err error) string {
	feedback, _ := toolFailure(err)
	return feedback + "\n" + deniedNote
}

// cannotLine ends a turn whose model keeps asking for refused tools, so the
// person still gets the one line the note asked for.
func cannotLine(tool string) string {
	return "I can't use " + humanTool(tool, "") + " in this app."
}
