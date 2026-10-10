package placegraph

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Promoting into what a place knows reuses the store memory decider as the
// writer. The decider already settles one candidate against what is stored
// near it — add, refine, replace, or skip — and that answer is what lands
// here. A memory titled for a place becomes a line the person said in chat.
// Separately, one kind of question answered the same way three times in a
// place, with no different answer of that kind in between, becomes a learned
// line. This file does not call a model and does not write the session memory
// store; the caller hands in a decider (the real one, or a fake in tests).

// LearnedAnswerCount is how many consistent answers of one kind, in one place,
// with no different answer of that kind in between, produce a learned line.
// Decisions 12a and P-17 both say three.
const LearnedAnswerCount = 3

// LearnedFromAnswers is the source caption under a learned line. Zero and
// negative counts render as nothing: an unknown count is not "learned from 0".
func LearnedFromAnswers(n int) string {
	if n < 1 {
		return ""
	}
	return fmt.Sprintf("Learned from %d of your answers", n)
}

// MemoryCandidate is the memory being settled for one place. Title is required
// on the settled result: a memory with no title is not "titled for a place"
// and is not written. ChatID and At are the said-in-chat provenance when the
// caller has them; unknown values stay absent.
type MemoryCandidate struct {
	Title  string
	Text   string
	ChatID string
	At     time.Time
}

// MemoryNeighbor is one live knows line as the decider sees it. A knows line
// is one sentence, so Title and Text are that sentence: the decider reads both.
type MemoryNeighbor struct {
	ID    string
	Title string
	Text  string
}

// MemoryVerdict is the decider's answer. Op is add, refine, replace, or skip.
// The store decider's own words update and supersede are refine and replace.
type MemoryVerdict struct {
	Op       string
	TargetID string
	Title    string
	Text     string
}

// MemoryDecider settles one candidate against the place's live lines. The
// session wires the store memory decider; tests pass a fake.
type MemoryDecider interface {
	Decide(candidate MemoryCandidate, neighbors []MemoryNeighbor) (MemoryVerdict, error)
}

// KindAnswer is one answer a person gave to one kind of question. Kind is the
// question's kind (AskKind plus subject, the shape P-10 names). Way is the
// answer itself, in the person's words.
type KindAnswer struct {
	Kind   string
	Way    string
	ChatID string
	At     time.Time
}

// Promotion is what a promote call wrote. Lines are the lines added or
// updated, in that order. Ask is set when a replacement was not struck
// because both lines are the person's own words from the same day. Skipped
// means nothing was written. Receipt is the graph's Undo receipt; a skipped
// call has none.
type Promotion struct {
	Lines   []Line
	Ask     *AskOnce
	Skipped bool
	Receipt Receipt
}

// PromoteMemory asks decider and writes the line it settles, in one graph
// mutation. The decider runs before the file lock is held: a model call must
// not block another window's save. A missing place is an error and does not
// ask the decider. An empty place, a nil decider, or a candidate with no
// title and no text writes nothing.
func (s *Store) PromoteMemory(placeID string, candidate MemoryCandidate, decider MemoryDecider) (Promotion, error) {
	placeID = strings.TrimSpace(placeID)
	if placeID == "" || decider == nil {
		return Promotion{Skipped: true}, nil
	}
	if strings.TrimSpace(candidate.Title) == "" && strings.TrimSpace(candidate.Text) == "" {
		return Promotion{Skipped: true}, nil
	}
	sn, err := s.Snapshot()
	if err != nil {
		return Promotion{}, err
	}
	place, ok := sn.Place(placeID)
	if !ok {
		return Promotion{}, fmt.Errorf("%w: %s", ErrNotFound, placeID)
	}
	if place.Archived {
		return Promotion{}, ErrArchived
	}
	verdict, err := decider.Decide(candidate, memoryNeighbors(sn.Knowledge(placeID), candidate))
	if err != nil {
		return Promotion{}, err
	}
	var promo Promotion
	rc, err := s.mutate(func(st *State, now time.Time) (*change, error) {
		var ch *change
		var applyErr error
		promo, ch, applyErr = applyMemoryVerdict(st, placeID, candidate, verdict, now, s.opts.NewID)
		return ch, applyErr
	})
	if err != nil {
		return Promotion{}, err
	}
	promo.Receipt = rc
	if rc.Noop() {
		promo.Skipped = true
	}
	return promo, nil
}

// LearnAnswers writes a learned line for each kind that was answered the same
// way LearnedAnswerCount times with no different answer of that kind in
// between. answers is the history for this place, oldest first; passing the
// same history again does not add a second line. A later streak of a different
// answer strikes the learned line of the earlier way. A line the person wrote
// is not struck by the count.
func (s *Store) LearnAnswers(placeID string, answers []KindAnswer) (Promotion, error) {
	placeID = strings.TrimSpace(placeID)
	if placeID == "" {
		return Promotion{Skipped: true}, nil
	}
	var promo Promotion
	rc, err := s.mutate(func(st *State, now time.Time) (*change, error) {
		var ch *change
		var applyErr error
		promo, ch, applyErr = applyLearned(st, placeID, answers, now, s.opts.NewID)
		return ch, applyErr
	})
	if err != nil {
		return Promotion{}, err
	}
	promo.Receipt = rc
	if rc.Noop() {
		promo.Skipped = true
	}
	return promo, nil
}

func applyMemoryVerdict(st *State, placeID string, candidate MemoryCandidate, verdict MemoryVerdict, now time.Time, newID func(string) string) (Promotion, *change, error) {
	p, err := st.needPlace(placeID)
	if err != nil {
		return Promotion{}, nil, err
	}
	if p.Archived {
		return Promotion{}, nil, ErrArchived
	}
	op, ok := canonicalMemoryOp(verdict.Op)
	if !ok {
		return Promotion{}, nil, fmt.Errorf("%w: memory op %q", ErrInvalid, verdict.Op)
	}
	if op == "skip" {
		return Promotion{Skipped: true}, nil, nil
	}
	_, body, titled := settledMemory(verdict, candidate)
	if !titled {
		return Promotion{Skipped: true}, nil, nil
	}
	switch op {
	case "add":
		line := saidLine(placeID, body, candidate, now, newID)
		st.Lines = append(st.Lines, line)
		return Promotion{Lines: []Line{line}}, &change{ActionLineAdd, line.ID}, nil
	case "refine":
		return refineLine(st, placeID, verdict.TargetID, body)
	case "replace":
		return replaceLine(st, placeID, verdict.TargetID, body, candidate, now, newID)
	default:
		return Promotion{Skipped: true}, nil, nil
	}
}

func refineLine(st *State, placeID, targetID, body string) (Promotion, *change, error) {
	cur := liveLine(st, placeID, targetID)
	if cur == nil {
		return Promotion{Skipped: true}, nil, nil
	}
	if cur.Text == body {
		return Promotion{Lines: []Line{*cur}}, nil, nil
	}
	next := *cur
	next.Text = body
	*cur = next
	return Promotion{Lines: []Line{next}}, &change{ActionLineUpdate, next.ID}, nil
}

func replaceLine(st *State, placeID, targetID, body string, candidate MemoryCandidate, now time.Time, newID func(string) string) (Promotion, *change, error) {
	cur := liveLine(st, placeID, targetID)
	if cur == nil {
		// A replacement of a line this place does not hold is a skip, the same
		// answer the memory settle gives a target it cannot see.
		return Promotion{Skipped: true}, nil, nil
	}
	// The id is copied before append. Append can move the line slice, and the
	// pointer from liveLine would then address the old array.
	olderID := cur.ID
	fresh := saidLine(placeID, body, candidate, now, newID)
	struck, ask := Supersede(*cur, fresh, now)
	st.Lines = append(st.Lines, fresh)
	promo := Promotion{Lines: []Line{fresh}}
	if ask != nil {
		promo.Ask = ask
		return promo, &change{ActionLineAdd, fresh.ID}, nil
	}
	again := st.findLine(olderID)
	*again = struck
	promo.Lines = append(promo.Lines, struck)
	return promo, &change{ActionLineAdd, fresh.ID}, nil
}

func saidLine(placeID, body string, candidate MemoryCandidate, now time.Time, newID func(string) string) Line {
	src := LineSource{Kind: LineSaidInChat, At: candidate.At}
	if validChatID(candidate.ChatID) == nil {
		src.ChatID = candidate.ChatID
	}
	return Line{
		ID:        newID("ln_"),
		PlaceID:   placeID,
		Text:      body,
		Source:    src,
		CreatedAt: now,
	}
}

func liveLine(st *State, placeID, id string) *Line {
	cur := st.findLine(id)
	if cur == nil || cur.PlaceID != placeID || cur.ReplacedBy != "" {
		return nil
	}
	return cur
}

// canonicalMemoryOp accepts the decider's own spellings and the names this
// task uses for them. update refines a line in place; supersede replaces it.
func canonicalMemoryOp(op string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "add":
		return "add", true
	case "refine", "update":
		return "refine", true
	case "replace", "supersede":
		return "replace", true
	case "skip", "":
		return "skip", true
	default:
		return "", false
	}
}

// settledMemory is the line the decider settled. The verdict wins where it
// spoke; the candidate fills a blank. Titled is false when neither supplied a
// title, even if a body exists — an untitled memory is not promoted.
func settledMemory(verdict MemoryVerdict, candidate MemoryCandidate) (title, body string, titled bool) {
	title = firstText(verdict.Title, candidate.Title)
	body = firstText(verdict.Text, candidate.Text, title)
	return title, body, title != "" && body != ""
}

func firstText(parts ...string) string {
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			return p
		}
	}
	return ""
}

// memoryNeighbors lists live lines, closest to the candidate first. The memory
// decider only reads a few neighbours, so the closest have to be first or a
// later line it should have refined is invisible to it. A struck line is not a
// neighbour: refining history would resurrect it.
func memoryNeighbors(lines []Line, candidate MemoryCandidate) []MemoryNeighbor {
	want := wordSet(candidate.Title + " " + candidate.Text)
	type item struct {
		n     MemoryNeighbor
		score int
		i     int
	}
	items := make([]item, 0, len(lines))
	for i, l := range lines {
		if l.ReplacedBy != "" || strings.TrimSpace(l.Text) == "" {
			continue
		}
		score := 0
		for w := range wordSet(l.Text) {
			if want[w] {
				score++
			}
		}
		items = append(items, item{
			n:     MemoryNeighbor{ID: l.ID, Title: l.Text, Text: l.Text},
			score: score,
			i:     i,
		})
	}
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].score != items[b].score {
			return items[a].score > items[b].score
		}
		return items[a].i < items[b].i
	})
	out := make([]MemoryNeighbor, len(items))
	for i, it := range items {
		out[i] = it.n
	}
	return out
}

func wordSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), unicode.IsSpace) {
		w = strings.TrimFunc(w, unicode.IsPunct)
		if w != "" {
			set[w] = true
		}
	}
	return set
}

type learnedStreak struct {
	way    string
	chatID string
	at     time.Time
}

func applyLearned(st *State, placeID string, answers []KindAnswer, now time.Time, newID func(string) string) (Promotion, *change, error) {
	p, err := st.needPlace(placeID)
	if err != nil {
		return Promotion{}, nil, err
	}
	if p.Archived {
		return Promotion{}, nil, ErrArchived
	}
	var promo Promotion
	var addedID, updatedID string
	for _, kind := range kindOrder(answers) {
		var prevID string
		for _, streak := range streaksOf(kind) {
			key := normalizeLineText(streak.way)
			live, learned := lineByText(st, placeID, key)
			standingID := ""
			if live != nil {
				standingID = live.ID
			} else if !learned {
				chatID := streak.chatID
				if validChatID(chatID) != nil {
					chatID = ""
				}
				fresh := Line{
					ID:      newID("ln_"),
					PlaceID: placeID,
					Text:    streak.way,
					Source: LineSource{
						Kind:    LineLearned,
						ChatID:  chatID,
						At:      streak.at,
						Answers: LearnedAnswerCount,
					},
					CreatedAt: now,
				}
				st.Lines = append(st.Lines, fresh)
				promo.Lines = append(promo.Lines, fresh)
				standingID = fresh.ID
				addedID = fresh.ID
			}
			if standingID != "" && prevID != "" && prevID != standingID {
				ask, struck, did := strikeLearned(st, prevID, standingID, now)
				if ask != nil {
					promo.Ask = ask
				}
				if did {
					promo.Lines = append(promo.Lines, struck)
					updatedID = struck.ID
				}
			}
			if standingID == "" {
				continue
			}
			cur := st.findLine(standingID)
			if cur != nil && cur.ReplacedBy == "" && cur.Source.Kind == LineLearned {
				prevID = standingID
			} else {
				prevID = ""
			}
		}
	}
	if addedID != "" {
		return promo, &change{ActionLineAdd, addedID}, nil
	}
	if updatedID != "" {
		return promo, &change{ActionLineUpdate, updatedID}, nil
	}
	promo.Skipped = true
	return promo, nil, nil
}

func kindOrder(answers []KindAnswer) [][]KindAnswer {
	var order []string
	groups := map[string][]KindAnswer{}
	for _, a := range answers {
		kind := strings.TrimSpace(a.Kind)
		way := strings.TrimSpace(a.Way)
		if kind == "" || way == "" || !utf8.ValidString(way) || strings.ContainsRune(way, '\x00') {
			continue
		}
		if normalizeLineText(way) == "" {
			continue
		}
		if _, ok := groups[kind]; !ok {
			order = append(order, kind)
		}
		a.Kind, a.Way = kind, way
		groups[kind] = append(groups[kind], a)
	}
	out := make([][]KindAnswer, 0, len(order))
	for _, kind := range order {
		out = append(out, groups[kind])
	}
	return out
}

func streaksOf(answers []KindAnswer) []learnedStreak {
	var key string
	var count int
	var cur KindAnswer
	var out []learnedStreak
	for _, ans := range answers {
		next := normalizeLineText(ans.Way)
		if next == key {
			count++
		} else {
			key = next
			count = 1
		}
		cur = ans
		if count == LearnedAnswerCount {
			out = append(out, learnedStreak{way: cur.Way, chatID: cur.ChatID, at: cur.At})
		}
	}
	return out
}

func lineByText(st *State, placeID, key string) (live *Line, learned bool) {
	for i := range st.Lines {
		l := &st.Lines[i]
		if l.PlaceID != placeID || normalizeLineText(l.Text) != key {
			continue
		}
		if l.Source.Kind == LineLearned {
			learned = true
		}
		if l.ReplacedBy == "" && live == nil {
			live = l
		}
	}
	return live, learned
}

func strikeLearned(st *State, olderID, newerID string, now time.Time) (*AskOnce, Line, bool) {
	older := st.findLine(olderID)
	newer := st.findLine(newerID)
	if older == nil || newer == nil || older.ReplacedBy != "" || older.Source.Kind != LineLearned {
		return nil, Line{}, false
	}
	if normalizeLineText(older.Text) == normalizeLineText(newer.Text) {
		return nil, Line{}, false
	}
	struck, ask := Supersede(*older, *newer, now)
	if ask != nil {
		return ask, *older, false
	}
	*older = struck
	return nil, struck, true
}
