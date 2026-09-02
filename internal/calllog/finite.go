package calllog

// NUMBERS JSON CANNOT SPELL, AND WHY THEY MAY NOT COST A ROW.
//
// encoding/json has no spelling for +Inf, -Inf or NaN, and it refuses the whole
// record when one reaches it. The record it refuses is almost always an END
// row: the wait controller prices acting at +Inf whenever it holds no
// alternative lane, that figure travels through the watch into CostS, and the
// row that would have said how a long call finished never lands. What is left
// is a start row with nothing beside it — which every reader of this file, a
// person and the manual alike, reads as a call that is still in flight.
//
// SO THE LOG NEVER LOSES A ROW OVER A VALUE. A number JSON cannot spell is
// taken off the record, the rest of the row is written whole, and a sentence on
// the row says which number it was and what it said.
//
// TAKEN OFF RATHER THAN SPELLED AS A STRING OR A NULL, because the field's type
// is what every reader of this log decodes with, today and for every row
// already written: a `cost_s` that is sometimes a number and sometimes "+Inf"
// breaks the decode of a whole file for the sake of one line of it. A missing
// field already has a meaning here — the emptiness law, nothing was measured —
// and the note is what tells "infinite" apart from "unknown", in words, on the
// same row.

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
)

// marshalJSON is seamed for the reason stderr is: the branch below that gives
// up on a record cannot be reached with any Record, since every field of one is
// a kind JSON can always spell once the non-finite numbers are off it. A law a
// log may not break should not rest on unexercised code.
var marshalJSON = json.Marshal

// marshalRecord turns one record into its line, rescuing the one failure that
// is a value rather than a broken disk. The record arrives by value, so the
// repair below is this line's own and never the caller's.
func marshalRecord(record Record) ([]byte, error) {
	line, err := marshalJSON(record)
	if err == nil {
		return line, nil
	}
	if !dropNonFinite(&record) {
		return nil, err
	}
	return marshalJSON(record)
}

// floatField is one of a record's measured numbers, under the name a person
// greps the file for rather than the name the struct gives it.
type floatField struct {
	name string
	at   *float64
}

// measured is every float a record carries. It is a table rather than
// reflection because it is read only on the rare row JSON has already refused,
// and a table is the version somebody can check against the struct by eye.
func measured(record *Record) [4]floatField {
	return [4]floatField{
		{"waste_usd", &record.WasteUSD},
		{"wait_s", &record.WaitS},
		{"cost_s", &record.CostS},
		{"cost", &record.Cost},
	}
}

// dropNonFinite takes every number JSON cannot spell off the record and says on
// the row which ones they were and what they said, reporting whether it changed
// anything. A record with no such number is left exactly as it arrived, so the
// caller can tell this repair from a failure it cannot help.
func dropNonFinite(record *Record) bool {
	var said []string
	for _, field := range measured(record) {
		value := *field.at
		if !math.IsInf(value, 0) && !math.IsNaN(value) {
			continue
		}
		*field.at = 0
		said = append(said, fmt.Sprintf("%s was %s and is not on this row.", field.name, spellNonFinite(value)))
	}
	if len(said) == 0 {
		return false
	}
	record.Note = withSentences(record.Note, said)
	return true
}

// spellNonFinite is how the note names a number JSON refused. The three words
// are the ones Go's own formatting uses, because the reader who greps for them
// is reading a program's account of itself.
func spellNonFinite(value float64) string {
	switch {
	case math.IsInf(value, 1):
		return "+Inf"
	case math.IsInf(value, -1):
		return "-Inf"
	default:
		return "NaN"
	}
}

// withSentences adds what this repair has to say to whatever the call had
// already noted. The controller's own sentence comes first and is never
// replaced: it is an account of a decision, and this is only an account of a
// number.
func withSentences(note string, sentences []string) string {
	added := strings.Join(sentences, " ")
	if note = strings.TrimSpace(note); note == "" {
		return added
	}
	return note + " " + added
}

// unspellable carries the one complaint about a record no shape of this package
// could turn into a line. Once per process, because a builder bug repeats on
// every call and a line of stderr per model call would be worse than the row it
// is about; and it deliberately does NOT silence the log, because the rows that
// follow belong to other calls and are almost certainly fine.
var unspellable sync.Once

func reportUnspellable(err error) {
	unspellable.Do(func() {
		fmt.Fprintf(stderr, "aforge: a model-call record could not be written to the log (%v); the log stays on for the calls that follow\n", err)
	})
}
