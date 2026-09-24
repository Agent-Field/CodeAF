// Package judge scores a finished run seat by seat, independently of the crew
// that produced it.
//
// THE MECHANISM. A run's record names which model held each judged seat —
// worker, high, mastermind — and what the run delivered. Every seat the record
// carries is put to one model that was not in the crew: the caller supplies
// [Ask], which sends a system and a user prompt and returns the answer. One
// question is asked per seat, in role order, and each answer is read as one
// JSON object carrying a score from 0 to 100 and a one-sentence reason. That is
// the scale a pool records its role_quality metric in, so a run's seats land
// on one scale across every install.
//
// THE CALLER OWNS THE CALL. This package touches no disk and no network and
// holds no credentials: the model, the transport and the bill are the caller's,
// and the caller bills the judge's calls to its own seat. [Pick] chooses that
// model from a catalog, and the chosen id rides in the record ([Record.Judge])
// so a seat already holding it is skipped rather than scoring its own work.
package judge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// recordClip is how many runes of the brief and of the deliverable the user
// prompt carries. They are the two fields that grow with the size of the work
// rather than with its shape, and a judge scoring a seat does not need the whole
// of either to say whether the seat did its job. The report, the claim and the
// ending travel whole: they are the run's own account of itself, and cutting
// them would score a seat on a sentence it did not finish.
const recordClip = 6000

// DefaultFloor is the published coding index a judge must reach to be picked.
// A judge below the worker's own class rubber-stamps the work rather than
// scoring it: it has less of the capability the seat is scored on than the seat
// it is reading.
const DefaultFloor = 60

// Role is one of a crew's judged seats.
type Role string

const (
	// RoleWorker is the seat that carried the work.
	RoleWorker Role = "worker"
	// RoleHigh is the seat that checked the work.
	RoleHigh Role = "high"
	// RoleMastermind is the seat that planned the work.
	RoleMastermind Role = "mastermind"
)

// seatOrder is the order seats are asked in: the work, then what checked it,
// then the plan it was cut under.
var seatOrder = []Role{RoleWorker, RoleHigh, RoleMastermind}

// Record is one finished run as the judge reads it: the spec it ran under, its
// own account of itself, what it touched, and who held each seat. It is the
// judge's own copy, assembled by the caller from wherever the run is recorded,
// so this package imports neither the session store nor anything else that
// owns the run.
type Record struct {
	// Brief is what the run was asked to do.
	Brief string
	// Deliverable is what had to exist when it was over.
	Deliverable string
	// Report is the account the run landed with.
	Report string
	// Claim is the run's own summary of its answer, kept beside the report.
	Claim string
	// Ending is why a run that stopped early stopped where it did.
	Ending string
	// Files is every path the run wrote.
	Files []string
	// Changed is how many paths it changed.
	Changed int
	// Checks is the repeatable verification the run ran.
	Checks []string
	// Seats is which model held each judged seat. The chat door asks no planner
	// call, so a record may carry only the worker and the high seats.
	Seats map[Role]string
	// Judge is the model that will answer the seat questions, the id [Pick]
	// returned. A seat already holding it is skipped, so a model never scores
	// its own work; an empty Judge names no model and skips nothing.
	Judge string
	// CostUSD is what the run spent, in dollars.
	CostUSD float64
}

// Score is one seat's reading: the role, the model that held it, the score on
// the 0-100 scale and the judge's one-sentence reason.
type Score struct {
	Role   Role
	Model  string
	Score  float64
	Reason string
}

// Ask puts one question to the judge model and returns its answer. The caller
// owns the model, the credentials and the transport, and bills the call to the
// judge's own seat; this package writes both prompts and reads the answer.
type Ask func(ctx context.Context, system, user string) (answer string, err error)

// Judge asks one question per seat present in rec.Seats, in role order, and
// answers with a Score for each seat that was read. A seat held by the judge's
// own model ([Record.Judge]) is skipped and named in the returned error; a seat
// whose call fails, or whose answer is not one JSON object carrying an integer
// score from 0 to 100, is an error for that seat alone and never stops the
// others. The scores obtained are returned beside a joined error naming every
// seat that failed, and a record with no seats returns no scores and no error.
func Judge(ctx context.Context, ask Ask, rec Record) ([]Score, error) {
	if len(rec.Seats) == 0 {
		return nil, nil
	}
	if ask == nil {
		return nil, errors.New("judge: no ask function")
	}
	scores := make([]Score, 0, len(seatOrder))
	var failures []error
	for _, role := range seatOrder {
		model, present := rec.Seats[role]
		if !present {
			continue
		}
		if rec.Judge != "" && model == rec.Judge {
			failures = append(failures, fmt.Errorf("judge: the %s seat is held by the judge's own model and was skipped", role))
			continue
		}
		answer, err := ask(ctx, seatSystem(role), seatUser(role, model, rec))
		if err != nil {
			failures = append(failures, fmt.Errorf("judge: the %s seat: %w", role, err))
			continue
		}
		score, reason, err := readAnswer(answer)
		if err != nil {
			failures = append(failures, fmt.Errorf("judge: the %s seat: %w", role, err))
			continue
		}
		scores = append(scores, Score{Role: role, Model: model, Score: score, Reason: reason})
	}
	if len(failures) > 0 {
		return scores, errors.Join(failures...)
	}
	return scores, nil
}

// Candidates returns every catalog row that can judge a crew, cheapest first.
//
// A row qualifies exactly as the judge a crew would otherwise be handed does:
// its published coding index reaches floor, it carries "tools", no crew seat
// holds it, and its vendor differs from the worker's. Rows the provider
// published no price for are out of the running. TWO MORE ROWS ARE NEVER
// CANDIDATES: one whose prompt and completion prices sum to zero, and one
// whose id ends in ":free" whatever the case. A free row is a rate-limited
// row and not a price — the provider answers it with a 429 at the moment a
// judge most needs an answer — so it is asked for nothing, whatever its index
// says.
//
// The list is ordered by cost and then by id, so the cheapest stands first and
// the answer is a property of the catalog rather than of its order. A crew
// with nothing left to draw from yields no candidates.
func Candidates(models []catalog.Model, crew map[Role]string, floor float64) []string {
	workerVendor := ""
	if worker, held := crew[RoleWorker]; held && worker != "" {
		workerVendor = vendor(worker)
	}
	held := make(map[string]bool, len(crew))
	for _, model := range crew {
		if model != "" {
			held[model] = true
		}
	}
	type candidate struct {
		id   string
		cost float64
	}
	var found []candidate
	for _, model := range models {
		if model.PriceUnknown || model.CodingIndex < floor || !hasTools(model.Parameters) || held[model.ID] {
			continue
		}
		if workerVendor != "" && vendor(model.ID) == workerVendor {
			continue
		}
		if model.PromptPrice+model.CompletionPrice == 0 {
			continue
		}
		if strings.HasSuffix(strings.ToLower(model.ID), ":free") {
			continue
		}
		found = append(found, candidate{model.ID, model.PromptPrice + model.CompletionPrice})
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].cost != found[j].cost {
			return found[i].cost < found[j].cost
		}
		return found[i].id < found[j].id
	})
	ids := make([]string, 0, len(found))
	for _, c := range found {
		ids = append(ids, c.id)
	}
	return ids
}

// Pick returns the cheapest judge [Candidates] names, and ok is false when it
// names none. It is Candidates' head-or-nothing, so the properties its callers
// rely on hold here too: the cheaper of two rows wins, and a tie breaks to the
// lower id.
func Pick(models []catalog.Model, crew map[Role]string, floor float64) (judgeID string, ok bool) {
	candidates := Candidates(models, crew, floor)
	if len(candidates) == 0 {
		return "", false
	}
	return candidates[0], true
}

// seatJobs is what each seat is responsible for, and therefore what its work is
// scored against. One paragraph per seat, read as the opening of the system
// prompt.
var seatJobs = map[Role]string{
	RoleWorker:     "You are scoring the WORKER seat of a finished run. The worker seat carried the work: judge whether the delivered work did what the brief asked, coherently, and with the checks the record says were run.",
	RoleHigh:       "You are scoring the HIGH seat of a finished run. The high seat checked the work: judge whether the check read the work itself rather than the worker's claim about it, and whether it caught what a careful reader of the work would catch.",
	RoleMastermind: "You are scoring the MASTERMIND seat of a finished run. The mastermind seat planned the work: judge whether the work was cut into steps the brief supports.",
}

// answerShape is the one object every seat's answer is read as, and nothing
// else. It is stated in both prompts because a model that answers with prose
// around the object is still read, but a model that answers with no object at
// all fails its seat.
const answerShape = `Answer with ONE JSON object and nothing else: {"score": <0-100 integer>, "reason": "<one sentence>"}`

// seatSystem is the system prompt for one seat: the seat's job followed by the
// shape of the one answer asked for.
func seatSystem(role Role) string {
	return seatJobs[role] + " " + answerShape
}

// seatUser is the user prompt for one seat: the record's fields, the brief and
// the deliverable clipped to recordClip runes each, and a line naming which
// model held the seat. The model id is named and nothing is said about its
// maker — the seat is scored on the work, not on the name behind it.
func seatUser(role Role, model string, rec Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Seat: %s\n", role)
	fmt.Fprintf(&b, "Model holding this seat: %s\n\n", model)
	fmt.Fprintf(&b, "Brief:\n%s\n\n", clip(rec.Brief))
	fmt.Fprintf(&b, "Deliverable:\n%s\n\n", clip(rec.Deliverable))
	fmt.Fprintf(&b, "Report:\n%s\n\n", rec.Report)
	fmt.Fprintf(&b, "Claim:\n%s\n\n", rec.Claim)
	fmt.Fprintf(&b, "Ending:\n%s\n\n", rec.Ending)
	fmt.Fprintf(&b, "Checks:\n%s\n\n", strings.Join(rec.Checks, "\n"))
	fmt.Fprintf(&b, "Files:\n%s\n\n", strings.Join(rec.Files, "\n"))
	fmt.Fprintf(&b, "Changed: %d\n", rec.Changed)
	fmt.Fprintf(&b, "Cost (USD): %g\n", rec.CostUSD)
	return b.String()
}

// clip returns s cut to at most recordClip runes, so neither the brief nor the
// deliverable can push the prompt past the budget whatever the run was.
func clip(s string) string {
	if len(s) <= recordClip {
		return s
	}
	runes := []rune(s)
	if len(runes) <= recordClip {
		return s
	}
	return string(runes[:recordClip])
}

// readAnswer reads the first JSON object in the answer into a score and a
// reason. A score that is missing, not a number, not an integer, or outside 0
// to 100 is an error, and so is an answer carrying no JSON object at all. The
// reason may be absent and reads as empty.
func readAnswer(answer string) (float64, string, error) {
	raw, found := firstObject(answer)
	if !found {
		return 0, "", errors.New("the answer carries no JSON object")
	}
	var got struct {
		Score  *json.Number `json:"score"`
		Reason string       `json:"reason"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return 0, "", fmt.Errorf("the answer is not a JSON object: %w", err)
	}
	if got.Score == nil {
		return 0, "", errors.New("the answer carries no score")
	}
	score, err := strconv.ParseFloat(got.Score.String(), 64)
	if err != nil || math.IsNaN(score) || math.IsInf(score, 0) {
		return 0, "", fmt.Errorf("the score %q is not a number", got.Score.String())
	}
	if score != math.Trunc(score) {
		return 0, "", fmt.Errorf("the score %v is not an integer", score)
	}
	if score < 0 || score > 100 {
		return 0, "", fmt.Errorf("the score %v is outside 0-100", score)
	}
	return score, got.Reason, nil
}

// firstObject reads the first JSON value that opens at the answer's first "{",
// so prose before the object costs nothing and prose after it is ignored. An
// answer with no "{" and an object that does not parse both answer with false.
func firstObject(answer string) (json.RawMessage, bool) {
	open := strings.IndexByte(answer, '{')
	if open < 0 {
		return nil, false
	}
	var raw json.RawMessage
	if err := json.NewDecoder(strings.NewReader(answer[open:])).Decode(&raw); err != nil {
		return nil, false
	}
	return raw, true
}

// hasTools reports whether the provider published "tools" among the fields a
// model accepts.
func hasTools(parameters []string) bool {
	for _, parameter := range parameters {
		if strings.EqualFold(strings.TrimSpace(parameter), "tools") {
			return true
		}
	}
	return false
}

// vendor is the part of a model id before the first slash — "z-ai" in
// "z-ai/glm-5.3" — and the whole id when there is no slash. A seat arrives
// spelled however its caller wrote it, so the leading `~` alias marker and a
// thinking level come off first: the same-vendor exclusion reads the model,
// not the spelling it arrived in.
func vendor(id string) string {
	model, _ := roles.SplitEffort(id)
	id = strings.TrimPrefix(model, "~")
	if slash := strings.IndexByte(id, '/'); slash > 0 {
		return id[:slash]
	}
	return id
}
