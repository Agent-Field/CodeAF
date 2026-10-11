package session

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/placeorg"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// Questions about filing, naming and organising places share one role door.
// Embeddings use the same account selection and detached billing seams.
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
// All sit LOW: a person approves every offer, so a wrong answer costs one
// declined line and never a filed chat or a created place.
func init() {
	roles.Register(roles.RolePlaceFile, roles.TierLow)
	roles.Register(roles.RolePlaceSuggest, roles.TierLow)
	for _, role := range organiserPlaceRoles {
		roles.Register(role, roles.TierLow)
	}
}

// organiserPlaceRoles keeps the registration and allow-list on the same contract.
var organiserPlaceRoles = []roles.Role{
	placeorg.RolePlaceName, placeorg.RolePlaceCharter, placeorg.RolePlaceFacet,
	placeorg.RolePlaceReview, placeorg.RolePlaceSteer, placeorg.RolePlaceKind,
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

// errNotAPlacesQuestion refuses roles outside the places allow-list.
// THE SEAM CANNOT BE USED TO MAKE AN ARBITRARY ERRAND: a caller
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

// AskPlaces asks one places question on its own role and returns
// the raw answer for placegraph to read. It makes one call, which the caller
// bounds with ctx, billed off every turn's clock against the model that
// answered. It refuses, without a call, a role that is not an allowed
// place role, a question with no system or user text, and a closed
// conversation.
func (a *Agent) AskPlaces(ctx context.Context, req placegraph.ModelRequest) (PlacesAnswer, error) {
	var tag string
	switch req.Role {
	case roles.RolePlaceFile:
		tag = auxRolePlaceFile
	case roles.RolePlaceSuggest:
		tag = auxRolePlaceSuggest
	default:
		for _, role := range organiserPlaceRoles {
			if req.Role == role {
				tag = string(role)
				break
			}
		}
		if tag == "" {
			return PlacesAnswer{}, errNotAPlacesQuestion
		}
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

// PlacesEmbeddingModel fixes the space independently of the conversation's model.
const PlacesEmbeddingModel = "openai/text-embedding-3-small"

// placesEmbedBatch bounds each charge and keeps cancellation between batches.
const placesEmbedBatch = 64

// EmbedPlaces embeds in one space and bills every completed batch off the turn.
// A failed later batch returns no vectors, but earlier paid calls stay recorded.
func (a *Agent) EmbedPlaces(ctx context.Context, texts []string) ([][]float32, error) {
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return nil, errors.New("the conversation is closed")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	client, model, _, err := a.completerFor(PlacesEmbeddingModel)
	if err != nil {
		return nil, err
	}
	door, ok := unwrapCompleter(client).(interface {
		Embed(context.Context, string, []string, int) ([][]float32, *ai.Response, error)
	})
	if !ok {
		return nil, errors.New("this provider cannot embed places")
	}
	ctx, cancel := context.WithTimeout(ctx, roles.PatienceFor(roles.RolePlaceFile))
	defer cancel()
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += placesEmbedBatch {
		end := min(start+placesEmbedBatch, len(texts))
		vectors, usage, err := door.Embed(ctx, model, texts[start:end], placeorg.PrimaryDims)
		if usage != nil {
			a.addDetachedUsageAs(usage, PlacesEmbeddingModel, 1, "placeembed")
		}
		if err != nil {
			return nil, err
		}
		out = append(out, vectors...)
	}
	return out, nil
}
