package plan

import "strings"

// A NODE WHOSE OWN WORDS NAME TWO OR MORE PARTS IS NOT ONE SITTING UNTIL THE
// STAGE QUESTION SAYS SO.
//
// The burden of proof reads two facts about a node — the parts the ruler named
// for it and the size the ruler gave it — and on a goal that hands three
// disjoint lanes over one large file it can get both of them wrong at once. The
// spine answers with a single stage whose title and summary ENUMERATE the three
// lanes ("North: …; South: …; East: …"), the sizing pass calls that one stage
// atomic and names no parts for it, and JudgeSplit then refuses it as unnamed —
// so the stage question, which exists precisely to divide what cannot be
// finished in one sitting, is never asked. Measured on a 144 KB register:
// three lanes planned as one leaf on two draws of three.
//
// The node's own words are the evidence nobody was reading. They cost no call
// to read: this file is the reader, and it is pure, deterministic and asked of
// nothing but a title and a summary.
//
// WHAT IT DOES NOT DO is decide the division. Reading two names out of a
// summary is evidence that the node MIGHT be several sittings, never a finding
// that it is; the finding belongs to the stage question in sequence.go, which
// answers with the stages or answers with one piece — and one piece is the
// refusal that gets journaled, exactly as it always was. So the cost of reading
// an enumeration where there is none is one model call and a refusal, which is
// the cheap side of this trade; the cost of missing one is a leaf that runs
// against a window it has already been measured past.
//
// It is deliberately NOT splitgate.Items, which counts how many things an ASK
// enumerates in order to decide whether parallelism pays for itself. That is a
// different question with a different floor, and answering this one with it
// would tie a structural reading of one node's words to a bench-calibrated
// number about whole goals.

// piecesNamed reads the pieces a node's own words enumerate, and returns their
// labels in the order they were written. Fewer than two is "this node names no
// division of itself", which is the answer for almost every node.
//
// Two shapes are read, and they are the two the planner's own passes write:
//
//   - Part sentences separated by semicolons, each one labelled: "North:
//     Rewrite North dates.; South: Sort South by quantity.; East: Prefix low
//     items." EVERY segment has to carry a label, which is what keeps an
//     ordinary sentence with a colon and a semicolon in it from reading as a
//     division.
//   - A labelled list — "L1", "L2", "L3", or "step1"/"step2" — where the same
//     name is worn by two or more numbers.
//
// A bare comma list is deliberately NOT read. "Sort the South block by
// quantity, renumber the ids" is one sitting written with a comma, and there is
// no reading of the punctuation alone that separates it from "North dates,
// South sort, East prefix" — so the comma list reaches this file only when its
// items are labelled, which is how the spine writes it when they are genuinely
// separate pieces.
func piecesNamed(title, summary string) []string {
	if pieces := labelledSentences(summary); len(pieces) >= 2 {
		return pieces
	}
	if pieces := labelledSentences(title); len(pieces) >= 2 {
		return pieces
	}
	if pieces := labelledList(title + "\n" + summary); len(pieces) >= 2 {
		return pieces
	}
	return nil
}

// namesSeveralPieces is piecesNamed as the predicate its three callers ask.
func namesSeveralPieces(title, summary string) bool {
	return len(piecesNamed(title, summary)) >= 2
}

// labelledSentences reads "X: …; Y: …" and returns X and Y.
//
// The rule is that every segment must be labelled and every label must look
// like a name rather than a sentence — no comma, no full stop, no question or
// exclamation inside it — because the shape being recognised is a writer
// naming the pieces, not a writer using a colon. The text after the colon is
// only required to be there: what a piece says about itself is its own
// business, and it routinely carries colons of its own ("append one line `#
// East total: <sum>`"), which is why the label is taken at the FIRST colon and
// the rest is left whole.
func labelledSentences(text string) []string {
	segments := strings.Split(text, ";")
	if len(segments) < 2 {
		return nil
	}
	labels := make([]string, 0, len(segments))
	seen := make(map[string]bool, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return nil
		}
		colon := strings.Index(segment, ":")
		if colon < 0 {
			return nil
		}
		label := strings.TrimSpace(segment[:colon])
		rest := strings.TrimSpace(segment[colon+1:])
		if label == "" || rest == "" || strings.ContainsAny(label, ".,!?") {
			return nil
		}
		if seen[strings.ToLower(label)] {
			return nil
		}
		seen[strings.ToLower(label)] = true
		labels = append(labels, label)
	}
	return labels
}

// labelledList reads a list whose items are marked with one name and a number —
// L1, L2, L3 — and returns the marks in the order they were written.
//
// A mark is a whole word of letters immediately followed by digits, and it
// counts only when the same letters are worn by two or more different numbers,
// so a lone "v2" or "UTF8" names nothing. Words that mix the two any other way
// — "S-0001", "R1a", "YYYY-MM-DD" — are not marks and are not read.
func labelledList(text string) []string {
	type mark struct {
		name   string
		number string
	}
	var marks []mark
	numbers := make(map[string]map[string]bool)
	for _, word := range alnumWords(text) {
		letters := 0
		for letters < len(word) && isLetter(word[letters]) {
			letters++
		}
		if letters == 0 || letters > 4 || letters == len(word) {
			continue
		}
		digits := word[letters:]
		if len(digits) > 2 {
			continue
		}
		allDigits := true
		for index := 0; index < len(digits); index++ {
			if !isDigit(digits[index]) {
				allDigits = false
				break
			}
		}
		if !allDigits {
			continue
		}
		name := strings.ToLower(word[:letters])
		if numbers[name] == nil {
			numbers[name] = make(map[string]bool)
		}
		numbers[name][digits] = true
		marks = append(marks, mark{name: name, number: digits})
	}
	var kept []string
	seen := make(map[string]bool, len(marks))
	for _, found := range marks {
		if len(numbers[found.name]) < 2 || seen[found.name+found.number] {
			continue
		}
		seen[found.name+found.number] = true
		kept = append(kept, found.name+found.number)
	}
	return kept
}

// alnumWords cuts text into maximal runs of letters and digits. Everything else is a
// separator, which is what keeps "S-0001" two words and "L1" one.
func alnumWords(text string) []string {
	var out []string
	start := -1
	for index := 0; index < len(text); index++ {
		char := text[index]
		if isLetter(char) || isDigit(char) {
			if start < 0 {
				start = index
			}
			continue
		}
		if start >= 0 {
			out = append(out, text[start:index])
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, text[start:])
	}
	return out
}

func isLetter(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

func isDigit(char byte) bool { return char >= '0' && char <= '9' }

// admitsEnumeratedPieces is the enumerated reading as the two burden questions
// are allowed to ask it, and it is the ONE PLACE that says which builds may
// divide a node because of the words written on it.
//
// A REMAINDER DIVIDES ON ITS SIZE AND NEVER ON ITS WORDS. Options.Undivided
// marks the one build that is a remainder — the replan a leaf that ran out of
// room asks for, and the only caller in the tree that sets it — and a
// remainder's words are a LIST OF WHAT IS LEFT. A fresh plan that names three
// lanes is a goal with three lanes in it, which is what this reading was built
// for; a remainder that names eight failing tests is one worker's assignment
// written out, and reading that list as eight jobs is how a repair round drew
// six leaves and then eight and ran fourteen repairs in parallel to the wall.
// Measured on the canary: the do door's spend doubled, $0.46 to $0.95 over nine
// cells, with quality flat.
//
// What stays is the ruler's reach. A remainder the ruler puts past one worker
// is still divided — into simultaneous parts where it has them and into ordered
// stages where it does not — because that is a judgment about size, which is
// the only judgment a remainder is divided on.
func admitsEnumeratedPieces(node *Node, options Options) bool {
	if node == nil || options.Undivided {
		return false
	}
	return namesSeveralPieces(node.Title, node.Summary)
}
