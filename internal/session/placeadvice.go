package session

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// Offers about places: which existing place a chat belongs in, and what a
// group of chats in no place should be called.
//
// THE QUESTIONS ARE internal/placegraph's AND THE DOOR IS THIS PACKAGE'S. The
// recommender decides when to ask, with which labelled candidates, and checks
// every answer against its own contract before anything is offered; this file
// only carries the question through [Agent.callRoleChecked], so the model is
// the role's (its pin, then its tier, then this conversation's model), and the
// call is journaled and billed like every other errand. No second provider
// client exists for places, and none may.
//
// IT IS ONE ASK WITH NO RETRIES OF ITS OWN, for the team namer's reason
// (teamname.go): the caller already holds what rules alone decided, and an
// answer that is slow, empty or malformed is simply not used.

// REGISTERING HERE IS WHAT MAKES THE DESKTOP'S "Chat filing" AND "Place
// suggestions" ROWS LIVE (internal/config's DesktopRoleLive). internal/roles
// deliberately leaves both unregistered so that a build with no caller says
// the job is not in use rather than offering a model for a call nothing makes.
// Both sit LOW: a person approves every offer, so a wrong answer costs one
// declined line and never a filed chat or a created place.
func init() {
	roles.Register(roles.RolePlaceFile, roles.TierLow)
	roles.Register(roles.RolePlaceSuggest, roles.TierLow)
}

// The journal tags for the two place errands. They live here rather than
// beside auxRoleTitle in loop.go for jobname.go's reason — this file owns the
// calls — and they are the same words the roles are registered under, which
// is what cmd/codeaf-replay's request map reads them back as.
const (
	auxRolePlaceFile    = "placefile"
	auxRolePlaceSuggest = "placesuggest"
)

// placesAnswerCap bounds the answer handed back. Both questions are answered
// with a handful of short labels and at most one short name, so anything near
// this size is a model that has stopped answering the question; the parser in
// internal/placegraph refuses it either way, and the cap only keeps a runaway
// answer from crossing the wire and the desktop bridge whole.
const placesAnswerCap = 16 << 10

// errNotAPlacesQuestion is the refusal for a role that is not one of the two
// place roles. THE SEAM CANNOT BE USED TO MAKE AN ARBITRARY ERRAND: a caller
// that could name any role here could spend any tier's money on any prompt,
// from a door that was only ever meant to file chats.
var errNotAPlacesQuestion = errors.New("not a places question")

// PlacesAnswer is one place question's raw answer and the model that gave it.
// Model is the model that ACTUALLY answered — the role's pin or tier, or this
// conversation's model when neither is set — so a surface can say and price
// what was asked, rather than guessing from settings that may have changed.
type PlacesAnswer struct {
	Text  string `json:"text"`
	Model string `json:"model,omitempty"`
}

// AskPlaces asks one internal/placegraph question on its own role and returns
// the raw answer for placegraph to read. It makes one call, which the caller
// bounds with ctx, billed off every turn's clock against the model that
// answered. It refuses, without a call, a role that is not one of the two
// place roles, a question with no system or user text, and a closed
// conversation.
func (a *Agent) AskPlaces(ctx context.Context, req placegraph.ModelRequest) (PlacesAnswer, error) {
	var tag string
	switch req.Role {
	case roles.RolePlaceFile:
		tag = auxRolePlaceFile
	case roles.RolePlaceSuggest:
		tag = auxRolePlaceSuggest
	default:
		return PlacesAnswer{}, errNotAPlacesQuestion
	}
	if strings.TrimSpace(req.System) == "" || strings.TrimSpace(req.User) == "" {
		return PlacesAnswer{}, errors.New("a places question needs both its instructions and its question")
	}
	a.mu.Lock()
	model, closed := a.model, a.closed
	a.mu.Unlock()
	if closed {
		return PlacesAnswer{}, errors.New("the conversation is closed")
	}
	response, named, err := a.callRoleChecked(withDetachedUsage(ctx), req.Role, model,
		[]ai.Message{textMessage("system", req.System), textMessage("user", req.User)}, nil)
	if err != nil {
		return PlacesAnswer{}, err
	}
	if response == nil {
		return PlacesAnswer{}, errEmptyAnswer
	}
	a.addDetachedUsageAs(response, named, 1, tag)
	return PlacesAnswer{Text: capPlacesAnswer(response.Text()), Model: named}, nil
}

// capPlacesAnswer cuts text to [placesAnswerCap] bytes on a rune boundary, so
// what is handed on is still valid UTF-8 for the JSON it is about to become.
func capPlacesAnswer(text string) string {
	if len(text) <= placesAnswerCap {
		return text
	}
	cut := placesAnswerCap
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}
