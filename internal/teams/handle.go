package teams

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// A handle is how a member is named inside its team, in the Traffic log and to
// the team tools: ONE lowercase word that names what the conversation is about
// (@security, @milestones, @gravity), short enough to type and stable enough to
// refer back to.
//
// IT IS CHOSEN TWICE, ONCE AT ONCE AND ONCE WELL. The word list here
// ([DeriveHandle]) guesses one the moment the member has a title, so the member
// is addressable at once. The conversation's own title model then chooses the
// word, once, when the conversation's title is made (internal/session's
// handlepick.go), through [File.ChooseHandle]; the manager and the members are
// told of the change in the Traffic log. A word list cannot tell a subject from
// the kind of work done to it, and the first handles made from real titles were
// @review, @reviewing and @session.
//
// A HANDLE A PERSON OR THE MANAGER GAVE IS NEVER REPLACED ([HandleByTyped]),
// and a handle the model chose is not chosen again, so a line in the log keeps
// meaning the member it meant.
//
// A member that joins before its conversation has a title has no handle yet; it
// takes one the first time it is saved with a title.

// Who chose a member's handle ([Member.HandleBy]).
const (
	HandleByWords = "words" // the word list's instant guess ([DeriveHandle])
	HandleByModel = "model" // the title model's word ([File.ChooseHandle])
	HandleByTyped = "typed" // given by a person or the manager; never replaced
)

// HandleDerived reports whether m's handle is the word list's guess, which the
// title model may replace once ([File.ChooseHandle]). A handle written before
// [Member.HandleBy] was kept reads as one: the only handles then were derived,
// but for the manager's own starts, which it names again the same way.
func (m Member) HandleDerived() bool {
	return m.HandleBy == "" || m.HandleBy == HandleByWords
}

// Handle lengths, in characters.
const (
	HandleMin = 2
	HandleMax = 12
)

// Reserved addresses are words the Traffic log uses for someone who is not a
// member, so no member may take one as its handle.
var reservedHandles = map[string]bool{
	FromManager: true, FromYou: true, FromSystem: true, ToEveryone: true, ToRoom: true,
}

// stopWords are dropped when a handle is derived from a title.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
	"of": true, "to": true, "in": true, "on": true, "for": true, "with": true,
	"at": true, "by": true, "from": true, "into": true, "about": true, "as": true,
	"is": true, "are": true, "be": true, "it": true, "this": true, "that": true,
	"my": true, "our": true, "your": true, "its": true, "please": true,
	"can": true, "could": true, "would": true, "should": true, "will": true,
	"i": true, "we": true, "you": true, "me": true, "us": true, "let": true,
	"lets": true, "some": true, "how": true, "what": true, "why": true,
}

// ValidHandle says what is wrong with h as a handle, or nil. A handle is
// lowercase letters, digits and hyphens, starts with a letter or digit, is
// HandleMin to HandleMax characters, and is not a reserved address.
func ValidHandle(h string) error {
	if err := handleShape(h); err != nil {
		return err
	}
	if reservedHandles[h] {
		return fmt.Errorf("%s is reserved", h)
	}
	return nil
}

// handleShape is [ValidHandle] without the reserved words.
func handleShape(h string) error {
	if len(h) < HandleMin || len(h) > HandleMax {
		return fmt.Errorf("a handle is %d to %d characters", HandleMin, HandleMax)
	}
	for i, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0:
		default:
			return errors.New("a handle is lowercase letters, digits and hyphens")
		}
	}
	return nil
}

// handleProblem says why h cannot be the handle of the member with key in t.
func handleProblem(t Team, key, h string) error {
	if err := ValidHandle(h); err != nil {
		return err
	}
	for _, m := range t.Members {
		if m.Key != key && m.Handle == h {
			return fmt.Errorf("%s is already the handle of another member", h)
		}
	}
	return nil
}

// fillerWords are words a title is made of that name no piece of work: the
// verbs a person opens a request with (checking, review, fix), and the words
// nearly every title on one machine shares (codeaf, repo, agent). A handle made
// of one of them says nothing about which conversation it is, which is what the
// first handles made from real titles were: @checking, @review, @agent.
var fillerWords = map[string]bool{
	"check": true, "checking": true, "checked": true, "checks": true,
	"review": true, "reviewing": true, "reviewed": true, "reviews": true,
	"look": true, "looking": true, "see": true, "tell": true, "show": true,
	"help": true, "helping": true, "try": true, "trying": true, "want": true, "need": true,
	"make": true, "making": true, "get": true, "getting": true, "do": true, "doing": true,
	"run": true, "running": true, "find": true, "finding": true, "add": true, "adding": true,
	"fix": true, "fixing": true, "refactor": true, "refactoring": true,
	"update": true, "updating": true, "write": true, "writing": true,
	"explain": true, "explaining": true, "investigate": true, "investigating": true,
	"debug": true, "debugging": true, "happening": true, "going": true,
	"agent": true, "agents": true, "chat": true, "conversation": true, "question": true,
	"codeaf": true, "code": true, "repo": true, "repository": true, "project": true,
	"thing": true, "things": true, "stuff": true, "work": true, "task": true,
	"new": true, "hey": true, "hi": true, "hello": true, "whats": true,
	"here": true, "there": true, "now": true, "just": true, "also": true, "again": true,
}

// genericHeads are nouns that end a title without saying what it is about: in
// "Fix the login bug" the bug is not the subject, the login is, and in "lexer
// rewrite" the rewrite is what is done to the lexer.
var genericHeads = map[string]bool{
	"bug": true, "bugs": true, "issue": true, "issues": true, "problem": true,
	"problems": true, "error": true, "errors": true, "support": true,
	"rewrite": true, "sweep": true, "cleanup": true, "pass": true, "audit": true,
	"change": true, "changes": true, "plan": true, "notes": true, "draft": true,
	"overview": true, "summary": true, "polish": true, "tweaks": true, "wip": true,
}

// handleWordMin is the shortest word a handle is made of on its own. A shorter
// one is a fragment ("can you te" gave @te) or a qualifier, and a qualifier
// only ever rides in front of the word it qualifies (@qa-binary, @api-docs).
const handleWordMin = 3

// DeriveHandle is the handle a title suggests, before collisions and reserved
// words are taken into account.
//
// IT IS THE TITLE'S HEAD NOUN, as nearly as a word list can find it: the last
// word that is not a function word, a filler word ([fillerWords]) or a
// fragment, because an English title names its subject last ("checking codeaf
// branches for qa binary" is about the binary). A generic last word ("bug",
// "support") gives way to the one before it. A short qualifier right in front
// of the head rides with it when both fit (@qa-binary, @api-docs). The result
// is lowercased and cut to HandleMax. A title with no usable word gives "chat";
// an empty title gives "".
func DeriveHandle(title string) string {
	if strings.TrimSpace(title) == "" {
		return ""
	}
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	usable := func(w string) bool {
		return !stopWords[w] && !fillerWords[w] && strings.Trim(w, "0123456789") != ""
	}
	head := -1
	for i := len(words) - 1; i >= 0; i-- {
		w := words[i]
		if len(w) < handleWordMin || !usable(w) {
			continue
		}
		if head < 0 {
			head = i
		}
		if !genericHeads[w] {
			head = i
			break
		}
	}
	if head < 0 {
		return "chat"
	}
	base := words[head]
	if head > 0 {
		prev := words[head-1]
		if len(prev) >= HandleMin && len(prev) <= handleWordMin && usable(prev) && len(prev)+1+len(base) <= HandleMax {
			base = prev + "-" + base
		}
	}
	base = cutHandle(base, HandleMax)
	if handleShape(base) != nil {
		return "chat"
	}
	return base
}

// cutHandle is h cut to n characters with no hyphen left at the end.
func cutHandle(h string, n int) string {
	if len(h) > n {
		h = h[:n]
	}
	return strings.TrimRight(h, "-")
}

// uniqueHandle is base, or base with the lowest number from 2 that no member
// of t other than key uses, cut so the whole stays within HandleMax.
func uniqueHandle(t Team, key, base string) string {
	if handleProblem(t, key, base) == nil {
		return base
	}
	for n := 2; ; n++ {
		suffix := strconv.Itoa(n)
		h := cutHandle(base, HandleMax-len(suffix)) + suffix
		if handleProblem(t, key, h) == nil {
			return h
		}
	}
}

// assignHandles clears every handle in t that is invalid or repeats one an
// earlier member has, then gives each member with a title and no handle one,
// in member order. It reports whether it changed anything.
func assignHandles(t *Team) bool {
	changed := false
	seen := map[string]bool{}
	for i := range t.Members {
		h := t.Members[i].Handle
		if h == "" {
			continue
		}
		if ValidHandle(h) != nil || seen[h] {
			t.Members[i].Handle = ""
			changed = true
			continue
		}
		seen[h] = true
	}
	for i := range t.Members {
		m := &t.Members[i]
		if m.Handle != "" {
			continue
		}
		base := DeriveHandle(m.Word)
		if base == "" {
			continue
		}
		m.Handle, m.HandleBy = uniqueHandle(*t, m.Key, base), HandleByWords
		changed = true
	}
	return changed
}

// ChooseHandle gives the member with key in team id the title model's word:
// the first of choices, best first, that no other member of the team has.
// When every choice is taken, the first is qualified by a word of the title in
// front of it (@api-security), as [DeriveHandle] qualifies; only when nothing
// fits is it numbered. old is the handle the member had and now the one it has;
// a member whose handle was given ([HandleByTyped]) or already chosen by the
// model keeps it, and so does a member whose handle is already the choice.
func (f *File) ChooseHandle(id, key string, choices []string, title string) (old, now string, err error) {
	i, err := f.at(id)
	if err != nil {
		return "", "", err
	}
	t := &f.Teams[i]
	j := t.member(key)
	if j < 0 {
		return "", "", fmt.Errorf("%s is not in team %s", key, t.Name)
	}
	m := &t.Members[j]
	old = m.Handle
	if !m.HandleDerived() && m.Handle != "" {
		return old, old, nil
	}
	var usable []string
	for _, c := range choices {
		if ValidHandle(c) == nil && !strings.Contains(c, "-") {
			usable = append(usable, c)
		}
	}
	if len(usable) == 0 {
		return old, old, errors.New("no usable handle among the choices")
	}
	now = pickHandle(*t, key, usable, title)
	m.Handle, m.HandleBy = now, HandleByModel
	return old, now, nil
}

// pickHandle is the first free choice, else the first qualified by a title
// word, else the first numbered.
func pickHandle(t Team, key string, choices []string, title string) string {
	for _, c := range choices {
		if handleProblem(t, key, c) == nil {
			return c
		}
	}
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	for _, c := range choices {
		for _, q := range words {
			if q == c || len(q) < handleWordMin || stopWords[q] || fillerWords[q] || strings.Trim(q, "0123456789") == "" {
				continue
			}
			if h := q + "-" + c; len(h) <= HandleMax && handleProblem(t, key, h) == nil {
				return h
			}
		}
	}
	return uniqueHandle(t, key, choices[0])
}
