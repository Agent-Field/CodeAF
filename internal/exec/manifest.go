package exec

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// THE MANIFEST: everything about a subharness that is true before it runs.
//
// docs/SUBHARNESS-PRD.md §3 asks for one language-agnostic description shared by
// the subharnesses compiled into this binary and the ones a person writes as a
// JavaScript bundle, and it asks for it as a GROWTH of [SubharnessInfo] rather
// than as a second type beside it. That is what the embedding below is. Every
// reader that has ever asked a registration for its name, its purpose, its ruler
// or its budget shape still asks the same fields of the same struct; a manifest
// is that struct plus the six things a TYPED program needs and a leaf worker
// never did — the schemas, the whitelist, the cues, the guards, and where it
// came from.
//
// ONE NAMESPACE ACROSS GO AND JS, which is the decision the whole contract rests
// on. There is exactly one map from a name to a subharness in this process, so a
// bundle on disk and a worker compiled in are the same kind of thing to the
// person, to the model, and to every list either of them reads. Which language
// ran it is a fact about the runner and reaches no surface.
type Manifest struct {
	// SubharnessInfo is the identity and the COST SHAPE, unchanged and in the
	// same spelling every existing caller reads: the name, the one line of
	// purpose, the capacity ruler the sizing pass quotes, and the three fields
	// [SubharnessInfo.Deadline] turns into a leaf's hang backstop. The PRD names
	// this pair — prior anchors plus deadline shape — as the manifest's cost
	// shape and says to keep the linearInfo form. Embedding is how it is kept:
	// there is no second spelling of a budget anywhere in this file.
	SubharnessInfo

	// Cues are the trigger vocabulary a match is made against — the words that
	// mean this work, written down at design time and PERSISTED with the
	// program. Their absence from disk is a real hole in the old system, named
	// at internal/session/harness_build.go where a designed harness's cues die
	// with the session that designed them and the page it saved carries none.
	// A manifest with no cues is found by being NAMED and by nothing else, which
	// is a quieter subharness rather than a broken one.
	Cues []string `json:"cues,omitempty"`

	// Input is the typed front door: a JSON Schema with defaults and optional
	// fields, which the intake card draws field by field and chat fills from the
	// conversation. A Go runner may derive it by reflection over a struct; a
	// bundle declares it. Both arrive here as the same bytes.
	Input Schema `json:"input,omitempty"`

	// Output is the PROMISE, and it is the machine-checkable finish line the old
	// one-string program form never had. A run that ends without producing this
	// shape is INCOMPLETE — never done with a strange message — and that
	// sentence is enforceable only because the shape is written down here.
	Output Schema `json:"output,omitempty"`

	// Whitelist is the belt tools this subharness may call through [Env.Tool].
	// It is a ceiling and not a grant: a tool named here that the session does
	// not have is simply not there, and every call still goes through the same
	// consent doors an ordinary tool call goes through. An empty whitelist is a
	// subharness that spends only on the model.
	Whitelist []string `json:"whitelist,omitempty"`

	// Guards are the cheap preconditions checked before a run — a file that has
	// to exist, a tool that has to be on the belt, an input field that has to
	// look a certain way. A FAILED GUARD IS NOT A FAILED RUN: the run falls back
	// to the general worker with the same input, and the person is told the step
	// needed a closer look. See [Guard] for the vocabulary law that governs what
	// they are allowed to say.
	Guards []Guard `json:"guards,omitempty"`

	// Provenance is where this subharness was found: shipped in the binary,
	// written by the person, or committed into the repository they are standing
	// in. It is drawn as a dim mark on the list and it is the one field a person
	// reads that this file spells in words rather than in a code.
	//
	// PROVENANCE IS STAMPED, NEVER AUTHORED. A manifest.json on disk does not get
	// to claim it is built in; the registry writes this field from the layer the
	// bundle was actually loaded out of ([Layer.Provenance]), which is why
	// [Manifest.Validate] neither requires it nor objects to it.
	Provenance Provenance `json:"provenance,omitempty"`
}

// Provenance is where a subharness came from, in the three words a person reads.
// They are person-facing strings rather than codes because they are drawn
// literally — there is no second table translating a constant into English, and
// so no way for the two to drift apart.
type Provenance string

const (
	// FromBinary is a subharness compiled into this build: the defaults the
	// owner ships, always present, never missing on a fresh machine.
	FromBinary Provenance = "built-in"
	// FromYou is a bundle out of the person's own home store.
	FromYou Provenance = "yours"
	// FromProject is a bundle committed into the repository they are working in,
	// which is the whole of the org-sharing story: sharing is a pull, review is a
	// pull request, and history is the versions.
	FromProject Provenance = "from this project"
)

// Layer is a place a subharness can be found, in the order the registry looks.
//
// THE ORDER IS THE LOOKUP ORDER AND IT LIVES HERE ONCE. PRD §7 states it and
// nothing else in the tree is allowed to restate it: a source registers itself
// at a layer and the registry sorts by the constant, so adding the packed
// trailer in a later phase is a registration at [LayerPacked] and not an edit to
// any lookup.
type Layer int

const (
	// LayerBuiltIn is the compiled-in Go runners. They are layer zero — before
	// every store — and that is deliberate: a bundle on disk MAY NOT SHADOW A
	// NAME THIS BINARY SHIPS. The names the owner ships are the names the manual
	// and the prompts describe, and a store that could silently replace one
	// would make both of those documents a lie about the program that actually
	// ran.
	LayerBuiltIn Layer = iota
	// LayerPacked is a zip appended to the binary itself. It is Phase 2 and
	// nothing in this tree writes or reads one yet; the constant exists so the
	// phase that does is a registration rather than a renumbering.
	LayerPacked
	// LayerProject is `.aforge/subharnesses/` inside the repository in hand.
	LayerProject
	// LayerHome is `~/.aforge/subharnesses/`, which moves with AFORGE_HOME the
	// same way the page store below does.
	LayerHome
	// LayerPages is `~/.aforge/harnesses/`, where a subharness written as a PAGE
	// lives — the shape the design flow saves when somebody asks for a program to
	// be built for them (internal/subharness's store.go owns the layout).
	//
	// ONE CATALOG, SEVERAL PLACES A PROGRAM CAN LIVE. A page is not a second kind
	// of subharness and nothing downstream of this constant may treat it as one:
	// it is found here rather than one directory over, it is the person's own
	// ([Layer.Provenance] answers `yours` for it as it does for the home store),
	// and every list draws it beside the bundles and the compiled-in workers
	// without a word about which is which.
	//
	// It is LAST because a page declares no schemas of its own, so a name carried
	// by both stores is better served by the bundle: the lookup order is the whole
	// of that decision and it lives here.
	LayerPages
)

// Provenance is the word a person reads for a layer. The packed trailer answers
// the same word the home store does on purpose: what a person wants to know from
// the mark is whether a program is the owner's, theirs, or their team's, and a
// bundle they packed into a binary themselves is still theirs.
func (l Layer) Provenance() Provenance {
	switch l {
	case LayerBuiltIn:
		return FromBinary
	case LayerProject:
		return FromProject
	default:
		return FromYou
	}
}

// GuardKind is what a guard actually checks. The four kinds are the cheap ones
// the PRD allows and no more: three are decided by looking, and the fourth is
// permitted one tiny model call. A guard that would cost real money is not a
// guard, it is the first step of the run.
type GuardKind string

const (
	// GuardFile passes when the named path exists.
	GuardFile GuardKind = "file"
	// GuardTool passes when the named tool is actually on this session's belt.
	GuardTool GuardKind = "tool"
	// GuardField passes when the named input field matches Pattern.
	GuardField GuardKind = "field"
	// GuardJudgement is the one rung that costs: one small question, answered
	// yes or no, about the input in hand. It is last because everything above it
	// is free.
	GuardJudgement GuardKind = "judgement"
)

// Guard is one cheap precondition, checked before the program is entered.
//
// THE FAILURE IS A FALLBACK AND NOT AN ERROR. When a guard does not pass, the
// work still gets done — by the general worker, with the original input — and
// what the person is told is that the step needed a closer look and was handled
// the long way. Nothing here may be drawn with the word "guard" in it, and
// [Guard.Because] exists so that the sentence a person reads was written by
// somebody who knew what the check was for.
type Guard struct {
	Kind GuardKind `json:"kind"`
	// File, Tool and Field name what is being looked at, one per kind.
	File  string `json:"file,omitempty"`
	Tool  string `json:"tool,omitempty"`
	Field string `json:"field,omitempty"`
	// Pattern is what a [GuardField] field has to match. It is a plain substring
	// test in the checker the runtime lane writes; a regular expression in a
	// manifest is a program a person did not know they were writing.
	Pattern string `json:"pattern,omitempty"`
	// Question is the whole of a [GuardJudgement]: one question, one word back.
	Question string `json:"question,omitempty"`
	// Because is what this check is FOR, in a person's words — "this one needs
	// the brief in the repository". It is the sentence the fallback line is
	// built from, and a guard without one falls back silently rather than
	// explaining itself in machinery vocabulary.
	Because string `json:"because,omitempty"`
}

// Validate says whether one guard is checkable at all, in the prose the author
// of the manifest needs to hear.
func (g Guard) Validate() error {
	switch g.Kind {
	case GuardFile:
		if strings.TrimSpace(g.File) == "" {
			return fmt.Errorf("a file check needs a path to look for")
		}
	case GuardTool:
		if strings.TrimSpace(g.Tool) == "" {
			return fmt.Errorf("a tool check needs the name of the tool")
		}
	case GuardField:
		if strings.TrimSpace(g.Field) == "" {
			return fmt.Errorf("a field check needs the name of the field")
		}
		if strings.TrimSpace(g.Pattern) == "" {
			return fmt.Errorf("a field check needs something to match against")
		}
	case GuardJudgement:
		if strings.TrimSpace(g.Question) == "" {
			return fmt.Errorf("a judgement needs the question to ask")
		}
	case "":
		return fmt.Errorf("a check has to say what it looks at: file, tool, field or judgement")
	default:
		return fmt.Errorf("%q is not something a check can look at — the kinds are file, tool, field and judgement", string(g.Kind))
	}
	return nil
}

// Schema is a JSON Schema, carried as the bytes it was written as.
//
// IT IS THE BYTES AND NOT A GO STRUCT, and that is the language-agnostic half of
// the contract doing its job: a bundle's manifest.json round-trips through this
// field unchanged, a Go runner's reflected schema arrives as the same bytes, and
// nothing in this tree has to own a model of JSON Schema in order for either to
// be stored. [Schema.Fields] reads out the little the intake card actually needs
// and is deliberately shallow — the card draws a form, it does not validate a
// document.
type Schema json.RawMessage

// MarshalJSON writes the schema as itself. A schema nobody declared is `null`,
// which is what an omitted field already means, so a manifest with no input
// schema and a manifest whose input schema is absent are one document.
func (s Schema) MarshalJSON() ([]byte, error) {
	if len(s) == 0 {
		return []byte("null"), nil
	}
	return []byte(s), nil
}

// UnmarshalJSON keeps a copy of the bytes. The copy matters: the decoder's
// buffer is reused, and a schema that aliased it would change under a manifest
// that had already been read.
func (s *Schema) UnmarshalJSON(data []byte) error {
	if s == nil {
		return fmt.Errorf("cannot read a schema into nothing")
	}
	if string(data) == "null" {
		*s = nil
		return nil
	}
	*s = append((*s)[0:0], data...)
	return nil
}

// Empty reports that nothing was declared. It is a fact and not a fault: a
// subharness that takes no input is a real thing, and the intake card for one
// has nothing to ask and launches straight away.
func (s Schema) Empty() bool { return len(strings.TrimSpace(string(s))) == 0 || string(s) == "null" }

// Validate says whether this is a schema at all, in prose. It checks that the
// bytes are JSON and that they describe an OBJECT, because every door on both
// sides — the intake card's fields, the adapter that fills them from an upstream
// task, the output the "done" surface renders — is written against named fields
// and has nothing to draw for a bare string.
func (s Schema) Validate() error {
	if s.Empty() {
		return nil
	}
	var shape schemaShape
	if err := json.Unmarshal([]byte(s), &shape); err != nil {
		return fmt.Errorf("this is not JSON: %w", err)
	}
	if shape.Type != "" && shape.Type != "object" {
		return fmt.Errorf("a subharness takes and returns named fields, so the schema has to be an object — this one says %q", shape.Type)
	}
	return nil
}

// schemaShape is as much of JSON Schema as this package reads: the type, the
// named properties, and which of them are required. Everything else in the
// document is carried through untouched, because the thing that validates a
// document against a schema is the runner, not the registry.
type schemaShape struct {
	Type       string                 `json:"type"`
	Properties map[string]schemaField `json:"properties"`
	Required   []string               `json:"required"`
	Order      []string               `json:"x-order"`
}

type schemaField struct {
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Default     json.RawMessage `json:"default"`
	Enum        []string        `json:"enum"`
}

// Field is one line of the intake card: what to ask for, whether it has to be
// answered, and what it means. It is the reason [Schema] is read at all in this
// package.
type Field struct {
	Name        string
	Type        string
	Title       string
	Description string
	Required    bool
	// Default is what the schema says to use when nobody says otherwise, in its
	// own JSON. It is nil when the schema named none, which the card draws as
	// nothing rather than as a guessed blank.
	Default json.RawMessage
	Enum    []string
}

// Fields reads the schema's named fields out, in a stable order.
//
// THE ORDER IS `x-order` WHERE THE SCHEMA STATES ONE AND ALPHABETICAL OTHERWISE,
// because Go decodes an object into a map and a card whose fields moved between
// two draws of the same subharness would be a card nobody could learn. A schema
// that cares about the order of its own form says so; one that does not gets an
// order that is at least the same every time.
func (s Schema) Fields() []Field {
	if s.Empty() {
		return nil
	}
	var shape schemaShape
	if err := json.Unmarshal([]byte(s), &shape); err != nil {
		return nil
	}
	required := make(map[string]bool, len(shape.Required))
	for _, name := range shape.Required {
		required[name] = true
	}
	names := make([]string, 0, len(shape.Properties))
	seen := make(map[string]bool, len(shape.Properties))
	for _, name := range shape.Order {
		if _, ok := shape.Properties[name]; ok && !seen[name] {
			names, seen[name] = append(names, name), true
		}
	}
	rest := make([]string, 0, len(shape.Properties))
	for name := range shape.Properties {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	names = append(names, rest...)

	fields := make([]Field, 0, len(names))
	for _, name := range names {
		property := shape.Properties[name]
		fields = append(fields, Field{
			Name:        name,
			Type:        property.Type,
			Title:       property.Title,
			Description: property.Description,
			Required:    required[name],
			Default:     property.Default,
			Enum:        property.Enum,
		})
	}
	return fields
}

// Validate says whether a manifest is one, in the words its author needs.
//
// EVERY MESSAGE IS A SENTENCE ABOUT WHAT IS MISSING, because the two readers are
// a person writing a bundle by hand and a model iterating against the error it
// got back. Neither is served by a code, and the model is served badly enough by
// a code that it will invent a field name to satisfy it.
func (m Manifest) Validate() error {
	name := strings.TrimSpace(m.Name)
	if name == "" {
		return fmt.Errorf("a subharness needs a name — it is the identity, and one namespace covers every one of them")
	}
	if err := validSubharnessName(name); err != nil {
		return err
	}
	if strings.TrimSpace(m.Purpose) == "" && name != LinearSubharness {
		return fmt.Errorf("%q needs a purpose: one line, and it is what every list and every card draws under the name", name)
	}
	if err := m.Input.Validate(); err != nil {
		return fmt.Errorf("%q's input schema: %w", name, err)
	}
	if err := m.Output.Validate(); err != nil {
		return fmt.Errorf("%q's output schema: %w", name, err)
	}
	for _, tool := range m.Whitelist {
		if strings.TrimSpace(tool) == "" {
			return fmt.Errorf("%q lists a tool with no name in it", name)
		}
	}
	for index, guard := range m.Guards {
		if err := guard.Validate(); err != nil {
			return fmt.Errorf("%q's check %d: %w", name, index+1, err)
		}
	}
	if m.DeadlineFloor < 0 || m.DeadlineStep < 0 || m.DeadlinePerTokens < 0 {
		return fmt.Errorf("%q asks for a negative amount of time", name)
	}
	return nil
}

// validSubharnessName holds the one namespace to one spelling.
//
// A NAME IS TYPED, SAID OUT LOUD, AND WRITTEN INTO A PATH. It is what somebody
// types after `/subharness`, what chat says when it proposes one, and the
// directory the bundle lives in — so the rules are the intersection of what all
// three can carry, and they are stated once, here, rather than discovered
// separately by the list, the store and the CLI.
func validSubharnessName(name string) error {
	if len(name) > 64 {
		return fmt.Errorf("%q is too long for a name — keep it under 64 characters", name)
	}
	if !unicode.IsLetter(rune(name[0])) {
		return fmt.Errorf("%q has to start with a letter", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		case r >= 'A' && r <= 'Z':
			return fmt.Errorf("%q has a capital in it — names are lowercase, so that one typed two ways is one subharness", name)
		case r == ' ':
			return fmt.Errorf("%q has a space in it — a name is one word, with dashes where you want the gaps", name)
		default:
			return fmt.Errorf("%q has a %q in it — names are lowercase letters, digits, dashes and underscores", name, string(r))
		}
	}
	return nil
}
