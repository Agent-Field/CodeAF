package placeorg

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/roles"
)

// The roles this package asks through the engine's role door. They are
// registered by the engine (internal/session) at the low tier, the same way
// RolePlaceFile and RolePlaceSuggest are, so the person's settings page shows
// them and a per-role pin works. The desktop pins the low tier to the Flash
// model named in desktop/docs/DESIGN-QUESTIONS.md D4.
const (
	// RolePlaceName names one place from candidates and a few member titles.
	RolePlaceName roles.Role = "placename"
	// RolePlaceCharter writes one line saying what a top-level place is
	// about, which the name question then reads.
	RolePlaceCharter roles.Role = "placecharter"
	// RolePlaceFacet answers whether a group is about one thing or one kind
	// of activity, which decides whether it is a place or a lens.
	RolePlaceFacet roles.Role = "placefacet"
	// RolePlaceReview reads the whole outline once and offers renames.
	RolePlaceReview roles.Role = "placereview"
	// RolePlaceSteer turns a sentence into one steering action.
	RolePlaceSteer roles.Role = "placesteer"
	// RolePlaceKind labels chats with a kind of work for a steer by kind.
	RolePlaceKind roles.Role = "placekind"
)

// Option is one thing a model may point at. Key is the label it answers with
// (p1, c3, THING); Text is what it is shown. Neither is ever a real id.
type Option struct {
	Key  string
	Text string
}

// Question is one judgment point. Options is the whole option space; the
// model cannot add to it. Shape says what a Write answer must look like.
type Question struct {
	Role roles.Role
	// Purpose is one sentence a reader of the journal understands.
	Purpose string
	// Context is what the model is shown besides the options: titles, a
	// charter, the taken names. It is bounded by the stage that builds it.
	Context string
	Options []Option
	// MaxTokens bounds the answer; zero means the role's default.
	MaxTokens int
}

// Pick is what came back. FellBack says the fallback is the answer, and Why
// says why: no model, timed out, over budget, answer outside the contract.
type Pick struct {
	Key      string
	Written  string
	Model    string `json:"Model,omitempty"`
	FellBack bool
	Why      string
	CostUSD  float64
}

// Validator holds a written answer to a contract and returns the cleaned
// text, or false when the answer is unusable.
type Validator func(raw string) (string, bool)

// Judge is the ONLY way a stage reaches a model, and it cannot fail. Choose
// returns an Option that was in q.Options, or the fallback; Write returns text
// the validator accepted, or the fallback. Every call is journaled with its
// question, its options, what was picked, what it cost and whether the
// fallback was used. The engine supplies the model half through
// placegraph.Asker; the Runner wraps it with the budget and the journal.
type Judge interface {
	Choose(ctx context.Context, q Question, fallback Option) Pick
	Write(ctx context.Context, q Question, valid Validator, fallback string) Pick
}

// Fallback documents one deterministic path a stage takes when a Judge falls
// back. The Runner writes every stage's fallbacks into the journal at the
// start of a run, so the record of a run states its own degraded behaviour.
type Fallback struct {
	// When names the judgment point ("naming a place").
	When string
	// Does names the deterministic answer ("the top c-TF-IDF candidate").
	Does string
}

// Judged is implemented by every stage that holds a Judge. The structural test
// fails any package under placeorg that references Judge without declaring
// conformance to this interface, so a stage cannot ask a model without
// stating, in code, what it does when the model does not answer.
type Judged interface {
	Fallbacks() []Fallback
}

// Budget bounds one run's spend. When Calls or USD is reached the Judge
// answers every further question with its fallback and the journal says
// "over budget"; the run still completes.
type Budget struct {
	Calls int
	USD   float64
}

// Spent is what a run has used so far, published with progress.
type Spent struct {
	Calls     int
	USD       float64
	Fallbacks int
}
