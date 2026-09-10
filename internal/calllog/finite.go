package calllog

// NUMBERS JSON CANNOT SPELL, AND WHY THEY MAY NOT COST A ROW.
//
// encoding/json has no spelling for +Inf, -Inf or NaN, and it refuses the whole
// record when one reaches it. The record it refuses is almost always an END
// row, so what is left is a start row with nothing beside it — which every
// reader of this file, a person and the manual alike, reads as a call that is
// still in flight.
//
// SO THE LOG NEVER LOSES A ROW OVER A VALUE. A number JSON cannot spell is
// taken off the record and the rest of the row is written whole.
//
// THIS IS NOW THE LAST LINE OF DEFENCE AND NOT THE ORDINARY ROAD, and that is
// the change. The wait controller used to price acting at +Inf whenever it held
// no alternative lane, and its belief answered +Inf for a wait far enough into
// the tail; both figures travelled through the watch into `cost_s` and `wait_s`
// and landed here on ordinary, healthy, one-second calls. They are
// [control.Seconds] now — a figure, or nothing, or past what a belief can price
// — and only a figure is ever written, so nothing in the request path produces
// a value for this file to rescue. What reaches it is a builder defect.
//
// AND IT SAYS NOTHING ON THE ROW ABOUT IT. It used to add a sentence naming the
// field and the float, which was the right repair while an infinity was a
// legitimate answer and is the wrong one now: under the emptiness law a row
// with no number carries NOTHING, and a row explaining a float to somebody who
// asked what happened to a model call is a program talking about its own
// arithmetic. The complaint goes to stderr, once, where a defect belongs.
//
// TAKEN OFF RATHER THAN SPELLED AS A STRING OR A NULL, because the field's type
// is what every reader of this log decodes with, today and for every row
// already written: a `cost_s` that is sometimes a number and sometimes "+Inf"
// breaks the decode of a whole file for the sake of one line of it.

import (
	"encoding/json"
	"fmt"
	"math"
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

// dropNonFinite takes every number JSON cannot spell off the record, reporting
// whether it changed anything. A record with no such number is left exactly as
// it arrived, so the caller can tell this repair from a failure it cannot help.
//
// The row keeps its silence and the DEFECT is complained about once, on stderr:
// nothing in the request path can produce one of these any more, so a value
// arriving here is a bug in whoever built the record and not a fact about the
// call.
func dropNonFinite(record *Record) bool {
	dropped := false
	for _, field := range measured(record) {
		value := *field.at
		if !math.IsInf(value, 0) && !math.IsNaN(value) {
			continue
		}
		*field.at = 0
		dropped = true
		reportUnspellableField(field.name, value)
	}
	return dropped
}

// spellNonFinite is how the complaint names a number JSON refused. The three
// words are the ones Go's own formatting uses, because the reader is reading a
// program's account of itself.
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

// unspellableField carries the complaint about a figure no shape of this
// package could turn into a line. Once per process, for [unspellable]'s reason:
// a builder bug repeats on every call, and a line of stderr per model call
// would be worse than the figure it is about.
var unspellableField sync.Once

func reportUnspellableField(name string, value float64) {
	unspellableField.Do(func() {
		fmt.Fprintf(stderr, "aforge: a model-call record carried %s as %s, which JSON cannot write; the figure is off the row and the row is kept\n",
			name, spellNonFinite(value))
	})
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
