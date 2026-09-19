package tui3

import (
	"context"
	"strings"
)

// Collab is the home/chat seam onto Wave 3 coordination. It is a TUI
// interface so this package never imports internal/workspace or wsapi. The
// DTOs are exported so cmd/codeaf can implement the seam without this package
// importing wsapi, and without wsapi importing tui3. Wiring owns a separate
// adapter from Folders: messaging verbs must not grow on [Folders], or
// `var _ tui3.Folders` would demand them.
//
// NIL IS NO CHROME, NOT A BROKEN BUS. A door that could not bind the router
// leaves this nil; mark/coordinate verbs are absent, and natural-language
// "coordinate these" still works if session.Config.Collab is wired. A belt
// that painted dummy sent lines would be a capability advertised as broken.
//
// THE SNAPSHOT IS TAKEN ON THE HOME BEAT and after a collab mutation.
// View, the cursor and a mere rebuild read [app.collabView], which is a memo.
type Collab interface {
	Mark(ctx context.Context, refID string) error
	Unmark(ctx context.Context, refID string) error
	Marked(ctx context.Context) ([]CollabMark, error)
	CoordinateMarked(ctx context.Context, coordinatorID string) error
	Activity(ctx context.Context, coordinatorID string) ([]CollabActivity, error)
	Participants(ctx context.Context, discussionID string) ([]CollabParticipant, error)
}

// CollabMark is one conversation a person marked as a convenience. Marking is
// never required to coordinate; the primary path is saying "coordinate these".
type CollabMark struct {
	RefID, Title string
}

// CollabActivity is one visible sent/request/reply line. Kind is the
// person-facing word: request, reply, sent. Store words (accepted, recorded,
// processed) are never painted. SourceRef is the cited chat, drawn as a link.
type CollabActivity struct {
	DeliveryID, Pattern, Body, SourceRef string
	Kind, ToTitle                        string
}

// CollabParticipant is one labelled speaker on a joint discussion. The
// discussion itself is a normal chat; these labels are the only extra.
type CollabParticipant struct {
	ActorID, Role, SourceTitle string
}

// collabReading is the memo [app.readCollab] writes. View reads this and
// never the seam. A failed refresh keeps the last good snapshot (P12).
type collabReading struct {
	marks        []CollabMark
	activity     []CollabActivity
	participants []CollabParticipant
}

// Person-facing collab copy, quoted in the contract and the tests as these
// exact phrases. Delivery machinery words are not among them.
const (
	collabMarkWord       = "mark this chat"
	collabUnmarkWord     = "unmark this chat"
	collabCoordinateWord = "coordinate these"
	collabNeedChatWord   = "coordinate from this chat · or say coordinate these"
	collabMarkedWord     = "marked"
	collabRequestWord    = "request"
	collabReplyWord      = "reply"
	collabSentWord       = "sent"
	collabCouldNotMark   = "could not mark that chat"
	collabCouldNotUnmark = "could not unmark that chat"
	collabCouldNotCoord  = "could not coordinate these"
	collabNoStandWord    = "stand on a chat · then mark it"
	collabSourceWord     = "source"
)

// readCollab is the beat's coordination reading. Nil Collab is absence: the
// memo is empty and there is no chrome. A Marked failure keeps the last good
// marks so a delivery arriving cannot wipe the selection.
func (a *app) readCollab() {
	if a.collab == nil {
		a.collabView = collabReading{}
		a.home.folders.marked = nil
		return
	}
	a.readCollabMarks()
	a.readCollabChat()
}

func (a *app) readCollabMarks() {
	marks, err := a.collab.Marked(a.folderCtx())
	if err != nil {
		return
	}
	a.collabView.marks = marks
	a.home.folders.marked = marks
}

func (a *app) readCollabChat() {
	id := a.conversationRef()
	if id == "" {
		return
	}
	ctx := a.folderCtx()
	if acts, err := a.collab.Activity(ctx, id); err == nil {
		a.collabView.activity = acts
	}
	if parts, err := a.collab.Participants(ctx, id); err == nil {
		a.collabView.participants = parts
	}
}

func collabChatMarked(in *homeGridInput, refID string) bool {
	refID = strings.TrimSpace(refID)
	if refID == "" {
		return false
	}
	for _, mark := range in.folders.marked {
		if mark.RefID == refID {
			return true
		}
	}
	return false
}

func (a *app) collabMarked(refID string) bool {
	refID = strings.TrimSpace(refID)
	if refID == "" {
		return false
	}
	for _, mark := range a.collabView.marks {
		if mark.RefID == refID {
			return true
		}
	}
	return false
}
