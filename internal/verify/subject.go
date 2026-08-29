package verify

// What a check is ABOUT, read out of the runner's own name grammar.
//
// A roster subtraction says which names stopped being reported. It cannot say
// what that means, and the two things it cannot tell apart cost opposite
// amounts: a check that was DELETED leaves its subject held by nothing, and a
// check that was REWRITTEN leaves its subject held by the thing that replaced
// it. happy-dom's v4-flash s13 run rewrote four stubs — `IntersectionObserver
// observe() Does nothing` and its three siblings — into real checks under the
// identical describe path, and the gate failed the delivery four rounds running
// with `This work removed checks that existed before it: …` over a tree the
// grader scored 9 of 9.
//
// So the comparison is made on SUBJECTS rather than on whole names. Every
// runner this program reads spells a check's identity hierarchically — a
// describe chain, a pytest node id, a Go test and its subtests — and the
// hierarchy is the runner's own statement of what the check is about. What is
// dropped is the leaf title's trailing CLAUSE, the assertion-free sentence a
// person writes after the thing being checked, and it is dropped by structure:
// it is whatever follows the leading run of code-shaped tokens. Nothing here
// holds a list of English words, which is the failure FAILSAFE's first clause
// is about.

import "strings"

// hierarchySeparators are the ways a runner spells "inside": pytest and rust
// node ids, gradle's and this package's own ancestor join, Go subtests. They
// are read for the LAST occurrence, because a name is a path and its leaf is
// whatever follows the deepest one — `github.com/x/y.TestA/sub` splits at the
// subtest and never at the import path.
var hierarchySeparators = []string{"::", " > ", "/"}

// sentencePunctuation is what a person ends a clause with, and it is trimmed off
// a token before the token is judged. `nothing.` is prose wearing a full stop;
// `RichLog.write` is a member access. Judging the raw token would confuse them.
const sentencePunctuation = ".,:;!?"

// CheckSubject is what a check is about: its parent path, plus the leading
// code-shaped tokens of its leaf title.
//
// ok is false where the name carries NO hierarchy — no separator, and a leaf
// that opens with prose. A TAP line reading `the widget renders` names one flat
// thing, there is nothing in it to compare a sibling against, and this mechanism
// is absent rather than guessing: such a name subtracts exactly as it always
// did. FAILSAFE clause 1 in the direction that costs nothing.
//
// The subject is returned as a PREFIX of the name itself rather than as a
// rebuilt string, so two names carrying the same subject carry it byte for byte
// and the comparison is an equality rather than a normalisation.
func CheckSubject(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	head := deepestSeparator(name)
	cut, tokens := head, 0
	// The leaf's leading code-shaped tokens belong to the subject; the clause
	// after them does not. `IntersectionObserver observe() Does nothing.` is a
	// call under a class, then a sentence about it.
	for offset := head; offset < len(name); {
		start := offset + len(name[offset:]) - len(strings.TrimLeft(name[offset:], " \t"))
		if start >= len(name) {
			break
		}
		end := start + len(name[start:])
		if space := strings.IndexAny(name[start:], " \t"); space >= 0 {
			end = start + space
		}
		if !codeShaped(name[start:end]) {
			break
		}
		cut, offset, tokens = end, end, tokens+1
	}
	// A SUBJECT READ OUT OF WHITESPACE ALONE NEEDS TWO CODE-SHAPED TOKENS. A
	// separator is the runner SAYING this name is a path, and one level of it —
	// a pytest file, a Go test function — is a subject. Whitespace says nothing,
	// so a single leading code name there is the top-level describe every check
	// in the file shares, and treating it as a subject would acquit the deletion
	// of any check in a file that still holds one. `IntersectionObserver
	// observe()` is a subject; `IntersectionObserver` is a file.
	if head == 0 && tokens < 2 {
		return "", false
	}
	subject := strings.TrimRight(strings.TrimSpace(name[:cut]), " \t")
	// A separator left standing on the end is the path spelled with nothing
	// under it, and it compares as its own thing. Trim it so `A::B::` and
	// `A::B` are one subject.
	for _, separator := range hierarchySeparators {
		subject = strings.TrimRight(subject, strings.TrimSpace(separator))
	}
	subject = strings.TrimSpace(subject)
	if subject == "" || cut == 0 {
		return "", false
	}
	return subject, true
}

// deepestSeparator is the offset just past the last hierarchy separator in a
// name, or 0 where it holds none.
func deepestSeparator(name string) int {
	deepest := 0
	for _, separator := range hierarchySeparators {
		if index := strings.LastIndex(name, separator); index >= 0 {
			deepest = max(deepest, index+len(separator))
		}
	}
	return deepest
}

// codeShaped says a token is a name a program spells rather than a word a
// person writes, BY ITS SHAPE and never by its letters.
//
// Three shapes, and every one of them is punctuation or capitalisation a
// sentence does not carry: a call or a subscript, a member access or a
// qualified name, and an inner capital — camelCase, CamelCase, an acronym. A
// capital in first position is not one of them, because that is how a sentence
// starts: `Does` is prose and `IntersectionObserver` is not.
//
// Snake_case is deliberately absent. `test_does_nothing` is how pytest and Go
// subtests spell a SENTENCE, so reading it as a code name would make every such
// leaf its own subject and this whole mechanism a no-op on the two largest
// ecosystems here.
func codeShaped(token string) bool {
	token = strings.Trim(token, sentencePunctuation)
	if token == "" {
		return false
	}
	if strings.ContainsAny(token, "()[]<>.#$") {
		return true
	}
	for index, letter := range token {
		if index > 0 && letter >= 'A' && letter <= 'Z' {
			return true
		}
	}
	return false
}

// Replacement is one check that stopped being reported and the checks that took
// its subject over.
type Replacement struct {
	Before string   `json:"before"`
	After  []string `json:"after,omitempty"`
}

// SplitReplaced sorts the names a roster stopped reporting into the ones
// NOTHING covers and the ones a later check does.
//
// THE RULE: A CHECK THAT EXISTED BEFORE AND IS ABSENT AFTER IS REMOVED ONLY IF
// NOTHING AFTER IT COVERS ITS SUBJECT. A check whose subject the after roster
// still names was rewritten, not deleted — and whether the thing that replaced
// it PASSES is a different question with a different finding already behind it
// (see Reading.OwnFailing).
//
// A name with no hierarchy to read is removed, exactly as it always was.
func SplitReplaced(gone, after []string) (removed []string, replaced []Replacement) {
	heirs := map[string][]string{}
	for _, name := range after {
		if subject, ok := CheckSubject(name); ok {
			heirs[subject] = append(heirs[subject], name)
		}
	}
	for _, name := range gone {
		subject, ok := CheckSubject(name)
		if !ok || len(heirs[subject]) == 0 {
			removed = append(removed, name)
			continue
		}
		taken := heirs[subject]
		if len(taken) > ReplacementsNamed {
			taken = taken[:ReplacementsNamed]
		}
		replaced = append(replaced, Replacement{Before: name, After: append([]string{}, taken...)})
	}
	return removed, replaced
}

// ReplacementsNamed bounds how many heirs one replacement records.
//
// A journal row is read to learn WHAT SHAPE the replacement had — whether one
// stub became one check or one stub became nine — and four settles that as well
// as forty. It is the same reasoning store.VerificationSample spells for a
// roster sample, at the size internal/revision spells for a list of observables.
const ReplacementsNamed = 4
