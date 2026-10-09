package placegraph

// The model half of recommendations: what is asked, and how the answer is read.
//
// THE MODEL NEVER NAMES A REAL ID, PATH OR PERMISSION. It is shown short labels
// ("p1", "c3") standing for places and chats rules already chose, and it answers
// with those labels. An answer naming a label it was not shown is thrown away
// whole, not repaired: a model that invents one label has stopped reading the
// question, and the rest of its answer is no better. The one free-text field it
// writes is a NEW PLACE'S NAME, which is held to the graph's own name rules and
// a few of its own, and which a person sees and approves before it exists.
//
// THIS FILE MAKES NO CALL. The call goes through the engine's role door
// (internal/session's callRoleChecked) via the Asker the engine supplies, so
// the model, the budget journal and the provider are the engine's own.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/roles"
)

// ModelRequest is one question for the engine's role door.
type ModelRequest struct {
	Role   roles.Role
	System string
	User   string
}

// Asker asks the engine's role door one question and returns the raw answer.
// It is supplied by the engine; nil means rules only, no model.
type Asker func(ctx context.Context, req ModelRequest) (string, error)

// ErrBadAnswer is a model answer that did not follow the contract. The graph
// is left exactly as it was.
var ErrBadAnswer = errors.New("placegraph: the model's answer did not fit")

const (
	recommendSystem = "You sort chats into places. A place is a named group of chats about the same work. Answer with one JSON object and nothing else."
	// MaxSuggestedNameRunes is the longest name the model may give a new place.
	// The graph allows longer names a person types; an offered name is a label
	// for a tile, and a sentence there is a sign the model did not understand.
	MaxSuggestedNameRunes = 40
)

func label(prefix string, i int) string { return prefix + strconv.Itoa(i+1) }

func placeLine(s *Snapshot, p Place) string {
	crumb := []string{}
	for _, a := range s.Breadcrumb(p.ID) {
		crumb = append(crumb, a.Name)
	}
	crumb = append(crumb, p.Name)
	line := strings.Join(crumb, " › ")
	if ins := strings.TrimSpace(p.Context.Instructions); ins != "" {
		line += " — " + oneLine(clipRunes(ins, 200))
	}
	return line
}

func chatLine(c ChatEvidence) string {
	parts := []string{oneLine(clipRunes(c.Title, 200))}
	if s := strings.TrimSpace(c.Summary); s != "" {
		parts = append(parts, oneLine(clipRunes(s, 300)))
	} else if m := strings.TrimSpace(c.FirstMessage); m != "" {
		parts = append(parts, oneLine(clipRunes(m, 300)))
	}
	return strings.Join(parts, " — ")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// fileQuestion asks which of the offered places a chat belongs in.
func fileQuestion(s *Snapshot, chat ChatEvidence, cands []candidate) ModelRequest {
	var b strings.Builder
	b.WriteString("Chat:\n")
	b.WriteString(chatLine(chat))
	b.WriteString("\n\nPlaces:\n")
	for i, c := range cands {
		fmt.Fprintf(&b, "%s: %s\n", label("p", i), placeLine(s, c.place))
	}
	b.WriteString("\nWhich one place does this chat clearly belong in? If none fits well, say none. " +
		"Answer with the place's label (p1, p2, ...), not its name. " +
		`Answer {"place": "<label or none>", "confidence": <0-100>}.`)
	return ModelRequest{Role: roles.RolePlaceFile, System: recommendSystem, User: b.String()}
}

type fileAnswer struct {
	Place      string `json:"place"`
	Confidence *int   `json:"confidence"`
}

// readFileAnswer returns the index of the chosen candidate, or -1 for none.
// names are the candidates' names in the order they were labelled.
func readFileAnswer(raw string, names []string) (int, int, error) {
	var a fileAnswer
	if err := decodeObject(raw, &a); err != nil {
		return -1, 0, err
	}
	pick := strings.ToLower(strings.TrimSpace(a.Place))
	if pick == "" || pick == "none" {
		return -1, 0, nil
	}
	i, ok := pickIndex(pick, "p", names)
	if !ok || a.Confidence == nil {
		return -1, 0, ErrBadAnswer
	}
	return i, clampPercent(*a.Confidence), nil
}

// pickIndex reads a label the model was shown, or — because a real model,
// shown "p1: Release pipeline", answers "Release pipeline" as often as "p1"
// (measured on deepseek/deepseek-v4.1-flash, 2026-10-09) — the exact name of
// exactly one of the places it was shown. A name it was not shown, or one two
// shown places share, is still refused: THE MODEL CAN ONLY EVER POINT AT A
// PLACE IT WAS SHOWN, never name one into being.
func pickIndex(answer, prefix string, names []string) (int, bool) {
	if i, ok := labelIndex(answer, prefix, len(names)); ok {
		return i, true
	}
	want := strings.ToLower(oneLine(answer))
	found := -1
	for i, name := range names {
		if strings.ToLower(oneLine(name)) == want && want != "" {
			if found >= 0 {
				return 0, false
			}
			found = i
		}
	}
	return found, found >= 0
}

// candidateNames are the candidates' place names in label order.
func candidateNames(cands []candidate) []string {
	names := make([]string, len(cands))
	for i, c := range cands {
		names[i] = c.place.Name
	}
	return names
}

// suggestQuestion asks whether a group of chats belongs together, and if so
// whether one of the offered places already holds them or what a new place
// should be called and where it should sit.
func suggestQuestion(s *Snapshot, chats []ChatEvidence, existing []candidate, parents []Place) ModelRequest {
	var b strings.Builder
	b.WriteString("Chats in no place:\n")
	for i, c := range chats {
		fmt.Fprintf(&b, "%s: %s\n", label("c", i), chatLine(c))
	}
	if len(existing) > 0 {
		b.WriteString("\nExisting places that might already fit:\n")
		for i, c := range existing {
			fmt.Fprintf(&b, "%s: %s\n", label("p", i), placeLine(s, c.place))
		}
	}
	if len(parents) > 0 {
		b.WriteString("\nPlaces a new place could go under:\n")
		for i, p := range parents {
			fmt.Fprintf(&b, "%s: %s\n", label("u", i), placeLine(s, p))
		}
	}
	b.WriteString("\nDo most of these chats clearly belong together? List only the ones that do. " +
		"Prefer an existing place when one fits. Otherwise name a new place in one to four plain words. " +
		"Use labels (c1, p1, u1), not names, wherever a label is asked for. " +
		`Answer {"belong": true|false, "chats": ["c1", ...], "use": "<existing p label or empty>", ` +
		`"name": "<new place name or empty>", "under": "<u label or root>", "confidence": <0-100>}.`)
	return ModelRequest{Role: roles.RolePlaceSuggest, System: recommendSystem, User: b.String()}
}

type suggestAnswer struct {
	Belong     *bool    `json:"belong"`
	Chats      []string `json:"chats"`
	Use        string   `json:"use"`
	Name       string   `json:"name"`
	Under      string   `json:"under"`
	Confidence *int     `json:"confidence"`
}

// suggestion is a suggest answer resolved back to real things.
type suggestion struct {
	chats      []int
	use        int // index into existing, or -1
	name       string
	under      int // index into parents, or -1 for the top level
	confidence int
}

func readSuggestAnswer(raw string, nChats int, existing, parents []string) (*suggestion, error) {
	var a suggestAnswer
	if err := decodeObject(raw, &a); err != nil {
		return nil, err
	}
	if a.Belong == nil || a.Confidence == nil {
		return nil, ErrBadAnswer
	}
	if !*a.Belong {
		return nil, nil
	}
	out := &suggestion{use: -1, under: -1, confidence: clampPercent(*a.Confidence)}
	seen := map[int]bool{}
	for _, c := range a.Chats {
		i, ok := labelIndex(strings.ToLower(strings.TrimSpace(c)), "c", nChats)
		if !ok {
			return nil, ErrBadAnswer
		}
		if !seen[i] {
			seen[i] = true
			out.chats = append(out.chats, i)
		}
	}
	if use := strings.ToLower(strings.TrimSpace(a.Use)); use != "" && use != "none" {
		i, ok := pickIndex(use, "p", existing)
		if !ok {
			return nil, ErrBadAnswer
		}
		out.use = i
		return out, nil
	}
	name, err := cleanSuggestedName(a.Name)
	if err != nil {
		return nil, err
	}
	out.name = name
	switch under := strings.ToLower(strings.TrimSpace(a.Under)); under {
	case "", "root", "none":
	default:
		i, ok := pickIndex(under, "u", parents)
		if !ok {
			return nil, ErrBadAnswer
		}
		out.under = i
	}
	return out, nil
}

// cleanSuggestedName holds a model-written name to the graph's name rules and
// to the shape of a label: no path separators, no URL, no leading dot, no
// quotes, at most MaxSuggestedNameRunes. Nothing about it is ever used as a
// path; the rules are there so it cannot even look like one on a tile.
func cleanSuggestedName(raw string) (string, error) {
	name := strings.Trim(oneLine(raw), "\"'`“”‘’ .")
	n, err := cleanName(name)
	if err != nil {
		return "", ErrBadAnswer
	}
	if len([]rune(n)) > MaxSuggestedNameRunes || strings.ContainsAny(n, `/\:*?<>|`) || strings.Contains(strings.ToLower(n), "http") || strings.HasPrefix(n, ".") {
		return "", ErrBadAnswer
	}
	letters := 0
	for _, r := range n {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	if letters < 2 {
		return "", ErrBadAnswer
	}
	return n, nil
}

// decodeObject reads exactly one JSON object from a model answer, allowing the
// fenced block small models like to wrap it in, and nothing else around it.
func decodeObject(raw string, into any) error {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") || len(s) > 8192 {
		return ErrBadAnswer
	}
	dec := json.NewDecoder(strings.NewReader(s))
	if err := dec.Decode(into); err != nil {
		return ErrBadAnswer
	}
	if dec.More() {
		return ErrBadAnswer
	}
	return nil
}

func labelIndex(s, prefix string, n int) (int, bool) {
	if !strings.HasPrefix(s, prefix) {
		return 0, false
	}
	i, err := strconv.Atoi(strings.TrimPrefix(s, prefix))
	if err != nil || i < 1 || i > n {
		return 0, false
	}
	return i - 1, true
}

func clampPercent(n int) int { return max(0, min(100, n)) }
