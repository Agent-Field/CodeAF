package head

import "strings"

// People dictate. "um cancel the— no wait, keep it, just make it faster" is one
// utterance a person understands instantly and the deterministic arms read
// exactly backwards: the cue is not at the start where a prefix test looks for
// it, and when it is found the sentence has already taken it back.
//
// The answer is emphatically NOT disfluency parsing — house law bans it, and a
// grammar for spoken repair is the same road as a grammar for intent, with the
// same end. What is here is two much smaller things, and neither of them
// decides what a sentence means.
//
// The first is a floor: the words below carry no proposition at all, in any
// sentence, in any order. Skipping them is not interpretation, it is finding
// where the instruction actually begins — which is what HasPrefix was always
// asking about and was measuring from the wrong byte.
//
// The second is a withdrawal: when a sentence repairs itself after its own cue,
// the deterministic reading of its opening is stale by construction, and the
// only honest move is to stop claiming the sentence. The markers below are not
// consulted for meaning and never produce an action; they only say "this one
// needs to be read whole", and the belt behind this reads it whole. So the fast
// path stays exactly as fast for a clean command, and the messy long tail
// reaches model judgment instead of a prefix match.

// leadingFiller is the noise a spoken sentence starts with. Every word here is
// contentless on its own — remove it from any instruction and the instruction
// is unchanged — which is the entire test for belonging in this map.
var leadingFiller = map[string]bool{
	"um": true, "umm": true, "uh": true, "uhh": true, "er": true, "erm": true,
	"ah": true, "oh": true, "hmm": true, "hm": true, "mm": true,
	"ok": true, "okay": true, "so": true, "well": true, "yeah": true,
	"yep": true, "hey": true, "please": true, "just": true,
}

// selfRepairMarkers are the ways a person takes back what they have just said.
// They are read for position only: one appearing after a cue means the sentence
// contradicts its own opening, and a deterministic arm may not act on an
// opening the rest of the sentence has withdrawn.
var selfRepairMarkers = []string{
	"no wait", "wait no", "no, wait", "wait, no",
	"actually no", "actually, no", "no actually", "no, actually",
	"scratch that", "never mind", "nevermind", "belay that",
	"on second thought", "on second thoughts",
	"forget i said", "ignore that", "strike that",
	"i mean", "i meant",
}

// instructionOpening returns the lowered instruction with its leading filler
// removed, so a prefix test measures from where the instruction begins rather
// than from where the recording did.
func instructionOpening(lower string) string {
	for {
		trimmed := strings.TrimLeft(lower, " \t\n,.;:!?-—–…\"'")
		word := leadingWord(trimmed)
		if word == "" || !leadingFiller[word] {
			return trimmed
		}
		lower = trimmed[len(word):]
	}
}

// leadingWord is the first run of letters, which is all the filler test needs.
func leadingWord(value string) string {
	for index, character := range value {
		if character >= 'a' && character <= 'z' {
			continue
		}
		return value[:index]
	}
	return value
}

// selfRepairsAfterCue reports that the instruction takes itself back somewhere
// after its own opening. A marker at position zero IS the opening — "actually,
// change the plan" is a correction, not a repair of one — so only a marker with
// words in front of it counts.
func selfRepairsAfterCue(opening string) bool {
	for _, marker := range selfRepairMarkers {
		if index := strings.Index(opening, marker); index > 0 {
			return true
		}
	}
	return false
}
