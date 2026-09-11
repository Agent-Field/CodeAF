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
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// The words the card's terms and the tool's answer are spelled with, each
// ONCE. The manual quotes them exactly (internal/manual/chat/standing-orders.md).
const (
	standingWhenTag   = "when · "
	standingCostsTag  = "costs · "
	standingDoesTag   = "does · "
	standingReportTag = "report · "
	standingFolderTag = "folder · "
	standingRuleTag   = "rule · "
	standingRulesTag  = "rules · "
	// standingReportWho is who writes the report, said on the card and in the
	// tool's answer: the run replies with it, and aforge is the only writer.
	standingReportWho = "aforge publishes this file; the run never writes it"
	// standingFolderReach is what a placement means to the person reading it,
	// for one folder and for several.
	standingFolderReach  = " — its rules reach every run"
	standingFoldersReach = " — their rules reach every run"
	// standingFolderInherited says why a folder nobody named is on the card.
	standingFolderInherited = ", where this conversation is placed"
	// standingFolderNone is work in no folder, which is allowed and not a gap.
	standingFolderNone = "none — it can be placed in one later"
	// standingRulesNone is a placement, or none, that no rule reaches yet.
	standingRulesNone = "none reach this work yet"
	// standingReportNone is work that runs and keeps no file, said rather than
	// left out: the person may have asked for one, and a card silent about it
	// reads the same as a card that forgot.
	standingReportNone = "none — no file is kept current"
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

// reach is what the placement means, in the number the folders are.
func (p standingPlacement) reach() string {
	if len(p.folders) > 1 {
		return standingFoldersReach
	}
	return standingFolderReach
}

// logLine is the item's own log line for its placement, in the terminal
// door's grammar ("placed in folder X; its rules reach this work").
func (p standingPlacement) logLine() string {
	if len(p.folders) > 1 {
		return "placed in folders " + strings.Join(p.ids(), ", ") + "; their rules reach this work"
	}
	return "placed in folder " + strings.Join(p.ids(), ", ") + "; its rules reach this work"
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
		if !a.mayBindFolders() {
			return standingPlacement{}, standingPlacementLaw
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
		return standingNamedFolder(folders, named)
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
	// AN INHERITED FOLDER IS STILL A BINDING, and it is written under the same
	// law as a named one: in a conversation with the person, never by a
	// delegated answer. Work set up without it would run outside the rules this
	// conversation is under, so it is refused rather than left unplaced.
	if len(place.folders) > 0 && !a.mayBindFolders() {
		return standingPlacement{}, standingPlacementLaw + " — this conversation is placed in " + place.names()
	}
	return place, ""
}

// standingNamedFolder is the folder a placement names: by id, else by the one
// folder with that name — "move it to my Work folder" is sent as "Work" as
// often as by the id collections find answers.
func standingNamedFolder(folders []workspace.Collection, named string) (standingPlacement, string) {
	var byName []workspace.Collection
	for _, folder := range folders {
		if folder.ID == named {
			return standingPlacement{folders: []workspace.Collection{folder}}, ""
		}
		if strings.EqualFold(folder.Name, named) {
			byName = append(byName, folder)
		}
	}
	switch len(byName) {
	case 0:
		return standingPlacement{}, "folder not found: " + named
	case 1:
		return standingPlacement{folders: byName}, ""
	}
	return standingPlacement{}, "Invalid arguments: more than one folder is called " + strconv.Quote(named) + " — send placement as its id"
}

// standingPlacementLaw is the refusal of a placement nobody may write here, the
// stand tool's spelling of collections' own law ([Agent.mayBindFolders]).
const standingPlacementLaw = "placing work in a folder needs the person's answer in a conversation"

// standingTerms is the card's lines about work that runs: what one run does,
// the report and who writes it, the folder, and the rules that reach it now.
// A line to say and a rule carry none (the card's own bands say all of them).
// found is what the report path holds that aforge did not put there.
func (a *Agent) standingTerms(ctx context.Context, item standing.Item, place standingPlacement, found standingReportFile) []string {
	if item.Does.Kind != standing.ActionTask {
		return nil
	}
	terms := []string{standingDoesTag + clip(oneLine(item.Does.Brief), standingCardClip)}
	terms = append(terms, standingReportTag+found.said(item.Does.Report))
	folder := standingFolderNone
	if len(place.folders) > 0 {
		folder = place.names()
		if place.inherited {
			folder += standingFolderInherited
		}
		folder += place.reach()
	}
	terms = append(terms, standingFolderTag+folder)
	governing, err := a.standingGoverningIfPlaced(ctx, item, place.ids())
	switch {
	case errors.Is(err, errNoGoverningReader):
		// NOTHING HERE CAN READ RULES, so the card says nothing about them
		// rather than claiming none reach the work.
	case err != nil:
		terms = append(terms, standingRulesTag+"could not be read: "+oneLine(err.Error()))
	default:
		terms = append(terms, governing.ruleTerms()...)
	}
	return terms
}

// errNoGoverningReader is a session with nothing behind it that can say which
// rules apply — a test's fake store, a door with no standing owner.
var errNoGoverningReader = errors.New("session: nothing here reads rules")

// standingGoverning is what would govern an item's runs if it were placed:
// every order that applies and the placements that bring them — the two
// things a run's <standing> section is rendered from.
type standingGoverning struct {
	items  []standing.Item
	places []workspace.GoverningCollection
}

// standingGoverningIfPlaced answers what would govern this item's runs if it
// were placed in folders, from the reading the run itself will make.
func (a *Agent) standingGoverningIfPlaced(ctx context.Context, item standing.Item, folders []string) (standingGoverning, error) {
	reader := a.governingReader()
	if reader == nil {
		return standingGoverning{}, errNoGoverningReader
	}
	var places []workspace.GoverningCollection
	if len(folders) > 0 {
		store, err := a.config.Organization.open(false)
		if err != nil {
			return standingGoverning{}, err
		}
		found, err := store.GoverningIfPlaced(ctx, folders)
		store.Close()
		if err != nil {
			return standingGoverning{}, err
		}
		places = nearestPlaces(nil, found)
	}
	items, err := reader.ApplicableScope(item.Workspace, item.Origin.SessionID, placementDepths(places))
	if err != nil {
		return standingGoverning{}, err
	}
	return standingGoverning{items: items, places: places}, nil
}

// ruleTerms is the card's lines about the rules: each rule's words, or the
// gate every run would meet.
//
// THE GATES EVERY RUN WOULD MEET ARE SAID BEFORE THE YES, in the order the run
// meets them ([Agent.standingBlockLocked]): more rules than a run may carry,
// then more rendered words than it may carry. A run over either stops before
// it acts, and a card that quoted five rules and said nothing would be
// agreeing to work that can never run.
func (g standingGoverning) ruleTerms() []string {
	rules := GoverningRules(g.items)
	switch {
	case len(rules) == 0:
		return []string{standingRulesTag + standingRulesNone}
	case len(rules) > governingHoldLimit:
		return []string{fmt.Sprintf("%s%d reach this work, more than the %d a run can carry — every run would stop until they are narrowed", standingRulesTag, len(rules), governingHoldLimit)}
	}
	if size := len(renderStandingWorld(g.items, g.places, "")); size > governingPromptBytes {
		return []string{fmt.Sprintf("%stheir words come to %d KiB, more than the %d KiB a run can carry — every run would stop until they are narrowed", standingRulesTag, (size+1023)/1024, governingPromptBytes/1024)}
	}
	var terms []string
	for at, rule := range rules {
		if at == standingCardRules {
			return append(terms, fmt.Sprintf("%sand %d more", standingRulesTag, len(rules)-at))
		}
		terms = append(terms, standingRuleTag+clip(oneLine(rule.Prompt()), standingCardClip))
	}
	return terms
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
	_ = a.unplaceFromFolders(ctx, id, place)
}

// unplaceFromFolders takes work out of folders, the relation `collections
// unplace` removes.
func (a *Agent) unplaceFromFolders(ctx context.Context, id string, place standingPlacement) error {
	if len(place.folders) == 0 {
		return nil
	}
	store, err := a.config.Organization.open(false)
	if err != nil {
		return err
	}
	defer store.Close()
	for _, folder := range place.ids() {
		if err := store.RemovePlacement(ctx, folder, workspace.Ref{Kind: workspace.StandingKind, ID: id}); err != nil {
			return err
		}
	}
	return nil
}

// placeInFolders is the governing placement itself, the relation
// `aforge standing add --place` and `collections place` write.
func (a *Agent) placeInFolders(ctx context.Context, id string, place standingPlacement) error {
	if len(place.folders) == 0 {
		return nil
	}
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

// ── what the card says about the report path, and what the yes does with it ──

// standingCardLines is the card the person answered, said back to the model
// line for line: `when ·`, every term but what one run does (which the model
// wrote itself), and `costs ·` (ruling R7).
//
// THE MODEL QUOTES WHAT IT IS HANDED. move replied "Work folder has no
// conditions of its own on record" right after a card that named the Work rule,
// and nested quoted $0.75 a run over a card that said $5.00 (the chat protocol,
// 2026-09-11): told only "set up", a model writes its one line from what it
// remembers sending. The card is already bounded ([standingCardRules],
// [standingCardClip]), so saying it back is too.
func standingCardLines(notice StandingNotice) string {
	var lines []string
	if when := strings.TrimSpace(notice.WhenWords); when != "" {
		// A rule over folders says where it applies in that band, which is not
		// a moment; its words stand alone as they always have.
		if notice.Item.Scope == nil {
			when = standingWhenTag + when
		}
		lines = append(lines, when)
	}
	for _, term := range notice.Terms {
		if !strings.HasPrefix(term, standingDoesTag) {
			lines = append(lines, term)
		}
	}
	if costs := strings.TrimSpace(notice.CostWords); costs != "" {
		lines = append(lines, standingCostsTag+costs)
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(lines, "\n")
}

// standingReportFile is a file at a report path that aforge did not put there,
// as a card describes it and the yes adopts it. The zero value is a path that
// holds nothing, or holds exactly what aforge last put there.
type standingReportFile struct {
	target  string
	sum     string
	bytes   int
	written time.Time
}

// said is the card's report line for report, by what the path holds.
//
// A FILE aforge NEVER WROTE IS SAID BEFORE THE YES (ruling R3). h2-spec and
// permission both drew `aforge publishes this file` over a file that was already
// there — one the chat had seeded, one the person's own — and every run was
// then held `report-changed`, a question nobody had been warned of.
func (f standingReportFile) said(report string) string {
	switch {
	case report == "":
		return standingReportNone
	case f.sum != "":
		return fmt.Sprintf("%s — this file already exists (%d bytes, written %s ago); aforge will replace it",
			report, f.bytes, TaskAgeWord(time.Since(f.written)))
	}
	return report + " — " + standingReportWho
}

// standingReceipts opens the store report paths' receipts are kept in: the
// conversation's own standing root, opened as the runner opens it
// ([standingRunner.fenceFor]), so the card, the yes and the run read one
// receipt.
func (a *Agent) standingReceipts() (*standing.Store, error) {
	store := a.standingItems()
	if store == nil || strings.TrimSpace(store.Root()) == "" {
		return nil, errors.New("there is no standing store here")
	}
	return standing.Open(store.Root())
}

// standingForeignReport answers what item's report path holds that aforge did
// not put there: a regular file whose bytes are not the path's receipt. A path
// that holds nothing, or what aforge last put there, answers the zero value.
func (a *Agent) standingForeignReport(item standing.Item) standingReportFile {
	if item.Does.Report == "" {
		return standingReportFile{}
	}
	_, target, err := reportTarget(item.Workspace, item.Does.Report)
	if err != nil {
		return standingReportFile{}
	}
	info, err := os.Lstat(target)
	if err != nil || !info.Mode().IsRegular() {
		return standingReportFile{}
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return standingReportFile{}
	}
	sum := sha256Hex(string(raw))
	if receipts, err := a.standingReceipts(); err == nil {
		if last, err := receipts.Receipt(target); err == nil && last != nil && last.SHA256 == sum {
			return standingReportFile{}
		}
	}
	return standingReportFile{target: target, sum: sum, bytes: len(raw), written: info.ModTime()}
}

// standingAdopt records, after the yes, that the file the card described is
// aforge's to replace — a receipt of class adopted with its bytes' sha256 — so
// the first report publishes over it rather than waiting on the person. It
// answers the line the tool's result carries when the file could not be
// adopted, and "" otherwise.
//
// THE FILE IS LOOKED AT AGAIN AT THE YES (the scale audit's L2). The person
// agreed to replace the bytes the card described; a file saved again between
// the card and the yes is not what they agreed to, so it is not adopted and the
// first report waits for them. A file gone by then needs no adopting: the first
// report creates it.
func (a *Agent) standingAdopt(store standingStore, item standing.Item, found standingReportFile) string {
	if found.sum == "" {
		return ""
	}
	receipts, err := a.standingReceipts()
	if err == nil {
		err = receipts.AtReport(found.target, "", "", func(*standing.Receipt) (*standing.Receipt, error) {
			raw, err := os.ReadFile(found.target)
			switch {
			case errors.Is(err, os.ErrNotExist):
				return nil, nil
			case err != nil:
				return nil, err
			case sha256Hex(string(raw)) != found.sum:
				return nil, errors.New("it changed after the card was drawn")
			}
			return &standing.Receipt{Class: standing.ReceiptAdopted, Item: item.ID, SHA256: found.sum, Bytes: found.bytes, At: time.Now().UTC()}, nil
		})
	}
	if err != nil {
		return "\n" + standingReportTag + item.Does.Report + " was not adopted: " + oneLine(err.Error()) +
			" — the first report will wait for them until the file is moved aside"
	}
	_ = store.Log(item.ID, fmt.Sprintf("report %s was already there (%d bytes); adopted at the yes, so the first report replaces it", item.Does.Report, found.bytes))
	return ""
}

// standingReportAt answers the standing work whose report is the file at path,
// among what stands here and has not been stopped: the one owner a report path
// has. Paths are compared as the filesystem resolves them now.
func (a *Agent) standingReportAt(path string) (standing.Item, bool) {
	items, err := a.standingHere()
	if err != nil {
		return standing.Item{}, false
	}
	want := resolvedFile(path)
	for _, item := range items {
		if item.Status != standing.StatusRetired && item.Does.Report != "" &&
			resolvedFile(filepath.Join(item.Workspace, item.Does.Report)) == want {
			return item, true
		}
	}
	return standing.Item{}, false
}

// standingReportTaken refuses work whose report is already another standing
// item's. Two items publishing one file would each replace the other's report
// on every run, and a person who wanted the first one different wants it edited.
func (a *Agent) standingReportTaken(item standing.Item) string {
	if item.Does.Report == "" {
		return ""
	}
	owner, taken := a.standingReportAt(filepath.Join(item.Workspace, item.Does.Report))
	if !taken || owner.ID == item.ID {
		return ""
	}
	return "Invalid arguments: " + item.Does.Report + " is already the report of " + strconv.Quote(owner.Words) + " (" + owner.ID +
		"). To change that work, send op edit with its id; to replace it, stop it first."
}

// resolvedFile is path with its folder resolved through the filesystem as it is
// now — the deepest part of it that exists — so one file reached two ways
// compares equal.
func resolvedFile(path string) string {
	dir := filepath.Dir(filepath.Clean(path))
	existing := deepestExisting(dir)
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return filepath.Clean(path)
	}
	rest, err := filepath.Rel(existing, dir)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Join(real, rest, filepath.Base(path))
}

// standingNamedReport refuses work that runs whose call left does.report out
// while the person's own sentence names a file — "keep reports/inbox.md
// current" set up as work that keeps nothing. The refusal names the field,
// every file the sentence names, and both honest answers, so the model sends
// one of them: the path of the file each run keeps current, or "" when the
// work only reads them.
//
// A NAMED FILE IS ASKED ABOUT ONLY IF IT COULD BE THE REPORT, by the law the
// report itself is admitted under ([standingCouldReport]): a home path, a path
// out of the project, a file the watch reaches or a file in a folder it
// watches is never the report, whatever the sentence says about it.
func standingNamedReport(parsed standArguments, item standing.Item) string {
	if item.Does.Kind != standing.ActionTask || parsed.Does.Report != nil {
		return ""
	}
	var named []string
	for _, field := range strings.Fields(parsed.Words) {
		path := strings.TrimRight(strings.Trim(field, "\"'`“”‘’,;:!?()[]{}<>"), ".")
		if standingLooksLikeFile(path) && standingCouldReport(item, path) && !slices.Contains(named, path) {
			named = append(named, path)
		}
	}
	switch len(named) {
	case 0:
		return ""
	case 1:
		return "Invalid arguments: their sentence names " + named[0] + " — send does.report " + strconv.Quote(named[0]) +
			" if each run keeps that file current, or does.report \"\" if the work only reads it"
	default:
		return "Invalid arguments: their sentence names " + strings.Join(named, ", ") +
			" — send does.report with the one each run keeps current, or does.report \"\" if the work only reads them"
	}
}

// standingCouldReport answers whether path could be this item's report at all,
// by [standing.Item.Validate] — the report law every door admits an item under:
// relative, inside the project, outside its own watch and the folders it
// watches. A path that opens with ~ is the person's home directory, which is
// outside the project whatever its lexical reading.
func standingCouldReport(item standing.Item, path string) bool {
	if strings.HasPrefix(path, "~") {
		return false
	}
	item.Does.Report = path
	return item.Validate() == nil
}

// standingLooksLikeFile answers whether one word of a sentence is a file path:
// a name with an extension of two or more characters that starts with a letter
// ("inbox-report.md", "notes/a.txt"), and not a web address, a pattern, a
// version or an abbreviation ("v1.2", "e.g", "i.e"). A one-letter extension
// ("x.c") is not read as a file; asking about it would be the rarer mistake.
func standingLooksLikeFile(word string) bool {
	if word == "" || strings.Contains(word, "://") || strings.ContainsAny(word, "*?[") {
		return false
	}
	ext := filepath.Ext(word)
	name := strings.TrimSuffix(filepath.Base(word), ext)
	if len(ext) < 3 || len(ext) > 9 || name == "" || !unicode.IsLetter(rune(ext[1])) {
		return false
	}
	for _, r := range ext[1:] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
