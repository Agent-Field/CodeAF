package store

import (
	"fmt"
	"strings"
)

// Admission control at the funnel door.
//
// This guard used to live in the head's spawn tool, which meant it protected
// exactly one caller: the conversation. Everything else that can commission
// work — `aforge do` running headless, a question answered into a continuation,
// a surface with its own composer — journaled twins freely, and the person paid
// for both. The funnel is the one seam between asking for work and getting it
// (chat-simplify §2.5), so the duplicate check belongs on this side of it.
//
// The window it guards is real: a splice sits pending for as long as the
// workforce takes to reach it, and every re-ask inside that window is a re-ask
// that could honestly believe the work does not exist yet. The live case that
// wrote it (2026-08-11) was "get me a list of X", then a minute later "ok start
// it" — two identical twenty-stock runs, two plans, two bills.

// pendingSpliceRead bounds the pending read. A session with more than this many
// splices waiting has a stalled workforce, and the guard degrades to catching
// the newest of them — which is the one a re-ask would twin.
const pendingSpliceRead = 32

// pendingSpliceTwin reports that this ask is already waiting, unstarted, in the
// same session, and names the command it duplicates.
func (s *Store) pendingSpliceTwin(sessionID, instruction string) (Command, bool, error) {
	words := askContentWords(instruction)
	if len(words) < 3 {
		return Command{}, false, nil
	}
	pending, err := s.queryCommands(
		`kind = ? AND status = ? AND session_id = ? ORDER BY seq DESC LIMIT ?`,
		[]any{CommandSplice, CommandPending, sessionID, pendingSpliceRead})
	if err != nil {
		return Command{}, false, err
	}
	for _, command := range pending {
		if sameAsk(words, askContentWords(command.Instruction)) {
			return command, true, nil
		}
	}
	return Command{}, false, nil
}

// sameAsk is the twin test: shared content words. Exact words are too strict
// across turns — a head re-reads the request each time it speaks — so the test
// is that most of the shorter sentence's meaningful words appear in the other.
// The bar is deliberately high (two thirds, and at least three shared words)
// because a false twin silently swallows a genuinely new job, which is worse
// than the duplicate it exists to prevent: a duplicate at least shows up on the
// board.
func sameAsk(words, theirs map[string]bool) bool {
	if len(words) < 3 || len(theirs) < 3 {
		return false
	}
	shorter, longer := words, theirs
	if len(theirs) < len(words) {
		shorter, longer = theirs, words
	}
	shared := 0
	for word := range shorter {
		if longer[word] {
			shared++
		}
	}
	return shared >= 3 && shared*3 >= len(shorter)*2
}

// askContentWords is the sentence as a set of meaningful lowercased words —
// what survives when the connective tissue is taken out.
func askContentWords(sentence string) map[string]bool {
	words := make(map[string]bool)
	for _, word := range strings.FieldsFunc(strings.ToLower(sentence), func(r rune) bool {
		return !('a' <= r && r <= 'z' || '0' <= r && r <= '9')
	}) {
		if len(word) < 3 || askStopWords[word] {
			continue
		}
		words[word] = true
	}
	return words
}

var askStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "that": true, "this": true,
	"with": true, "have": true, "from": true, "please": true, "you": true,
	"can": true, "get": true, "our": true, "are": true, "was": true,
	"will": true, "would": true, "should": true, "into": true, "out": true,
	"all": true, "any": true, "its": true, "then": true, "them": true,
}

// duplicateAsk is the refusal, worded for whoever submitted it. It names the
// waiting command so a caller can say the true thing — the work exists — rather
// than reporting a failure.
func duplicateAsk(twin Command) error {
	return fmt.Errorf("request command: %w: command %d is already waiting on the same ask",
		ErrDuplicate, twin.Seq)
}
