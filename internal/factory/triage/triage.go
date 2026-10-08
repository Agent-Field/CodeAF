// Package triage is the factory's cheap read on arrival: one model call per
// new item, made once, that writes the item's read, type, size, estimate,
// risk, possible duplicate and a guess at what to take first.
//
// AI WHERE IT EARNS ITS PLACE, AND NOWHERE ELSE. The read is drawn as dim facts
// a person reads, never as a decision: nothing here launches, hides, labels,
// comments, raises a card or changes a gate, cap or stage. A label GitHub
// gave the item and anything a person set win over the model, because
// [Apply] fills only what is empty.
//
// ONE CALL PER ITEM, EVER. [Worker] stamps [factory.Triage.TriagedAt] even
// when the answer could not be read, so an item the model cannot describe is
// left alone rather than paid for on every tick.
//
// The package holds no model client. The caller hands [Worker] a function
// that sends one prompt and answers the text (cmd/codeaf's
// factory_triage.go builds it on the crew's cheapest seat and puts its cost
// on the spend ledger), so a test drives the whole loop with a string.
package triage

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// BodyMost is how much of an item's text the prompt carries. The poll already
// keeps the first 2000 characters of an issue; a terminal or chat item may be
// longer, and a read does not need the whole of it.
const BodyMost = 3000

// ReasonWords is the most words a priority's reason may have. It is drawn at
// the right of a row, where five words is all there is room for.
const ReasonWords = 5

// RiskMost is how many risk phrases a read keeps, and RiskWords how long one
// may be. A risk is a short phrase a person scans, not a paragraph.
const (
	RiskMost  = 4
	RiskWords = 4
)

// Types, Sizes are the read's closed vocabularies. A word outside them is
// dropped rather than drawn, so the floor only ever says a word it knows.
var (
	Types = []string{"bug", "feat", "chore", "question"}
	Sizes = []string{"S", "M", "L"}
)

// CommentMost is how much of one comment the prompt carries, and Comments how
// many: the last three, so the read sees where the discussion stands.
const (
	CommentMost = 600
	Comments    = 3
)

// Prompt is the fixed question one item is asked. It is the same words for
// every item, with the item's own facts in the middle: the title, the body
// cut to [BodyMost], the labels, the kind, the repository, and the last
// [Comments] comments each cut to [CommentMost].
func Prompt(it factory.Item) string {
	body := strings.TrimSpace(it.Body)
	if r := []rune(body); len(r) > BodyMost {
		body = string(r[:BodyMost]) + "…"
	}
	labels := strings.Join(it.Labels, ", ")
	var b strings.Builder
	b.WriteString("You triage one incoming work item for a software team. Read it and answer with ONE JSON object and nothing else.\n\n")
	b.WriteString("repository: " + it.Repo + "\n")
	b.WriteString("kind: " + string(it.Kind) + "\n")
	if it.Num > 0 {
		b.WriteString("number: #" + strconv.Itoa(it.Num) + "\n")
	}
	b.WriteString("title: " + it.Title + "\n")
	if labels != "" {
		b.WriteString("labels: " + labels + "\n")
	}
	b.WriteString("body:\n" + body + "\n\n")
	if said := it.Comments; len(said) > 0 {
		if len(said) > Comments {
			said = said[len(said)-Comments:]
		}
		b.WriteString("latest comments, oldest first:\n")
		for _, c := range said {
			text := strings.Join(strings.Fields(c.Body), " ")
			if r := []rune(text); len(r) > CommentMost {
				text = string(r[:CommentMost]) + "…"
			}
			who := c.Author
			if who == "" {
				who = "someone"
			}
			b.WriteString("- " + who + ": " + text + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(`Answer exactly this shape:
{"read": "...", "type": "...", "size": "...", "est_usd": 0, "risk": [], "dup": "", "priority": 0, "reason": "..."}

- read: one plain sentence, under 20 words, on what the work really is and what is hard about it.
- type: one of bug, feat, chore, question.
- size: one of S, M, L (S an hour, M a day, L more).
- est_usd: what an AI coding agent would spend on model calls to do it, in US dollars, a number; most items cost between 0.5 and 10.
- risk: a list of short phrases for what the work touches, such as "touches money", "touches auth", "has ui", "migration"; [] when nothing stands out.
- dup: an issue or pull request this one repeats (the same request made twice), like "#12", only if the text names it; a pull request that fixes an issue is not a duplicate of it; otherwise "".
- priority: 1 (take it first) to 5 (later).
- reason: why that priority, at most five words.
`)
	return b.String()
}

// answer is the JSON the prompt asks for. est_usd and priority are read
// loosely, because a cheap model writes `"3"` as readily as `3`.
type answer struct {
	Read     string          `json:"read"`
	Type     string          `json:"type"`
	Size     string          `json:"size"`
	Est      json.RawMessage `json:"est_usd"`
	Risk     json.RawMessage `json:"risk"`
	Dup      json.RawMessage `json:"dup"`
	Priority json.RawMessage `json:"priority"`
	Reason   string          `json:"reason"`
}

// ErrNoRead is every way an answer can fail to be a read: no JSON object in
// it, or an object that says nothing this package can draw.
var ErrNoRead = errors.New("triage: no read in the answer")

// Parse reads the model's answer into a [factory.Triage]. It is tolerant of
// what cheap models wrap their JSON in: a code fence, a sentence before it,
// prose after it. It is strict about the words: a type or size outside the
// vocabulary is dropped, the priority is held to 1..5, the reason is cut to
// [ReasonWords] words and the risk list to [RiskMost] short phrases.
// TriagedAt is the caller's to stamp.
func Parse(reply string) (factory.Triage, error) {
	obj, ok := firstObject(reply)
	if !ok {
		return factory.Triage{}, ErrNoRead
	}
	var a answer
	if err := json.Unmarshal([]byte(obj), &a); err != nil {
		return factory.Triage{}, ErrNoRead
	}
	t := factory.Triage{
		Read:     oneLine(a.Read),
		Type:     inVocab(strings.ToLower(oneLine(a.Type)), Types),
		Size:     inVocab(strings.ToUpper(oneLine(a.Size)), Sizes),
		Est:      number(a.Est),
		Risk:     risks(a.Risk),
		Dup:      dup(a.Dup),
		Priority: int(number(a.Priority)),
		Reason:   words(oneLine(a.Reason), ReasonWords),
	}
	if t.Est < 0 {
		t.Est = 0
	}
	if t.Priority < 1 || t.Priority > 5 {
		t.Priority = 0
	}
	if t.Read == "" && t.Type == "" && t.Size == "" && t.Est == 0 && len(t.Risk) == 0 && t.Priority == 0 {
		return factory.Triage{}, ErrNoRead
	}
	return t, nil
}

// Apply lays a read over an item, filling ONLY WHAT IS EMPTY. The type and
// size the poll took from a label or a diff, and anything a person set, were
// there first and stay; the model's word is the fallback, never the
// override.
func Apply(it *factory.Item, t factory.Triage) {
	got := &it.Triage
	if strings.TrimSpace(got.Read) == "" {
		got.Read = t.Read
	}
	if got.Type == "" {
		got.Type = t.Type
	}
	if got.Size == "" {
		got.Size = t.Size
	}
	if got.Est == 0 {
		got.Est = t.Est
	}
	if len(got.Risk) == 0 {
		got.Risk = t.Risk
	}
	if got.Dup == "" {
		got.Dup = t.Dup
	}
	if got.Priority == 0 {
		got.Priority = t.Priority
	}
	if got.Reason == "" {
		got.Reason = t.Reason
	}
}

// Wants says whether an item is still waiting for its read: on the floor,
// with no read written and never triaged.
func Wants(it factory.Item) bool {
	return it.State != factory.StateDismissed &&
		strings.TrimSpace(it.Triage.Read) == "" &&
		it.Triage.TriagedAt.IsZero()
}

// firstObject finds the first balanced JSON object in s, skipping fences and
// prose on either side. Braces inside strings are honoured.
func firstObject(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	for start >= 0 {
		depth, inStr, esc := 0, false, false
		for i := start; i < len(s); i++ {
			c := s[i]
			switch {
			case esc:
				esc = false
			case inStr && c == '\\':
				esc = true
			case c == '"':
				inStr = !inStr
			case inStr:
			case c == '{':
				depth++
			case c == '}':
				depth--
				if depth == 0 {
					obj := s[start : i+1]
					if json.Valid([]byte(obj)) {
						return obj, true
					}
					i = len(s)
				}
			}
		}
		next := strings.IndexByte(s[start+1:], '{')
		if next < 0 {
			break
		}
		start += 1 + next
	}
	return "", false
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func inVocab(word string, vocab []string) string {
	for _, v := range vocab {
		if word == v {
			return v
		}
	}
	return ""
}

func words(s string, most int) string {
	f := strings.Fields(s)
	if len(f) > most {
		f = f[:most]
	}
	return strings.TrimRight(strings.Join(f, " "), ".,;:")
}

// number reads a JSON number or a string holding one (`"$3.50"` included);
// anything else is zero, which reads as nothing said.
func number(raw json.RawMessage) float64 {
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return f
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "$"))
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	}
	return 0
}

// risks reads a list of phrases, or one phrase, or a comma list in one
// string; each is lower-cased, cut to [RiskWords] words, and repeats and
// non-answers (`none`, `n/a`) are dropped.
func risks(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) != nil {
		var one string
		if json.Unmarshal(raw, &one) != nil {
			return nil
		}
		list = strings.Split(one, ",")
	}
	var out []string
	seen := map[string]bool{}
	for _, r := range list {
		r = words(strings.ToLower(oneLine(r)), RiskWords)
		switch r {
		case "", "none", "n/a", "na", "no", "nothing", "low":
			continue
		}
		if seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
		if len(out) == RiskMost {
			break
		}
	}
	return out
}

// dup reads the possible duplicate: a string such as `#12` or `acme/api#12`,
// or a bare number, which is written `#12`. Anything that is not a reference
// is dropped, so a sentence never stands where a ref is drawn.
func dup(raw json.RawMessage) string {
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		if n >= 1 {
			return "#" + strconv.Itoa(int(n))
		}
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t\n") {
		return ""
	}
	i := strings.LastIndexByte(s, '#')
	if i < 0 {
		if _, err := strconv.Atoi(s); err == nil && s != "0" {
			return "#" + s
		}
		return ""
	}
	if _, err := strconv.Atoi(s[i+1:]); err != nil {
		return ""
	}
	return s
}

// Store is the slice of the factory store the worker reads and writes.
// *store.Store satisfies it; [Store.Annotate] keeps the item's Changed, so a
// read the factory made does not make a row look as though it just moved.
type Store interface {
	List() ([]factory.Item, error)
	Annotate(id int, change func(*factory.Item) error) error
}

// Call sends one prompt to a model and answers its text.
type Call func(ctx context.Context, prompt string) (string, error)

// IdleTicks is how many ticks the worker sleeps after a tick that found
// nothing waiting: a floor with nothing new is looked at every IdleTicks ×
// every, not every tick, because a list is a read of every item's file.
const IdleTicks = 15

// CallTries is how many times a call that FAILED (as opposed to answered
// something unreadable) is tried on one item before the item is stamped and
// left. A network that is down for a minute must not stamp every new item
// unread for good; a request the provider refuses every time must not hold
// the queue forever.
const CallTries = 3

// Worker triages the floor until ctx ends: each tick it lists the items, takes
// the newest one still waiting ([Wants]), asks call ONCE, and writes the read
// through st.Annotate. One item a tick, so a floor that just connected forty
// issues is read at one every tick rather than in a burst.
//
// IT NEVER STOPS THE FLOOR. A failed list, a refused call or an answer that is
// not a read is logged (log may be nil) and the loop goes on. An answer that
// is not a read stamps TriagedAt at once, and a call that fails stamps it on
// its [CallTries]th failure, so no item is asked about forever. It raises no
// card and touches no state, gate, cap or stage.
//
// The write re-reads the item under its lock and does nothing to an item
// that another window read meanwhile, or that a person dismissed.
func Worker(ctx context.Context, st Store, call Call, every time.Duration, log func(string)) {
	if st == nil || call == nil {
		return
	}
	if every <= 0 {
		every = 2 * time.Second
	}
	w := &worker{st: st, call: call, log: log, fails: map[int]int{}}
	wait := every
	for {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		// A tick that read an item comes round again after every; an idle
		// floor, and a call the provider just refused, wait IdleTicks times as
		// long, because asking again in two seconds would only be refused
		// again.
		wait = every
		if w.once(ctx) != tickRead {
			wait = every * IdleTicks
		}
	}
}

type tick int

const (
	tickIdle tick = iota
	tickRead
	tickFailed
)

type worker struct {
	st    Store
	call  Call
	log   func(string)
	fails map[int]int
}

func (w *worker) say(s string) {
	if w.log != nil {
		w.log(s)
	}
}

// once is one tick: it triages at most one item.
func (w *worker) once(ctx context.Context) tick {
	items, err := w.st.List()
	if err != nil {
		w.say("triage: list: " + err.Error())
		return tickIdle
	}
	var it factory.Item
	found := false
	for _, x := range items {
		if Wants(x) {
			it, found = x, true
			break
		}
	}
	if !found {
		return tickIdle
	}
	// THE READ IS MARKED IN FLIGHT on the store, when the store keeps such
	// marks, so a window drawing the floor shows `reading` on this row for
	// as long as the call takes.
	if b, ok := w.st.(busyMarker); ok {
		_ = b.SetBusy(it.ID, factory.BusyReading)
		defer func() { _ = b.SetBusy(it.ID, "") }()
	}
	reply, callErr := w.call(ctx, Prompt(it))
	if ctx.Err() != nil {
		// A call cut short by the process closing is not this item's failure,
		// and stamping it would leave it unread for good.
		return tickRead
	}
	if callErr != nil {
		w.fails[it.ID]++
		w.say("triage: " + it.Ref() + ": " + callErr.Error())
		if w.fails[it.ID] < CallTries {
			return tickFailed
		}
	}
	delete(w.fails, it.ID)
	read, parseErr := factory.Triage{}, callErr
	if callErr == nil {
		read, parseErr = Parse(reply)
		if parseErr != nil {
			w.say("triage: " + it.Ref() + ": " + parseErr.Error())
		}
	}
	now := time.Now()
	err = w.st.Annotate(it.ID, func(cur *factory.Item) error {
		if !Wants(*cur) {
			return errAlreadyRead
		}
		if parseErr == nil {
			Apply(cur, read)
		}
		cur.Triage.TriagedAt = now
		return nil
	})
	if err != nil && !errors.Is(err, errAlreadyRead) {
		w.say("triage: " + it.Ref() + ": write: " + err.Error())
	}
	return tickRead
}

var errAlreadyRead = errors.New("triage: already read")

// busyMarker is a store that keeps what is in flight (factory.BusyKeeper's
// writing half); *store.Store is one.
type busyMarker interface {
	SetBusy(id int, word string) error
}
