package verify

// The tree read as a VOCABULARY: which of its public names a sentence names.
//
// The surface reader beside this answers "what does this tree declare". This
// answers the question a request asks of that answer — a person writing *the
// vertical scrollbar position* has named `ScrollBar.position`, in their own
// words, and nothing in this program could see it. textual s16 asserted
// `scroll_y` and `max_scroll_y` a hundred and six times, never once touched the
// scrollbar widget's own `position`, and the two hidden checks that failed
// assert exactly that (`assert 0 == 4 where 0 = ScrollBar(...position=0).position`).
//
// The match is by TOKEN SEQUENCE and nothing else. A name is broken into the
// words it is built out of — dots, underscores, dashes and interior capitals are
// all word boundaries — and it is spoken by a sentence where consecutive words
// of the sentence spell exactly those words, in order. `ScrollBar.position` is
// spoken by "scrollbar position"; it is not spoken by "position", because a
// single word is a word and not a name, and promoting every common word in a
// request to a name of the tree is how a floor becomes a wall.

import (
	"strings"
	"unicode"
)

// spokenWindow is the longest run of a sentence's words this will read as one
// name. Four covers every qualified name a person writes in prose —
// `half open max requests`, `scroll bar position` — and past it the run is a
// clause rather than a name.
const spokenWindow = 4

// SurfaceIndex is a tree's public names keyed by the words they are made of, so
// a sentence can be asked which of them it names.
type SurfaceIndex struct {
	// full is the whole name's words joined: `ScrollBar.position` under
	// "scrollbarposition". It is what a sentence is matched against, because a
	// sentence that names only the tail of a qualified name has named a word.
	full map[string]string
	// member is the LAST part of a qualified name under the same joining:
	// `Log.is_following_end` under "isfollowingend". It is what a symbol the
	// request already spelled distinctively is confirmed against, since a person
	// writing `is_following_end` has named the member and left the class implied.
	member map[string]string
}

// IndexSurface reads a tree's surface into that vocabulary.
func IndexSurface(surface Surface) SurfaceIndex {
	index := SurfaceIndex{full: map[string]string{}, member: map[string]string{}}
	for file := range surface {
		for _, name := range surface.Names(file) {
			words := nameWords(name)
			if len(words) == 0 {
				continue
			}
			if key := strings.Join(words, ""); key != "" {
				// FIRST NAME WINS, and the order is the walk's. Two names that
				// spell the same words are the same name to a person writing
				// prose, and choosing between them is not something this can do
				// from the words alone.
				if _, held := index.full[key]; !held {
					index.full[key] = name
				}
			}
			if member := memberWords(name); len(member) > 0 {
				if key := strings.Join(member, ""); key != "" {
					if _, held := index.member[key]; !held {
						index.member[key] = name
					}
				}
			}
		}
	}
	return index
}

// Empty reports that no tree could be read, in which case every caller behaves
// exactly as it did before this existed.
func (i SurfaceIndex) Empty() bool { return len(i.full) == 0 && len(i.member) == 0 }

// Holds confirms a name the request already spelled distinctively: the tree
// declares it, either whole or as the member of something.
//
// It is what keeps a hyphenated English compound out. `full-width` is spelled
// exactly like a name and is not one; the tree is what says so, and no list of
// words is consulted.
func (i SurfaceIndex) Holds(symbol string) (string, bool) {
	words := nameWords(symbol)
	if len(words) == 0 {
		return "", false
	}
	key := strings.Join(words, "")
	if name, held := i.full[key]; held {
		return name, true
	}
	if name, held := i.member[key]; held {
		return name, true
	}
	return "", false
}

// Spoken is every public name this sentence names in words, in the order the
// sentence names them.
//
// TWO WORDS AT LEAST, AND A QUALIFIED NAME. One word of a sentence is a word —
// "position", "log", "size", "write" — and reading it as a name of the tree
// would make an observable of nearly every noun a request contains. And a name
// with only one part is a TYPE: textual's request says "after users scroll up",
// textual declares a `ScrollUp` message, and the two have nothing to do with
// each other. `ScrollBar.position` and `RichLog.write` are members somebody can
// read a value off, and a person who spelled a bare class name spelled it as one
// word, where the symbol reader already has it.
func (i SurfaceIndex) Spoken(text string) []string {
	words := sentenceWords(text)
	var spoken []string
	seen := map[string]bool{}
	for start := range words {
		for length := 2; length <= spokenWindow && start+length <= len(words); length++ {
			key := strings.Join(words[start:start+length], "")
			name, held := i.full[key]
			if !held || seen[name] || !Qualified(name) {
				continue
			}
			seen[name] = true
			spoken = append(spoken, name)
		}
	}
	return spoken
}

// Qualified says this name has an owner — `ScrollBar.position`, `Igel().results_path`,
// `Type::method` — rather than standing alone at the top level of a module.
func Qualified(name string) bool {
	return len(ownerParts(name)) > 1
}

// nameWords breaks a declaration's name into the words it is built out of. Every
// separator this program's readers put in a name is a boundary — the dot of
// `ScrollBar.position`, the `()` of `Igel().results_path`, the `::` of
// `Type::method`, an underscore, a dash — and so is an interior capital.
func nameWords(name string) []string {
	var words []string
	for _, part := range strings.FieldsFunc(name, nameBoundary) {
		words = append(words, splitCase(part)...)
	}
	return words
}

// memberWords is the last part of a qualified name, in the same words.
func memberWords(name string) []string {
	parts := ownerParts(name)
	if len(parts) < 2 {
		return nil
	}
	return nameWords(parts[len(parts)-1])
}

// ownerParts splits a name where its OWNER ends and the thing it owns begins.
// Only the separators this program's readers use to spell that relation count —
// the dot of `ScrollBar.position`, the call of `Igel().results_path`, the `::` of
// `Type::method`. An underscore or a dash is inside one part, not between two:
// `is_following_end` is one name and `Log.is_following_end` is two.
func ownerParts(name string) []string {
	var parts []string
	for _, part := range strings.FieldsFunc(name, func(run rune) bool {
		return run == '.' || run == ':' || run == '(' || run == ')'
	}) {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func nameBoundary(run rune) bool {
	return !unicode.IsLetter(run) && !unicode.IsDigit(run)
}

// splitCase breaks one part on its interior capitals, so `ScrollBar` is two
// words and `HTTPServer` is two rather than nine. A run of capitals belongs to
// the word it leads into.
func splitCase(part string) []string {
	runes := []rune(part)
	var words []string
	start := 0
	for index := 1; index < len(runes); index++ {
		if !unicode.IsUpper(runes[index]) {
			continue
		}
		// A capital after a small letter or a digit opens a word; a capital
		// inside a run of them opens one only where a small letter follows.
		opens := !unicode.IsUpper(runes[index-1])
		if !opens && index+1 < len(runes) && unicode.IsLower(runes[index+1]) {
			opens = true
		}
		if opens && index > start {
			words = append(words, strings.ToLower(string(runes[start:index])))
			start = index
		}
	}
	if start < len(runes) {
		words = append(words, strings.ToLower(string(runes[start:])))
	}
	return words
}

// sentenceWords is a person's own text as the words a name could be spelled in:
// runs of letters and digits, lower-cased, with everything else a boundary.
func sentenceWords(text string) []string {
	var words []string
	for _, field := range strings.FieldsFunc(text, nameBoundary) {
		words = append(words, strings.ToLower(field))
	}
	return words
}
