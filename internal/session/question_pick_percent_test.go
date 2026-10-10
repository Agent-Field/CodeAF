package session

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A PICK CARRIES THE NUMBER DECISIONS DRAWS, AND THE LINES IT WAS WEIGHED
// AGAINST. Confidence stays the three words an asker is offered. Percent is
// the measurement (0 means there was none). Basis is the knows-line ids and
// receipt ids behind that number. An older build, which never heard of either,
// still reads the card.

// olderPick is the pick a build from before percent and basis knew. It is the
// reader the new bytes have to survive: encoding/json drops keys that reader
// has no field for, and must not fail the whole card.
type olderPick struct {
	Key         string `json:"key"`
	Reason      string `json:"reason,omitempty"`
	Confidence  string `json:"confidence,omitempty"`
	WouldChange string `json:"wouldChange,omitempty"`
}

type olderQuestion struct {
	ID   uint64     `json:"id"`
	Head string     `json:"head"`
	Pick *olderPick `json:"pick,omitempty"`
}

type olderPresence struct {
	Kind QuestionKind   `json:"kind"`
	ID   uint64         `json:"id"`
	Text string         `json:"text,omitempty"`
	Full *olderQuestion `json:"full,omitempty"`
}

func TestAPicksPercentAndBasisRoundTripThroughThePresenceQuestion(t *testing.T) {
	for _, percent := range []int{92, 97} {
		t.Run(strconv.Itoa(percent), func(t *testing.T) {
			question := wellFormed()
			question.Pick = &Pick{
				Key:         "1",
				Reason:      "matches what Marketing knows",
				Confidence:  ConfidenceSure,
				Percent:     percent,
				Basis:       []string{"knows-marketing-pricing", "receipt-44"},
				WouldChange: "if the seat price moved",
			}
			card := PresenceQuestion{
				Kind:    question.Kind,
				ID:      question.ID,
				Text:    question.Head,
				Options: question.Options,
				Full:    &question,
			}
			raw, err := json.Marshal(card)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			text := string(raw)
			if !strings.Contains(text, `"percent":`+strconv.Itoa(percent)) {
				t.Fatalf("the presence card dropped the percent:\n%s", text)
			}
			if !strings.Contains(text, `"basis":["knows-marketing-pricing","receipt-44"]`) {
				t.Fatalf("the presence card dropped the basis:\n%s", text)
			}

			var back PresenceQuestion
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back.Full == nil || back.Full.Pick == nil {
				t.Fatal("the card came back without its question")
			}
			pick := back.Full.Pick
			if pick.Key != "1" || pick.Confidence != ConfidenceSure || pick.Percent != percent || pick.WouldChange != "if the seat price moved" {
				t.Fatalf("the pick came back as %+v", pick)
			}
			if !slices.Equal(pick.Basis, []string{"knows-marketing-pricing", "receipt-44"}) {
				t.Fatalf("basis came back as %v", pick.Basis)
			}
			if pick.ResolvedPercent() != percent {
				t.Fatalf("a measured %d resolved to %d", percent, pick.ResolvedPercent())
			}

			// AN OLDER BUILD IGNORES THE KEYS IT DOES NOT KNOW, and still reads
			// the pick it always could. Rewriting the card must not invent the
			// new keys back, because that build has nowhere to put them.
			var older olderPresence
			if err := json.Unmarshal(raw, &older); err != nil {
				t.Fatalf("an older reader refused the card: %v", err)
			}
			if older.Full == nil || older.Full.Pick == nil || older.Full.Pick.Key != "1" || older.Full.Pick.Confidence != "sure" {
				t.Fatalf("an older reader lost the pick: %+v", older.Full)
			}
			rewritten, err := json.Marshal(older)
			if err != nil {
				t.Fatalf("older marshal: %v", err)
			}
			if strings.Contains(string(rewritten), "percent") || strings.Contains(string(rewritten), "basis") {
				t.Fatalf("an older reader wrote keys it does not know: %s", rewritten)
			}
		})
	}

	// A FILE FROM BEFORE THESE FIELDS still decodes, and the absent number is
	// unknown rather than a zero the asker measured on purpose beyond the word.
	old := []byte(`{"kind":"consent","id":1,"text":"may it overwrite build/notes.md?","full":{"id":1,"kind":"consent","ask":"permission","head":"may it overwrite build/notes.md?","reason":"bash always asks","stakes":"costly","options":[{"key":"1","label":"allow once"}],"pick":{"key":"1","confidence":"fairly","reason":"why"}}}`)
	var fromOld PresenceQuestion
	if err := json.Unmarshal(old, &fromOld); err != nil {
		t.Fatalf("an older card did not decode: %v", err)
	}
	if fromOld.Full == nil || fromOld.Full.Pick == nil {
		t.Fatal("an older card came back without its pick")
	}
	if fromOld.Full.Pick.Percent != 0 || fromOld.Full.Pick.Basis != nil {
		t.Fatalf("an absent percent or basis was invented: %+v", fromOld.Full.Pick)
	}
	if fromOld.Full.Pick.ResolvedPercent() != percentForFairly {
		t.Fatalf("a fairly pick with no number resolved to %d, want %d", fromOld.Full.Pick.ResolvedPercent(), percentForFairly)
	}

	// A WORD-ONLY PICK WRITES NEITHER NEW KEY, so a file this build writes for
	// a pick that measured nothing still looks like the file an older build wrote.
	wordRaw, err := json.Marshal(Pick{Key: "1", Confidence: ConfidenceUnsure})
	if err != nil {
		t.Fatalf("marshal word-only: %v", err)
	}
	if strings.Contains(string(wordRaw), "percent") || strings.Contains(string(wordRaw), "basis") {
		t.Fatalf("a word-only pick wrote a number or a basis: %s", wordRaw)
	}

	// THE OBJECT FORM KEEPS BOTH, and a bare key — the shape a model actually
	// sends — stays a key with nothing measured.
	var object Pick
	if err := json.Unmarshal([]byte(`{"key":"1","percent":97,"basis":["receipt-7"],"confidence":"sure"}`), &object); err != nil {
		t.Fatalf("object form: %v", err)
	}
	if object.Percent != 97 || object.Confidence != ConfidenceSure || !slices.Equal(object.Basis, []string{"receipt-7"}) {
		t.Fatalf("object form dropped the measurement: %+v", object)
	}
	var bare Pick
	if err := json.Unmarshal([]byte(`"1"`), &bare); err != nil {
		t.Fatalf("bare key: %v", err)
	}
	if bare.Key != "1" || bare.Percent != 0 || bare.Basis != nil || bare.ResolvedPercent() != 0 {
		t.Fatalf("a bare key grew a measurement: %+v resolved %d", bare, bare.ResolvedPercent())
	}
}

func TestTheGateRefusesAPercentOutsideZeroToOneHundred(t *testing.T) {
	for _, percent := range []int{0, 1, 92, 97, 100} {
		question := wellFormed()
		question.Pick = &Pick{Key: "1", Percent: percent, Basis: []string{"receipt-1"}}
		if err := question.Check(nil); err != nil {
			t.Fatalf("percent %d was refused: %v", percent, err)
		}
	}
	for _, percent := range []int{-1, 101} {
		question := wellFormed()
		question.Pick = &Pick{Key: "1", Percent: percent}
		err := question.Check(nil)
		if err == nil {
			t.Fatalf("percent %d was allowed", percent)
		}
		if !strings.Contains(err.Error(), strconv.Itoa(percent)) || !strings.Contains(err.Error(), "0 to 100") {
			t.Fatalf("the refusal for %d reads %q", percent, err)
		}
	}

	// THE GATE DOES NOT FILL THE NUMBER IN. A word-only pick stays at zero;
	// the band is what a reader computes, not a sentence the gate writes.
	question := wellFormed()
	question.Pick = &Pick{Key: "1", Confidence: ConfidenceSure}
	if err := question.Check(nil); err != nil {
		t.Fatalf("a word-only pick was refused: %v", err)
	}
	if question.Pick.Percent != 0 {
		t.Fatalf("the gate wrote a percent onto a word-only pick: %d", question.Pick.Percent)
	}
}

func TestAWordOnlyPickMapsOntoItsBand(t *testing.T) {
	for _, tc := range []struct {
		name string
		word Confidence
		want int
		low  int
		high int
	}{
		{"sure", ConfidenceSure, percentForSure, 90, 100},
		{"fairly", ConfidenceFairly, percentForFairly, 60, 89},
		{"unsure", ConfidenceUnsure, percentForUnsure, 1, 59},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (Pick{Confidence: tc.word}).ResolvedPercent()
			if got != tc.want {
				t.Fatalf("%s resolved to %d, want %d", tc.name, got, tc.want)
			}
			if got < tc.low || got > tc.high {
				t.Fatalf("%s resolved to %d, outside %d-%d", tc.name, got, tc.low, tc.high)
			}
		})
	}

	// A MEASURED NUMBER WINS, including when the word disagrees, and including
	// the two numbers Decisions draws.
	if got := (Pick{Percent: 92, Confidence: ConfidenceUnsure}).ResolvedPercent(); got != 92 {
		t.Fatalf("92 with a disagreeing word resolved to %d", got)
	}
	if got := (Pick{Percent: 97}).ResolvedPercent(); got != 97 {
		t.Fatalf("97 with no word resolved to %d", got)
	}
	if got := (Pick{}).ResolvedPercent(); got != 0 {
		t.Fatalf("a pick with neither resolved to %d, want unknown", got)
	}
	if got := (Pick{Confidence: Confidence("high")}).ResolvedPercent(); got != 0 {
		t.Fatalf("a word this type does not read resolved to %d", got)
	}
}
