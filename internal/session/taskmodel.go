package session

// WHICH MODEL A TASK RUNS ON, AND WHO DECIDES.
//
// A node is the same worker doing the same job somewhere quieter (task_run.go),
// so it inherits the conversation's model and that is the right default for
// almost every piece of work. What this file adds is the OTHER case: the person
// says "let opus do this one" or "use something cheap for the sweep", and the
// model that grooms the work is the one holding both halves of that sentence —
// what the work is, and what they asked for. So `model` is an argument on
// propose_task, resolved HERE against the models this install actually has.
//
// ── THREE ANSWERS, AND ONLY ONE OF THEM IS AN ERROR ──
//
//   - ONE MATCH IS THE ANSWER. "opus-5", "anthropic/claude-opus-5" and a
//     vendorless tail that only one row carries are all the same decision, and
//     none of them is worth asking a person about.
//   - MORE THAN ONE IS A QUESTION, not a guess. The shortlist rides on the
//     proposal the person is already being shown (task.go), because a task that
//     is about to start on a model nobody named is the one moment the choice is
//     cheap to correct.
//   - NOTHING IS A REFUSAL THE MODEL CAN ACT ON: an ordinary tool result naming
//     the nearest ids, so a wrong guess costs one round trip rather than a task
//     started on a model that does not exist. A word that matches half the
//     catalog is the same refusal for the same reason — a shortlist of eleven is
//     a list, not a choice.
//
// ── WHY THE MATCHING LIVES HERE AND THE CATALOG DOES NOT ──
//
// This package holds no catalog and must not: which models a person has is the
// surface's fact, resolved from settings and a provider listing, and it changes
// while the session runs. [Config.TaskModels] is the seam, exactly as
// SupportsImages and NearestModels are, and NIL IS "NOBODY CAN SAY" rather than
// "there are none — a caller that hands over no list gets the word taken as
// written and the provider's own error if it is wrong, which is what every
// caller had before this file existed.

import (
	"slices"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/roles"
)

const (
	// taskModelShortlist bounds one confirmation. Four options is a choice a
	// person makes at a glance; the fifth turns the proposal into a picker, and
	// a word that vague is better said again more precisely.
	taskModelShortlist = 4
	// taskModelNear bounds the ids a refusal names. It is the same figure for
	// the same reason: the refusal is a nudge back onto a real id, not a listing.
	taskModelNear = 4
)

// taskModelChoice is what one `model` argument came to. EXACTLY ONE FIELD IS
// EVER SET: the model it resolved to, the shortlist a person has to settle, or
// the refusal the model reads instead.
type taskModelChoice struct {
	model   string
	options []string
	problem string
}

// resolveTaskModel reads one `model` argument against the models this session
// can actually reach.
//
// An EMPTY word is the ordinary case and never an error: the task runs on the
// configured task model, and on the conversation's own model when nothing is
// configured. That default is resolved through the same matcher, so a settings
// row written as "opus-5" reaches the same id a proposal naming it would.
func (a *Agent) resolveTaskModel(word string) taskModelChoice {
	word = strings.TrimSpace(word)
	available := a.taskModelList()
	if word == "" {
		return taskModelChoice{model: a.defaultTaskModel(available)}
	}
	if len(available) == 0 {
		// Nobody holds a list, so nothing here can be validated against one.
		// Refusing would be this package inventing a catalog; the word travels as
		// written and the provider answers for it.
		return taskModelChoice{model: word}
	}
	candidates := matchTaskModel(word, available)
	switch {
	case len(candidates) == 1:
		return taskModelChoice{model: candidates[0]}
	case len(candidates) == 0:
		return taskModelChoice{problem: taskModelUnknown(word, nearTaskModels(word, available), a.defaultTaskModel(available))}
	case len(candidates) > taskModelShortlist:
		return taskModelChoice{problem: taskModelVague(word, candidates[:taskModelShortlist])}
	default:
		return taskModelChoice{options: candidates}
	}
}

// defaultTaskModel is what a proposal that names no model runs on: the
// configured task model, else the crew's worker seat, else the model this
// conversation is on right now.
//
// THE CREW'S WORKER SEAT SITS BETWEEN THE PIN AND THE CONVERSATION. The worker
// is the seat that pays most of a task's bill, and until it was on the ladder
// the crew moved everything about a task except its cost: a person on `frugal`
// talking to a frontier model handed every task to that frontier model. The
// `task.model` row still wins, because it is the more specific answer — one
// person pinning one thing — and the conversation is still the floor, because
// a cleared worker row means "follow the conversation" on every tier
// (internal/config's TierModelAt).
//
// A LEVEL ON THE WORKER ROW IS NOT CARRIED ONTO THE TASK. `vendor/model:high`
// on a tier row is the notation for the one-shot role calls that ride it, and
// a task's own thinking depth is its own dial ([Agent.SetTaskEffort], the
// `effort` row) — so the id travels and the level stays, rather than one row
// quietly setting two things.
//
// It reads the LIVE model rather than the one the session was built with,
// because /model moves it and a task groomed after the switch belongs on the
// model the person is working with now.
func (a *Agent) defaultTaskModel(available []string) string {
	a.mu.Lock()
	configured := strings.TrimSpace(a.config.TaskModel)
	source, model := a.config.RolesSource, a.model
	a.mu.Unlock()
	if configured == "" {
		if seat, ok := roles.TierModel(roles.Source(source), roles.TierWorker); ok {
			id, _ := roles.SplitEffort(seat)
			configured = strings.TrimSpace(id)
		}
	}
	if configured == "" {
		return model
	}
	// A row that resolves to exactly one id is written down in that id's own
	// spelling; anything else is the person's word, kept as they wrote it. A
	// settings row is not a place to refuse from.
	if candidates := matchTaskModel(configured, available); len(candidates) == 1 {
		return candidates[0]
	}
	return configured
}

// taskModelList is the models this session may hand work to, or nil when the
// surface holds no catalog.
func (a *Agent) taskModelList() []string {
	if a.config.TaskModels == nil {
		return nil
	}
	return a.config.TaskModels()
}

// settleTaskModel closes a shortlist with the person's answer.
//
// ONLY A MEMBER OF THE SHORTLIST MAY WIN. An answer naming anything else is a
// surface answering a question that was not asked, and the leading candidate —
// the closest match, which is what the proposal showed — takes the work
// instead. That is also what SILENCE settles to, and it is why an ambiguous
// proposal still starts when the clock runs out: the countdown is a window to
// correct groomed work, never a gate the work waits behind (task.go).
func settleTaskModel(options []string, answer string) string {
	if len(options) == 0 {
		return ""
	}
	wanted := normalizeModelWord(answer)
	if wanted != "" {
		for _, option := range options {
			if normalizeModelWord(option) == wanted {
				return option
			}
		}
	}
	return options[0]
}

// firstTaskModel is the model a proposal SAYS it will run on: the leading
// option while a shortlist is open — which is both the closest match and what
// silence settles to — and the resolved id when there is none. A card that
// showed nothing while the options were up would be asking about a task whose
// model it had declined to name.
func firstTaskModel(options []string, model string) string {
	if len(options) > 0 {
		return options[0]
	}
	return model
}

// ── the matcher ─────────────────────────────────────────────────────────────

// matchTaskModel ranks every model one word could mean, closest first.
//
// The three rungs are tried in order and the first that answers wins, because a
// word that is one row's whole id must never be read as a substring of another:
// "openai/gpt-5" is an id, and it is also inside "openai/gpt-5-mini".
//
//  1. THE WHOLE ID, as written.
//  2. THE TAIL, the part after the vendor: "claude-opus-5" is how a person says
//     "anthropic/claude-opus-5", and two vendors carrying the same tail is a
//     genuine question rather than a tie to break.
//  3. EVERY TOKEN, anywhere in the id: "opus 5" matches
//     "anthropic/claude-opus-5". This is the rung that produces shortlists, and
//     it is ordered shortest id first — the plain name before its variants,
//     which is the one a person naming a family usually means.
func matchTaskModel(word string, available []string) []string {
	needle := normalizeModelWord(word)
	if needle == "" {
		return nil
	}
	tokens := modelTokens(needle)
	var exact, tails, partial []string
	for _, id := range available {
		candidate := normalizeModelWord(id)
		if candidate == "" {
			continue
		}
		switch {
		case candidate == needle:
			exact = append(exact, strings.TrimSpace(id))
		case modelTail(candidate) == needle:
			tails = append(tails, strings.TrimSpace(id))
		case containsAll(candidate, tokens):
			partial = append(partial, strings.TrimSpace(id))
		}
	}
	if len(exact) > 0 {
		return exact
	}
	if len(tails) > 0 {
		return tails
	}
	sortModelIDs(partial)
	return partial
}

// nearTaskModels names the ids worth suggesting to a word that matched nothing:
// the rows sharing at least one of its tokens, nearest first, capped.
//
// It answers nothing rather than something for a word with no overlap at all
// ("fast", "cheap"), and that is the honest answer: the refusal then says what
// the task WOULD run on, which is the fact that actually unsticks the model.
func nearTaskModels(word string, available []string) []string {
	tokens := modelTokens(normalizeModelWord(word))
	if len(tokens) == 0 {
		return nil
	}
	var near []string
	for _, id := range available {
		candidate := normalizeModelWord(id)
		if candidate == "" {
			continue
		}
		if containsAny(candidate, tokens) {
			near = append(near, strings.TrimSpace(id))
		}
	}
	sortModelIDs(near)
	if len(near) > taskModelNear {
		near = near[:taskModelNear]
	}
	return near
}

// sortModelIDs puts the shortest id first and breaks ties alphabetically, so
// two runs of the same catalog produce the same shortlist. A shorter id is the
// less qualified one — "anthropic/claude-opus-5" before
// "anthropic/claude-opus-5-thinking" — which is the reading a person naming a
// family means far more often than not.
func sortModelIDs(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool {
		if len(ids[i]) != len(ids[j]) {
			return len(ids[i]) < len(ids[j])
		}
		return ids[i] < ids[j]
	})
}

// normalizeModelWord is one id or one word in the spelling everything here
// compares in: lower case, trimmed, and without OpenRouter's "~" alias marker,
// which is part of no model's name.
func normalizeModelWord(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "~")
}

// modelTail is the part of a slug after the vendor, and the whole of an id that
// carries no vendor.
func modelTail(id string) string {
	if slash := strings.LastIndex(id, "/"); slash >= 0 {
		return id[slash+1:]
	}
	return id
}

// modelTokens cuts a word into the pieces a model id is spelled with. Anything
// that is not a letter or a digit separates, so "claude opus 5", "claude-opus-5"
// and "claude/opus.5" are the same three tokens.
func modelTokens(word string) []string {
	return strings.FieldsFunc(word, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return false
		}
		return true
	})
}

func containsAll(candidate string, tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	for _, token := range tokens {
		if !strings.Contains(candidate, token) {
			return false
		}
	}
	return true
}

func containsAny(candidate string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(candidate, token) {
			return true
		}
	}
	return false
}

// ── the two refusals, in words ──────────────────────────────────────────────

// taskModelUnknown is what a word that matches nothing comes back as. It names
// what the task would run on if the argument were simply left out, because that
// is the answer for every word that was never a model id — "fast", "cheap", the
// name of something this install does not carry.
func taskModelUnknown(word string, near []string, fallback string) string {
	problem := "no model here is called " + quoteModel(word)
	if len(near) > 0 {
		return problem + " — did you mean " + strings.Join(near, ", ") + "? Name one of those, or leave model out to run on " + fallback + "."
	}
	return problem + ". Name a model id the person has, or leave model out to run on " + fallback + "."
}

// taskModelVague is what a word that matches too much comes back as.
func taskModelVague(word string, some []string) string {
	return quoteModel(word) + " matches several models — say which: " + strings.Join(some, ", ") + "."
}

func quoteModel(word string) string { return `"` + strings.TrimSpace(word) + `"` }

// ── the rescue's own sentence ───────────────────────────────────────────────

// taskModelRescueSays is the middle of [taskModelRescueNote], and it is the
// whole of what recognising one costs: a note that carries it was written by the
// tool-use rescue and by nothing else on this belt.
const taskModelRescueSays = " has no tools; using "

// taskModelRescueNote is the one line a node's row carries while the tool-use
// rescue is the reason it is not on the model it was admitted with
// (task_run.go's [Agent.newTaskAgent]). It is spelled ONCE because two readers
// need it: the one that writes it, and [TaskNode.retarget], which has to know
// its own machinery's sentence in order to take it back down.
func taskModelRescueNote(model, fallback string) string {
	return "model " + model + taskModelRescueSays + fallback
}

// isTaskModelRescueNote reports whether a node's `mend` line is the rescue's
// rather than a repair round's. A repair says what gap it is closing, in the
// work's own words; only this one is about a model.
func isTaskModelRescueNote(note string) bool {
	if !strings.HasPrefix(note, "model ") {
		return false
	}
	// BOTH RESCUES, because both are this machinery's own sentence about a model
	// and both are things a person's explicit pick takes back down. A repair
	// round's `mend` says what gap it is closing, in the work's own words, and is
	// neither of these.
	return strings.Contains(note, taskModelRescueSays) || strings.Contains(note, taskModelMovedSays)
}

// ── the move, and its own sentence ──────────────────────────────────────────

// taskModelMovedSays is the middle of [taskModelMovedNote], and it is the whole
// of what recognising one costs — the same bargain [taskModelRescueSays] makes.
const taskModelMovedSays = " could not answer; using "

// taskModelMovedNote is the line a node's row carries after its worker died on
// a provider that could not answer and the work was run again somewhere else
// (task_run.go's [Agent.workTaskNode]). It is the rescue note's twin: same
// shape, same reader, a different reason.
func taskModelMovedNote(model, next string) string {
	return "model " + model + taskModelMovedSays + next
}

// taskModelMovedSentence is the same fact for the REPORT rather than the row —
// the paragraph a person reads on the card when the work lands, or on the
// failure when it does not.
//
// It names BOTH models on purpose. A card whose cost, voice and quality all
// belong to a model the person never chose and is never told about is a card
// they cannot reconcile against anything.
func taskModelMovedSentence(from, to string) string {
	return from + " stopped answering, so this ran again on " + to
}

// resolveProgramModels is [Agent.resolveTaskModel] for a program, which works
// with one model or several: a `model` naming more than one, separated by
// commas, is resolved word by word, and each word must name exactly one model.
// One word is resolved as any task's is, its shortlist and all.
//
// EVERY MODEL NAMED MUST BE ONE A CONNECTED SERVICE CAN SERVE. The list a word
// is matched against is the whole catalog, and a model none of the person's
// services can reach was handed to the program anyway and then answered, call
// after call, on the crew's working seat by the run's model API — the person
// asked for one model and was quietly given another. It is refused here, by
// name, before a card is shown.
func (a *Agent) resolveProgramModels(word string) taskModelChoice {
	var words []string
	for _, part := range strings.Split(word, ",") {
		if part = strings.TrimSpace(part); part != "" {
			words = append(words, part)
		}
	}
	if len(words) < 2 {
		choice := a.resolveTaskModel(word)
		if len(words) == 0 || choice.problem != "" {
			return choice
		}
		if len(choice.options) > 0 {
			choice.options = slices.DeleteFunc(choice.options, func(model string) bool { return !a.programServes(model) })
			switch len(choice.options) {
			case 0:
				return taskModelChoice{problem: programUnservedProblem(words[0])}
			case 1:
				return taskModelChoice{model: choice.options[0]}
			}
			return choice
		}
		if !a.programServes(choice.model) {
			return taskModelChoice{problem: programUnservedProblem(choice.model)}
		}
		return choice
	}
	var models []string
	for _, part := range words {
		choice := a.resolveTaskModel(part)
		switch {
		case choice.problem != "":
			return choice
		case len(choice.options) > 0:
			return taskModelChoice{problem: taskModelVague(part, choice.options)}
		case !a.programServes(choice.model):
			return taskModelChoice{problem: programUnservedProblem(choice.model)}
		}
		if !slices.Contains(models, choice.model) {
			models = append(models, choice.model)
		}
	}
	return taskModelChoice{model: strings.Join(models, ",")}
}

// programServes says a connected service can take a call on model, by the one
// test the run's model API makes ([ServesModel]). A conversation with no
// services at all cannot be asked, and is not refused on that account.
func (a *Agent) programServes(model string) bool {
	a.mu.Lock()
	sources := a.config.Sources.OrDefault(a.config.APIKey, a.config.BaseURL)
	a.mu.Unlock()
	return sources.Empty() || ServesModel(sources, model)
}

// programUnservedProblem is the refusal for a model no connected service serves.
func programUnservedProblem(model string) string {
	return "none of the model services connected here can serve " + model +
		", so a program cannot be handed it; name a model one of them serves, spelled with its service's prefix when it is not the default service's"
}

// programAsked is the models a proposal asked its program to work with: what
// its `model` resolved to when the proposal named one, and nothing when it
// named none, so the run is handed the crew rather than the default a card
// shows for a task.
func programAsked(spec taskSpec) []string {
	if strings.TrimSpace(spec.modelWord) == "" || strings.TrimSpace(spec.model) == "" {
		return nil
	}
	var asked []string
	for _, model := range strings.Split(spec.model, ",") {
		if model = strings.TrimSpace(model); model != "" {
			asked = append(asked, model)
		}
	}
	return asked
}
