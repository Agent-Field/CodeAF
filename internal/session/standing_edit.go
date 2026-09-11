package session

// standing_edit.go is `stand`'s edit: changing ongoing work that already
// stands, in place, on the person's yes (ruling R1).
//
// ── ONE VERB, THE TERMINAL'S, AND ONE ROAD ──
//
// `aforge standing edit <id>` has always changed an item in place — a new
// instructions version (specRevision+1) of the SAME item, keeping what it has
// read and what it has published — through [standing.Store.Revise]. The chat
// had no such verb (`change` was an op that only refused), so a person's "also
// list who owns each request" became a stop and a new card, whose first pass
// swallowed a change as its baseline and whose first run was held at the file
// its predecessor had published: the chat protocol's lifecycle, move and policy
// cases, 2026-09-11. `op: edit` is now that verb, spelled the terminal's way,
// and its yes goes through the same store revision the terminal's does.
//
// ── THE CARD SAYS WHAT CHANGES AND KEEPS WHAT DOES NOT ──
//
// An edit card is the item's own card with each line that changes written
// `old → new`, every line that does not change as it is, and a first line
// naming the parts that change — the names [standing.SpecChanges] gives the log
// and the terminal. Nothing is revised until the yes, and the yes is fenced on
// the version the card was drawn from: a card answered after somebody else's
// edit changes nothing and says so (the scale audit's L2).
//
// ── WHAT AN EDIT DOES NOT DO ──
//
// It does not move work to another folder (`collections place` and `unplace`
// do, at both doors), widen or narrow how far a rule reaches, turn one kind of
// work into another, or reword the person's own sentence, which every row
// leads with: the words sent with an edit are the words of the change, and
// they go in the item's log beside it.

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// The first line of an edit card, and what the rest of it keeps.
const (
	standingChangesTag  = "changes · "
	standingChangesKept = " — the same work; what it has read and published stays"
)

// StandingNextRun is what a revision means for the runs, said once for both
// doors: `aforge standing edit` prints it and the chat's edit answers with it.
const StandingNextRun = "the next run uses it; a run already under way keeps what it started with"

// StandingRevised is the receipt of a revision, in the grammar both doors use:
// `revised <id> to version N: instructions, report`.
func StandingRevised(item standing.Item, changed []string) string {
	return fmt.Sprintf("revised %s to version %d: %s", item.ID, item.SpecRevision, strings.Join(changed, ", "))
}

// standingRevisedLine is what a model is told the instant a change stands, for
// [standingRatifiedLine]'s reason: it is the whole of what it may say next.
const standingRevisedLine = "They answered the card. Your WHOLE reply is one short line saying what changed: no preamble, no working out, no naming this tool or its arguments, and do not ask again."

// standEdit changes one item that stands, on the person's yes.
func (a *Agent) standEdit(ctx context.Context, parsed standArguments) (string, bool, error) {
	store := a.standingItems()
	if store == nil {
		return "there is nothing here to read", true, nil
	}
	if strings.TrimSpace(parsed.ID) == "" {
		return "Invalid arguments: edit names the item with id — its id, or the person's own words for it", true, nil
	}
	current, problem := a.standingNamed(standArguments{ID: parsed.ID})
	if problem != "" {
		return problem, true, nil
	}
	parsed, limits, problem := standingNamedLimits(parsed)
	if problem != "" {
		return problem, true, nil
	}
	draft, problem := standingEdited(current, parsed, time.Now())
	if problem != "" {
		return problem, true, nil
	}
	changed := standing.SpecChanges(current, draft)
	if len(changed) == 0 {
		return "nothing to change: send only what is different" + standingDroppedLine(limits), true, nil
	}
	found, problem := a.standingEditChecks(current, draft)
	if problem != "" {
		return problem, true, nil
	}
	place := a.standingPlacedNow(ctx, current)
	shownBefore, shownAfter := standingShownChange(current, draft)
	notice := StandingNotice{
		Item:      draft,
		WhenWords: standingChange(current.When.Words, draft.When.Words),
		CostWords: standingChange(a.standingCostWords(current, standingKnownLimits(current, standingLimits{})),
			a.standingCostWords(draft, standingKnownLimits(draft, limits))),
		Guessed: parsed.Guessed,
		Terms:   standingEditTerms(changed, a.standingWorkTerms(ctx, shownBefore, place, standingReportFile{}), a.standingWorkTerms(ctx, shownAfter, place, found)),
		// A CHANGE HAS NO `just once`: doing it once is not a smaller version of
		// changing work that keeps running.
		Options: standingEditOptions(draft),
	}
	answer, err := a.askStanding(ctx, &notice)
	switch {
	case errors.Is(err, errStandingUnwatched):
		return "nobody is here to say yes — this can only be changed in a conversation", true, nil
	case errors.Is(err, errStandingUnanswered):
		return "the card was left unanswered — nothing was changed", false, nil
	case err != nil:
		return "the card was never answered: the turn ended first", true, nil
	case !answer.Approved:
		if correction := strings.TrimSpace(answer.Change); correction != "" {
			return "the person changed it: " + correction + "\nNothing was changed yet. Send the edit again with that.", false, nil
		}
		return "nothing was changed: the person said no.", false, nil
	}
	revised, _, err := store.Revise(current.ID, current.SpecRevision, func(item *standing.Item) error {
		item.When, item.Does, item.Rails, item.Brief, item.Grant = draft.When, draft.Does, draft.Rails, draft.Brief, draft.Grant
		return nil
	})
	switch {
	case errors.Is(err, standing.ErrConflict):
		return "nothing was changed: it was changed elsewhere after the card was drawn — read it again with op list and send the edit again", true, nil
	case err != nil:
		return "nothing was changed: " + err.Error(), true, nil
	}
	// The item's log, in the terminal's grammar ("revised at the terminal to
	// version N: …"), with the person's own words for the change after it.
	logged := fmt.Sprintf("revised in the chat to version %d: %s", revised.SpecRevision, strings.Join(changed, ", "))
	if words := strings.TrimSpace(parsed.Words); words != "" {
		logged += " — " + strconv.Quote(words)
	}
	_ = store.Log(revised.ID, logged)
	// NO UPDATE ROW IS SENT. The surface draws four shapes of news and none of
	// them is "changed" (tui3's standUpdateRow draws an unknown word as
	// `stopped`), and a row that said the work stopped would be worse than the
	// one line the model now writes.
	return StandingRevised(revised, changed) + standingCardLines(notice) + a.standingAdopt(store, revised, found) +
		standingDroppedLine(limits) + "\n" + StandingNextRun + "\n" + standingRevisedLine, false, nil
}

// standingEdited is current with what the call changes applied, or the refusal
// of a change an edit does not make. Only what the call sent is changed.
func standingEdited(current standing.Item, parsed standArguments, now time.Time) (standing.Item, string) {
	if problem := standingEditRefusal(current, parsed); problem != "" {
		return current, problem
	}
	draft := current
	if parsed.When.Kind != "" {
		when, problem := standingWhen(parsed, now)
		if problem != "" {
			return current, problem
		}
		if (when.Kind == standing.WhenHold) != (current.When.Kind == standing.WhenHold) {
			return current, "Invalid arguments: an edit cannot make a rule wake or stop something waking — stop it and propose the other"
		}
		// THE SAME WAKING SENT AGAIN IS NO CHANGE TO IT. A model resending the
		// whole call would otherwise hand the item fallback words for the words
		// it has, and a changed waking starts its schedule and its watch over.
		if !standingSameWaking(current.When, when) {
			draft.When = when
		}
	}
	if words := strings.TrimSpace(parsed.WhenWords); words != "" && draft.When.Kind != standing.WhenHold {
		draft.When.Words = words
	}
	if problem := standingEditedDoes(&draft, parsed); problem != "" {
		return current, problem
	}
	if parsed.Rails.PerRunUSD != nil || parsed.Rails.MaxPerDay != nil || strings.TrimSpace(parsed.Rails.Expires) != "" {
		rails, problem := standingRails(parsed, draft.When, now)
		if problem != "" {
			return current, problem
		}
		if parsed.Rails.PerRunUSD != nil {
			draft.Rails.PerRunUSD = rails.PerRunUSD
		}
		if parsed.Rails.MaxPerDay != nil {
			draft.Rails.MaxPerDay = rails.MaxPerDay
		}
		if strings.TrimSpace(parsed.Rails.Expires) != "" {
			draft.Rails.Expires = rails.Expires
		}
	}
	if title := strings.TrimSpace(parsed.Title); title != "" {
		draft.Brief.Title = title
	}
	if grant := strings.TrimSpace(parsed.Grant); grant != "" {
		draft.Grant = grant
	}
	return draft, ""
}

// standingEditRefusal is the refusal of a change an edit does not make, each
// naming the road that does make it.
func standingEditRefusal(current standing.Item, parsed standArguments) string {
	kind := standing.ActionKind(strings.ToLower(strings.TrimSpace(parsed.Does.Kind)))
	switch {
	case len(parsed.Does.Retired) > 0:
		return standingRetiredBrief
	case strings.TrimSpace(parsed.Placement) != "":
		return "Invalid arguments: an edit does not move work — collections place and unplace do, with ref {kind: standing, id: " + current.ID + "}, and the work, what it has read and its report stay"
	case parsed.FolderScope != nil || strings.TrimSpace(parsed.Altitude) != "":
		return "Invalid arguments: an edit keeps how far it reaches — stop it and propose it with the new reach"
	case kind != "" && kind != current.Does.Kind:
		return "Invalid arguments: an edit keeps what the work is — " + standingKindOf(current) + " — so stop it and propose the other"
	}
	return ""
}

// standingKindOf is what an item is, as a refusal names it.
func standingKindOf(item standing.Item) string {
	switch {
	case item.When.Kind == standing.WhenHold:
		return "a rule"
	case item.Does.Kind == standing.ActionSay:
		return "a line to say"
	}
	return "work that runs"
}

// standingEditedDoes applies what the call changes about what a firing does.
// A rule's words for its runs are its instructions, as the terminal's
// `--instructions` writes them on a hold.
func standingEditedDoes(draft *standing.Item, parsed standArguments) string {
	if text := strings.TrimSpace(parsed.Does.Instructions); text != "" {
		switch {
		case draft.When.Kind == standing.WhenHold:
			draft.Brief.Prompt = text
		case draft.Does.Kind == standing.ActionTask:
			draft.Does.Brief = text
		default:
			return "Invalid arguments: a line to say changes with does.say"
		}
	}
	if say := strings.TrimSpace(parsed.Does.Say); say != "" {
		if draft.Does.Kind != standing.ActionSay {
			return "Invalid arguments: does.say is the line a say item delivers, and this one runs work"
		}
		draft.Does.Say = say
	}
	if parsed.Does.Report != nil {
		if draft.Does.Kind != standing.ActionTask {
			return "Invalid arguments: only work that runs keeps a report"
		}
		draft.Does.Report = *parsed.Does.Report
	}
	if acceptance := strings.TrimSpace(parsed.Does.Acceptance); acceptance != "" {
		draft.Does.Acceptance = acceptance
	}
	if model := strings.TrimSpace(parsed.Does.Model); model != "" {
		draft.Does.Model = model
	}
	if parsed.Does.MaxSteps < 0 {
		return "Invalid arguments: does.max_steps cannot be negative"
	}
	if parsed.Does.MaxSteps > 0 {
		draft.Does.MaxSteps = parsed.Does.MaxSteps
	}
	return ""
}

// standingEditChecks asks the changed item what a new one is asked before its
// card — the item's own admission law, its watch, and its report — and answers
// what a new report path already holds.
func (a *Agent) standingEditChecks(current, draft standing.Item) (standingReportFile, string) {
	if err := draft.Validate(); err != nil {
		return standingReportFile{}, "Invalid arguments: " + err.Error()
	}
	if !reflect.DeepEqual(current.When, draft.When) {
		if err := draft.CheckWatch(); err != nil {
			return standingReportFile{}, err.Error()
		}
	}
	if draft.Does.Report == current.Does.Report {
		return standingReportFile{}, ""
	}
	if err := CheckStandingReport(draft.Workspace, draft.Does.Report); err != nil {
		return standingReportFile{}, "Invalid arguments: does.report " + err.Error()
	}
	if problem := a.standingReportTaken(draft); problem != "" {
		return standingReportFile{}, problem
	}
	return a.standingForeignReport(draft), ""
}

// standingSameWaking answers whether two wakings wake the same way, whatever
// words each is said in.
func standingSameWaking(a, b standing.When) bool {
	a.Words, b.Words = "", ""
	return reflect.DeepEqual(a, b)
}

// standingChange is one line of an edit card: the new words, or `old → new`
// when they differ from the old ones.
func standingChange(before, after string) string {
	if before == after || before == "" || after == "" {
		return after
	}
	return before + " → " + after
}

// standingWorkTerms is an item's card lines for an edit: the terms of work that
// runs, or for a line to say and a rule, the one line of what they say.
func (a *Agent) standingWorkTerms(ctx context.Context, item standing.Item, place standingPlacement, found standingReportFile) []string {
	if terms := a.standingTerms(ctx, item, place, found); len(terms) > 0 {
		return terms
	}
	if item.When.Kind == standing.WhenHold {
		return []string{standingRuleTag + clip(oneLine(item.Prompt()), standingCardClip)}
	}
	return []string{"says · " + clip(oneLine(item.Does.Say), standingCardClip)}
}

// standingChangeContext is how many unchanged words an edit card keeps before
// the first changed one, so the change is read in its sentence.
const standingChangeContext = 3

// standingShownChange is the two versions of an item as its edit card draws
// them: each text the edit changes, from a few words before where the two
// begin to differ.
//
// A CHANGE PAST THE CLIP IS STILL SHOWN. Instructions longer than a card line
// that differ only after it clip to one identical line, and the card asked for
// a yes on a change nobody could see (the live lifecycle run of 2026-09-11,
// "also list who owns each request").
func standingShownChange(before, after standing.Item) (standing.Item, standing.Item) {
	before.Does.Brief, after.Does.Brief = standingFromTheChange(before.Does.Brief, after.Does.Brief)
	before.Does.Say, after.Does.Say = standingFromTheChange(before.Does.Say, after.Does.Say)
	before.Brief.Prompt, after.Brief.Prompt = standingFromTheChange(before.Brief.Prompt, after.Brief.Prompt)
	return before, after
}

// standingFromTheChange is two texts that differ, each from
// [standingChangeContext] words before the first difference and led by `…`
// when that is not their start. Texts that do not differ, and two that each
// fit a card line, are kept whole.
func standingFromTheChange(before, after string) (string, string) {
	if before == after || (len(before) <= standingCardClip && len(after) <= standingCardClip) {
		return before, after
	}
	same := 0
	for same < len(before) && same < len(after) && before[same] == after[same] {
		same++
	}
	start, words := same, 0
	for ; start > 0; start-- {
		if before[start-1] == ' ' {
			if words == standingChangeContext {
				break
			}
			words++
		}
	}
	if start == 0 {
		return before, after
	}
	return "…" + before[start:], "…" + after[start:]
}

// standingEditTerms is the edit card's terms: the parts that change, then the
// item's lines with each that changes written `old → new`.
func standingEditTerms(changed, before, after []string) []string {
	terms := []string{standingChangesTag + strings.Join(changed, ", ") + standingChangesKept}
	for at, term := range after {
		if at < len(before) && before[at] != term {
			if tag, _, ok := strings.Cut(term, " · "); ok && strings.HasPrefix(before[at], tag+" · ") {
				term = before[at] + " → " + strings.TrimPrefix(term, tag+" · ")
			}
		}
		terms = append(terms, term)
	}
	return terms
}

// standingEditOptions is an edit card's answers: a card's own, without `just
// once`.
func standingEditOptions(item standing.Item) []AnswerOption {
	var options []AnswerOption
	for _, option := range StandingOptions(item) {
		if option.Key != StandingOnceKey {
			options = append(options, option)
		}
	}
	return options
}

// standingKnownLimits is which of an item's limits an edit card names: those
// the call sent and kept, and any the item holds that differ from the quiet
// defaults — an edit card must not hide a limit set when the work was made. A
// limit an edit dropped leaves the item's own standing, so it is named by that.
func standingKnownLimits(item standing.Item, sent standingLimits) standingLimits {
	known := sent
	if known.perRun != limitKept && item.Rails.PerRunUSD != standDefaultPerRunUSD {
		known.perRun = limitKept
	} else if known.perRun == limitDropped {
		known.perRun = limitUnsent
	}
	if known.perDay != limitKept && item.Rails.MaxPerDay != standDefaultMaxPerDay {
		known.perDay = limitKept
	} else if known.perDay == limitDropped {
		known.perDay = limitUnsent
	}
	return known
}

// standingPlacedNow is where an item that stands is placed now — the folders
// it is placed in directly — so an edit card's folder and rule lines are the
// ones its runs will read.
func (a *Agent) standingPlacedNow(ctx context.Context, item standing.Item) standingPlacement {
	if item.Does.Kind != standing.ActionTask || a.config.Organization == nil {
		return standingPlacement{}
	}
	store, err := a.config.Organization.open(false)
	if err != nil {
		return standingPlacement{}
	}
	defer store.Close()
	places, err := store.GoverningCollections(ctx, workspace.Ref{Kind: workspace.StandingKind, ID: item.ID})
	if err != nil {
		return standingPlacement{}
	}
	var place standingPlacement
	for _, folder := range places {
		if folder.Depth == 0 {
			place.folders = append(place.folders, folder.Collection)
		}
	}
	return place
}
