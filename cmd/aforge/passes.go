package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// passesFlag is how many independent passes `aforge plan new` plans with before
// merging them, TYPED AS WORDS RATHER THAN AS MAGIC INTEGERS.
//
// It was `--ensemble 0|-1|N`, where 0 meant "decide from the goal", -1 meant
// "never", and N ≥ 2 meant N passes. That is a code-shaped API wearing a flag's
// clothes: nothing about `0` says "auto" or about `-1` says "off" to the person
// typing them, `--ensemble 1` had no meaning at all, and the help string had to
// spend a whole sentence teaching the encoding. The values underneath have not
// moved — [plan.EnsembleAuto] and [plan.EnsembleNever] are still what the
// builder is handed — only the spelling a person types.
type passesFlag struct{ count int }

func (f *passesFlag) String() string {
	switch f.count {
	case plan.EnsembleAuto:
		return "auto"
	case plan.EnsembleNever:
		return "off"
	}
	return strconv.Itoa(f.count)
}

func (f *passesFlag) Set(text string) error {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "auto":
		f.count = plan.EnsembleAuto
		return nil
	case "off", "none", "never":
		f.count = plan.EnsembleNever
		return nil
	}
	count, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return fmt.Errorf("auto, off, or a number of passes from 2")
	}
	// ONE PASS IS OFF, AND SAYING SO IS BETTER THAN ACCEPTING IT. A merge of one
	// plan with itself is not a thing the builder does, and the old spelling
	// left `--ensemble 1` to fall through the encoding into whatever the
	// comparison happened to do with it.
	if count < 2 {
		return fmt.Errorf("auto, off, or a number of passes from 2 — one pass is `off`")
	}
	f.count = count
	return nil
}
