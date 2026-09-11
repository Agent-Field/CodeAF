package session

// standing_placement.go is the chat door's half of ongoing work that runs:
// which folder the work will be placed in, what the card says before the yes,
// and the placement written after the yes and before the item itself.
//
// ── ONE ITEM, TWO DOORS ──
//
// `aforge standing add --instructions … --report … --place <folder>` makes an
// item at the terminal (cmd/aforge's standing.go). A person who says "keep an
// eye on my inbox folder and keep reports/inbox-report.md current" in a
// conversation gets THE SAME ITEM: the same instructions version, the same
// owner-published report, the same placement, and so the same rules, the same
// check before publication and the same receipts. Nothing here runs anything;
// the pass and the runner are the terminal door's and the timer's
// (standing_run.go), so the two doors cannot come to run work differently.
//
// ── THE INTERACTION MAY PRECEDE ITS ORGANIZATION ──
//
// A folder is never demanded first. Work named into a folder goes there; work
// that names none goes where the conversation that asked for it is placed,
// because the rules the person is working under in that conversation are the
// rules they would expect the work to keep; and work said in a conversation
// that is in no folder is in none, and can be placed later. That default
// ADDS governance and never removes it: a placement grants no tool permission
// (collections' own law), it only lets that folder's rules reach the runs.
//
// ── THE CARD SAYS WHAT WILL GOVERN, FROM THE READING THE RUN WILL MAKE ──
//
// The rules a card quotes are resolved exactly as the run resolves them — the
// standing owner's [GoverningReader.ApplicableScope] over the item's workspace,
// its conversation and the folders a placement would bring
// ([workspace.Store.GoverningIfPlaced], the same walk the run's
// GoverningCollections takes), filtered by [GoverningRules]. A card that read
// rules some other way would be a promise about a check the run does not make.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// The words the card's terms and the tool's answer are spelled with, each
// ONCE. The manual quotes them exactly (internal/manual/chat/standing-orders.md).
const (
	standingDoesTag   = "does · "
	standingReportTag = "report · "
	standingFolderTag = "folder · "
	standingRuleTag   = "rule · "
	standingRulesTag  = "rules · "
	// standingReportWho is who writes the report, said on the card and in the
	// tool's answer: the run replies with it, and aforge is the only writer.
	standingReportWho = "aforge publishes this file; the run never writes it"
	// standingFolderReach is what a placement means to the person reading it.
	standingFolderReach = " — its rules reach every run"
	// standingFolderInherited says why a folder nobody named is on the card.
	standingFolderInherited = ", where this conversation is placed"
	// standingFolderNone is work in no folder, which is allowed and not a gap.
	standingFolderNone = "none — it can be placed in one later"
	// standingRulesNone is a placement, or none, that no rule reaches yet.
	standingRulesNone = "none reach this work yet"
)

// standingCardRules is how many rules a card quotes by their words before it
// counts the rest. A card is read before a yes, not audited; `aforge standing
// show` lists every one afterwards.
const standingCardRules = 5

// standingCardClip is how many bytes of the instructions or a rule's words one
// card line carries. The card is a summary the person checks; the whole text is on
// the item, and `aforge standing show` prints it.
const standingCardClip = 160

// standingPlacementCap bounds the folders one piece of work is placed in by
// default, at the same figure a rule's folder scope is bounded by.
const standingPlacementCap = 32

// standingPlacement is where a proposal's work will be placed, and why there.
type standingPlacement struct {
	folders []workspace.Collection
	// inherited says the folders are the conversation's own placement and
	// were not named in the call.
	inherited bool
}

// ids is the folders by id, in order.
func (p standingPlacement) ids() []string {
	out := make([]string, 0, len(p.folders))
	for _, folder := range p.folders {
		out = append(out, folder.ID)
	}
	return out
}

// names is the folders by name, as a person reads them.
func (p standingPlacement) names() string {
	out := make([]string, 0, len(p.folders))
	for _, folder := range p.folders {
		out = append(out, folder.Name)
	}
	return strings.Join(out, ", ")
}

// standingPlacementFor decides the folders a proposal's work will be placed
// in: the one the call named, else the ones this conversation is placed in
// directly, else none. It answers a refusal in the model's grammar when the
// call cannot be honoured.
//
// ONLY WORK THAT RUNS IS PLACED. A folder's rules reach a run and its report;
// a line to say and a rule have neither, and a rule already names its folders
// with folder_scope.
func (a *Agent) standingPlacementFor(ctx context.Context, parsed standArguments, item standing.Item) (standingPlacement, string) {
	named := strings.TrimSpace(parsed.Placement)
	if item.Does.Kind != standing.ActionTask {
		if named != "" {
			return standingPlacement{}, "Invalid arguments: placement is for work that runs (does.kind task) — a line to say keeps no rules, and a rule names its folders with folder_scope"
		}
		return standingPlacement{}, ""
	}
	if named != "" {
		// THE SAME LAW AS collections place: a governing binding is changed in a
		// conversation with the person, never by a delegated answer.
		if a.steward() != nil {
			return standingPlacement{}, "placing work in a folder needs the person's answer in a conversation"
		}
		if err := (workspace.Ref{Kind: workspace.CollectionKind, ID: named}).Validate(); err != nil {
			return standingPlacement{}, "Invalid arguments: placement " + err.Error()
		}
	}
	store, err := a.config.Organization.open(false)
	switch {
	case errors.Is(err, os.ErrNotExist) || errors.Is(err, workspace.ErrNotFound):
		if named != "" {
			return standingPlacement{}, "folder not found: " + named
		}
		return standingPlacement{}, ""
	case err != nil:
		return standingPlacement{}, "folders could not be read: " + err.Error()
	}
	defer store.Close()
	if named != "" {
		folders, err := store.Collections(ctx)
		if err != nil {
			return standingPlacement{}, "folders could not be read: " + err.Error()
		}
		for _, folder := range folders {
			if folder.ID == named {
				return standingPlacement{folders: []workspace.Collection{folder}}, ""
			}
		}
		return standingPlacement{}, "folder not found: " + named
	}
	source := a.organizationSource()
	if source.Kind == "" {
		return standingPlacement{}, ""
	}
	places, err := store.GoverningCollections(ctx, source)
	if err != nil {
		return standingPlacement{}, "folders could not be read: " + err.Error()
	}
	place := standingPlacement{inherited: true}
	for _, folder := range places {
		if folder.Depth == 0 && len(place.folders) < standingPlacementCap {
			place.folders = append(place.folders, folder.Collection)
		}
	}
	return place, ""
}

// standingTerms is the card's lines about work that runs: what one run does,
// the report and who writes it, the folder, and the rules that reach it now.
// A line to say and a rule carry none (the card's own bands say all of them).
func (a *Agent) standingTerms(ctx context.Context, item standing.Item, place standingPlacement) []string {
	if item.Does.Kind != standing.ActionTask {
		return nil
	}
	terms := []string{standingDoesTag + clip(oneLine(item.Does.Brief), standingCardClip)}
	if report := item.Does.Report; report != "" {
		terms = append(terms, standingReportTag+report+" — "+standingReportWho)
	}
	folder := standingFolderNone
	if len(place.folders) > 0 {
		folder = place.names()
		if place.inherited {
			folder += standingFolderInherited
		}
		folder += standingFolderReach
	}
	terms = append(terms, standingFolderTag+folder)
	rules, err := a.standingRulesIfPlaced(ctx, item, place.ids())
	switch {
	case errors.Is(err, errNoGoverningReader):
		// NOTHING HERE CAN READ RULES, so the card says nothing about them
		// rather than claiming none reach the work.
	case err != nil:
		terms = append(terms, standingRulesTag+"could not be read: "+oneLine(err.Error()))
	case len(rules) == 0:
		terms = append(terms, standingRulesTag+standingRulesNone)
	default:
		for at, rule := range rules {
			if at == standingCardRules {
				terms = append(terms, fmt.Sprintf("%sand %d more", standingRulesTag, len(rules)-at))
				break
			}
			terms = append(terms, standingRuleTag+clip(oneLine(rule.Prompt()), standingCardClip))
		}
	}
	return terms
}

// errNoGoverningReader is a session with nothing behind it that can say which
// rules apply — a test's fake store, a door with no standing owner.
var errNoGoverningReader = errors.New("session: nothing here reads rules")

// standingRulesIfPlaced answers the rules that would reach this item's runs if
// it were placed in folders, from the reading the run itself will make.
func (a *Agent) standingRulesIfPlaced(ctx context.Context, item standing.Item, folders []string) ([]standing.Item, error) {
	reader := a.governingReader()
	if reader == nil {
		return nil, errNoGoverningReader
	}
	collections := map[string]int{}
	if len(folders) > 0 {
		store, err := a.config.Organization.open(false)
		if err != nil {
			return nil, err
		}
		places, err := store.GoverningIfPlaced(ctx, folders)
		store.Close()
		if err != nil {
			return nil, err
		}
		nearestDepths(collections, places)
	}
	items, err := reader.ApplicableScope(item.Workspace, item.Origin.SessionID, collections)
	if err != nil {
		return nil, err
	}
	return GoverningRules(items), nil
}

// governingReader is the standing owner this conversation reads rules
// through: the read door a delegated run was handed, else the conversation's
// own store. Nil is a session with neither.
func (a *Agent) governingReader() GoverningReader {
	if g := a.config.Governing; g != nil && g.Reader != nil {
		return g.Reader
	}
	if s := a.config.Standing; s != nil && s.Store != nil {
		return s.Store
	}
	return nil
}

// standingPlaceWork binds the work to the card's folders after the yes and
// BEFORE the item is written: it mints the item's id, places that id, and only
// then may the item be created under it. Nothing is placed for a card nobody
// said yes to, and no moment exists when the work stands outside the rules the
// person was shown.
//
// A PLACEMENT THAT DID NOT TAKE IS A YES THAT DID NOT TAKE. The person agreed
// to work governed by that folder's rules, so the item is never created and the
// conversation is told nothing was set up.
func (a *Agent) standingPlaceWork(ctx context.Context, item *standing.Item, place standingPlacement) error {
	if len(place.folders) == 0 {
		return nil
	}
	if a.config.Organization == nil {
		return errors.New("could not be placed in " + place.names() + ": folder organization is unavailable here")
	}
	item.ID = standing.NewItemID()
	if err := a.placeInFolders(ctx, item.ID, place); err != nil {
		a.standingUnplace(ctx, item.ID, place)
		return errors.New("could not be placed in " + place.names() + ": " + oneLine(err.Error()))
	}
	return nil
}

// standingUnplace takes back the bindings of work that was never created. It
// is best effort: a binding it cannot remove names an item that does not exist,
// and so governs nothing.
func (a *Agent) standingUnplace(ctx context.Context, id string, place standingPlacement) {
	if len(place.folders) == 0 || a.config.Organization == nil {
		return
	}
	store, err := a.config.Organization.open(false)
	if err != nil {
		return
	}
	defer store.Close()
	for _, folder := range place.ids() {
		_ = store.RemovePlacement(ctx, folder, workspace.Ref{Kind: workspace.StandingKind, ID: id})
	}
}

// placeInFolders is the governing placement itself, the relation
// `aforge standing add --place` and `collections place` write.
func (a *Agent) placeInFolders(ctx context.Context, id string, place standingPlacement) error {
	store, err := a.config.Organization.open(false)
	if err != nil {
		return err
	}
	defer store.Close()
	for _, folder := range place.ids() {
		if err := store.AddPlacement(ctx, folder, workspace.Ref{Kind: workspace.StandingKind, ID: id}); err != nil {
			return err
		}
	}
	return nil
}
