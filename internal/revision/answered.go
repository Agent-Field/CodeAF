package revision

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// PointOutcome is what became of one thing the request asked for.
type PointOutcome struct {
	Behaviour string `json:"behaviour"`
	State     string `json:"state"`
	Why       string `json:"why,omitempty"`
}

const (
	PointAnswered    = "answered"
	PointNotAnswered = "not answered"
	PointNotReached  = "not reached"
)

// ChecklistHeading is the one heading every person's account begins with.
// Readers use the same spelling to keep an account already carried by a failed
// node from being said a second time in the headless deliverable.
const ChecklistHeading = "What was asked for, and what happened to each:"

const (
	// behaviourWords leaves a long request point recognisable without letting one
	// point consume the bounded account meant for the whole checklist.
	behaviourWords = 140
	// checkWords keeps the gate's check identifiable while preserving room for
	// the other points the same gate read.
	checkWords = 80
	// checklistBlockBytes is HALF of what a node's error may hold, and it is
	// interpolated from that bound rather than typed: the account rides inside a
	// failed node's recorded error (store.Fail bounds it at store.MaxDigestBytes),
	// beside the cause, the transport and the file list, and a figure written
	// here by hand would go on claiming a share of a budget that had moved.
	checklistBlockBytes = store.MaxDigestBytes / 2
)

// AnswerChecklist reads what became of each journaled point from facts the run
// already recorded. A check may answer a point; a file comparison may only say
// whether the run wrote a named file, and every other shape stays not reached.
func AnswerChecklist(points []store.AcceptancePoint, gate store.DeliveryGate, gateRead bool, wrote []string) []PointOutcome {
	outcomes := make([]PointOutcome, 0, len(points))
	for _, point := range points {
		outcome := PointOutcome{Behaviour: point.Behaviour, State: PointNotReached}
		if gateRead {
			if exercised, found := exercisedPoint(point.Behaviour, gate.Exercises); found {
				if check := strings.TrimSpace(exercised.Check); check != "" {
					outcome.State = PointAnswered
					outcome.Why = "a check covers it: " + clipUTF8Bytes(check, checkWords)
				} else {
					outcome.State = PointNotAnswered
					outcome.Why = "no check exercises it"
				}
				outcomes = append(outcomes, outcome)
				continue
			}
		}

		named := fileWords(point.Behaviour + " " + point.Quote)
		if len(named) == 0 {
			outcomes = append(outcomes, outcome)
			continue
		}
		if matched := firstWrittenFile(named, wrote); matched != "" {
			outcome.Why = "the run changed " + matched
		} else {
			outcome.State = PointNotAnswered
			if len(named) > 3 {
				named = named[:3]
			}
			outcome.Why = "nothing this run wrote is " + strings.Join(named, ", ")
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func exercisedPoint(behaviour string, exercises []store.ExercisedPoint) (store.ExercisedPoint, bool) {
	behaviour = strings.TrimSpace(behaviour)
	for _, exercised := range exercises {
		if strings.TrimSpace(exercised.Point) == behaviour {
			return exercised, true
		}
	}
	return store.ExercisedPoint{}, false
}

func firstWrittenFile(named, wrote []string) string {
	for _, name := range named {
		for _, path := range wrote {
			if strings.EqualFold(name, filepath.Base(path)) {
				return name
			}
		}
	}
	return ""
}

// fileWords reads filename-shaped words rather than maintaining a list of
// languages or build systems that would become silently stale.
func fileWords(text string) []string {
	var files []string
	for _, field := range strings.Fields(text) {
		word := strings.Trim(field, "`'\"()[],;:.")
		if colon := strings.LastIndexByte(word, ':'); colon >= 0 &&
			colon < len(word)-1 && asciiDigits(word[colon+1:]) {
			word = word[:colon]
		}
		if !fileWord(word) {
			continue
		}
		name := filepath.Base(word)
		duplicate := false
		for _, kept := range files {
			if strings.EqualFold(kept, name) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			files = append(files, name)
		}
	}
	return files
}

func fileWord(word string) bool {
	if strings.ContainsAny(word, " \t\r\n@*") {
		return false
	}
	dot := strings.LastIndexByte(word, '.')
	if dot < 0 || dot == len(word)-1 {
		return false
	}
	extension := word[dot+1:]
	if len(extension) < 2 || len(extension) > 4 || !asciiLower(extension) {
		return false
	}
	stem := word[:dot]
	if slash := strings.LastIndexByte(stem, '/'); slash >= 0 {
		stem = stem[slash+1:]
	}
	return len(stem) >= 2 && hasASCIILetter(stem)
}

func asciiDigits(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func asciiLower(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < 'a' || value[index] > 'z' {
			return false
		}
	}
	return true
}

func hasASCIILetter(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] >= 'a' && value[index] <= 'z' || value[index] >= 'A' && value[index] <= 'Z' {
			return true
		}
	}
	return false
}

// ChecklistAccount renders the bounded account a person reads. The complete
// outcomes remain available to machine callers even when this block has to say
// how many whole lines live only on the run's own record.
func ChecklistAccount(outcomes []PointOutcome) string {
	if len(outcomes) == 0 {
		return ""
	}
	lines := make([]string, 0, len(outcomes))
	for index, outcome := range outcomes {
		line := fmt.Sprintf("%d. %s — %s", index+1,
			clipUTF8Bytes(strings.TrimSpace(outcome.Behaviour), behaviourWords), outcome.State)
		if why := strings.TrimSpace(outcome.Why); why != "" {
			line += ": " + why
		}
		lines = append(lines, line)
	}
	whole := ChecklistHeading + "\n" + strings.Join(lines, "\n")
	if len(whole) <= checklistBlockBytes {
		return whole
	}
	for kept := len(lines) - 1; kept >= 0; kept-- {
		omitted := fmt.Sprintf("…and %d more on this run's own record.", len(lines)-kept)
		parts := append([]string{ChecklistHeading}, lines[:kept]...)
		parts = append(parts, omitted)
		account := strings.Join(parts, "\n")
		if len(account) <= checklistBlockBytes {
			return account
		}
	}
	return ""
}
