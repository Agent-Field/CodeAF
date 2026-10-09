package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestServedInsideTheDemandedSetIsNotAskedNotServed is #969's second acceptance:
// once a row carries the set the chooser demanded (`lanes`), served being a
// DIFFERENT member of that set is the router choosing inside the choice we made,
// not routing around it - so it is not counted as "asked for is not the machine
// that served". Only served landing OUTSIDE the set is.
func TestServedInsideTheDemandedSetIsNotAskedNotServed(t *testing.T) {
	log := writeLines(t,
		// Served tinder, demanded {cinnabar, tinder}: inside the set, agreement
		// even though served != lane.
		`{"ts":"2026-09-08T10:00:00.000Z","id":"1","model":"m/one","served":"tinder","lane":"cinnabar","lanes":["cinnabar","tinder"],"status":200,"ms":500}`,
		// Served tinder, ranked cinnabar, no set: the pre-#937 shape, a real
		// asked != served.
		`{"ts":"2026-09-08T10:00:01.000Z","id":"2","model":"m/one","served":"tinder","lane":"cinnabar","status":200,"ms":500}`,
		// Served ember, demanded {cinnabar, tinder}: outside the set, a real
		// asked != served.
		`{"ts":"2026-09-08T10:00:02.000Z","id":"3","model":"m/one","served":"ember","lane":"cinnabar","lanes":["cinnabar","tinder"],"status":200,"ms":500}`,
	)
	rows, err := readLog(log)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	report(&out, log, rows, defaults())
	got := out.String()

	// Three rows are attributed; the one served inside its set is agreement, the
	// two served outside their admitted machines are not.
	if !strings.Contains(got, "| the machine asked for is not the machine that served | 2 | 3 |") {
		t.Errorf("served-inside-the-set was counted as asked != served:\n%s", got)
	}
}
