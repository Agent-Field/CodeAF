package placeorg

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// The Judge a Runner hands its stages. It sits between a stage and the
// engine's Asker and does four things in a fixed order: check the budget, ask,
// hold the answer to the question's contract, and write the journal. Whatever
// goes wrong at any step, the stage gets its fallback and the run goes on, so
// neither method can fail.

const judgeSystem = "You help sort a person's chats into named places. Answer exactly as the question asks and say nothing else."

// meteredJudge is the Runner's Judge.
type meteredJudge struct {
	ask    placegraph.Asker
	budget Budget
	// price turns one exchange into dollars. Nil means the engine does not
	// say what a call cost, so cost stays unknown and only Calls bounds a run.
	price func(prompt, answer string) float64
	note  func(Entry) error
	run   string
	now   func() time.Time

	mu    sync.Mutex
	spent Spent
	// over is set the first time a question was refused for budget, which is
	// what makes a Plan provisional.
	over bool
	// failed is the first journal error. The Judge cannot return it, so the
	// Runner reads it between stages and ends the run.
	failed error
}

var _ Judge = (*meteredJudge)(nil)

// Choose asks the model to point at one of q.Options and returns that option,
// or the fallback.
func (j *meteredJudge) Choose(ctx context.Context, q Question, fallback Option) Pick {
	pick := Pick{Key: fallback.Key, FellBack: true}
	answer, cost, model, why := j.exchange(ctx, q, choosePrompt(q))
	pick.CostUSD, pick.Model = cost, model
	if why == "" {
		if key, ok := matchOption(answer, q.Options); ok {
			pick.Key, pick.FellBack = key, false
		} else {
			why = "the answer named no option it was shown"
		}
	}
	pick.Why = why
	return j.finish(ctx, q, pick)
}

// Write asks the model for a short text and returns it only when the
// validator accepts it; otherwise the fallback.
func (j *meteredJudge) Write(ctx context.Context, q Question, valid Validator, fallback string) Pick {
	pick := Pick{Written: fallback, FellBack: true}
	answer, cost, model, why := j.exchange(ctx, q, writePrompt(q))
	pick.CostUSD, pick.Model = cost, model
	if why == "" {
		if clean, ok := valid(answer); ok {
			pick.Written, pick.FellBack = clean, false
		} else {
			why = "the answer did not fit the contract"
		}
	}
	pick.Why = why
	return j.finish(ctx, q, pick)
}

// exchange spends one call, if the budget has one left, and returns the raw
// answer. A non-empty why says no usable answer came back.
func (j *meteredJudge) exchange(ctx context.Context, q Question, prompt string) (answer string, cost float64, model, why string) {
	if why = j.reserve(); why != "" {
		return "", 0, "", why
	}
	if j.ask == nil {
		return "", 0, "", "no model is available"
	}
	if err := ctx.Err(); err != nil {
		return "", 0, "", "the run was cancelled"
	}
	raw, err := j.ask(ctx, placegraph.ModelRequest{Role: q.Role, System: judgeSystem, User: prompt, AnsweredBy: &model})
	if err != nil {
		return "", 0, model, "the model did not answer"
	}
	if j.price != nil {
		cost = j.price(prompt, raw)
	}
	return strings.TrimSpace(raw), cost, model, ""
}

// reserve counts the call before it is made, so two stages asking at once
// cannot both take the last one.
func (j *meteredJudge) reserve() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.budget.Calls <= j.spent.Calls || (j.budget.USD > 0 && j.spent.USD >= j.budget.USD) {
		j.over = true
		return "over budget"
	}
	j.spent.Calls++
	return ""
}

// finish books the cost, journals the judgment and, for a fallback, the
// fallback too.
func (j *meteredJudge) finish(ctx context.Context, q Question, pick Pick) Pick {
	j.mu.Lock()
	j.spent.USD += pick.CostUSD
	if pick.FellBack {
		j.spent.Fallbacks++
	}
	spent := j.spent
	j.mu.Unlock()
	q.Options = append([]Option(nil), q.Options...)
	entry := Entry{At: j.now(), Run: j.run, Kind: EntryJudgment, Question: &q, Pick: &pick, Spent: spent}
	j.write(entry)
	if pick.FellBack {
		entry.Kind, entry.Note = EntryFallback, q.Purpose+": "+pick.Why
		j.write(entry)
	}
	return pick
}

func (j *meteredJudge) write(e Entry) {
	if err := j.note(e); err != nil {
		j.mu.Lock()
		if j.failed == nil {
			j.failed = err
		}
		j.mu.Unlock()
	}
}

// snapshot is what the run has used so far.
func (j *meteredJudge) snapshot() (Spent, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.spent, j.over
}

func (j *meteredJudge) failure() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.failed
}

func choosePrompt(q Question) string {
	var b strings.Builder
	writeQuestion(&b, q)
	b.WriteString("\nOptions:\n")
	for _, o := range q.Options {
		fmt.Fprintf(&b, "%s: %s\n", o.Key, o.Text)
	}
	b.WriteString("\nAnswer with one JSON object, {\"pick\":\"<option key>\"}, and nothing else.")
	return b.String()
}

func writePrompt(q Question) string {
	var b strings.Builder
	writeQuestion(&b, q)
	b.WriteString("\nAnswer with the text only, on one line.")
	return b.String()
}

func writeQuestion(b *strings.Builder, q Question) {
	b.WriteString(q.Purpose)
	if q.Context != "" {
		b.WriteString("\n\n")
		b.WriteString(q.Context)
	}
}

// matchOption reads an answer as one of the options it was shown: inside a
// JSON object, as a bare key, or as an option's own text. Anything else is
// outside the contract.
func matchOption(answer string, options []Option) (string, bool) {
	keys := make([]string, len(options))
	texts := make([]string, len(options))
	for i, o := range options {
		keys[i], texts[i] = o.Key, o.Text
	}
	var obj struct {
		Pick string `json:"pick"`
	}
	if placegraph.DecodeObject(answer, &obj) == nil {
		answer = obj.Pick
	}
	answer = strings.Trim(strings.TrimSpace(answer), "\"'`")
	if i, ok := placegraph.PickIndex(answer, "", keys); ok {
		return options[i].Key, true
	}
	if i, ok := placegraph.PickIndex(answer, "", texts); ok {
		return options[i].Key, true
	}
	return "", false
}
