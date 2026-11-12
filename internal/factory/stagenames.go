package factory

import (
	"strings"
	"unicode"
)

// ── A STAGE IS ONE WORD, ON EVERY ITEM, OLD ONES TOO ───────────────────────
//
// Every door that names a stage takes one word or refuses ([StageWord]): the
// typed sentence, the manager's edit, the recipe file. An item written before
// that law can still carry a name of several words; the 2026-10-09 run of a
// pull request carried `do through`, added from the person's `do a through
// review from security and code and architecture perspective`, and the
// stages line, the step's chat name and its brief all said two words where
// every other step said one. So an item is read with every stage name made
// one word ([NormalizeStageNames]), and the next write keeps it.

// nameFiller are words a name of several words leaves out besides the ones
// an ask does ([stageLead], [stageStop]): the verb that only says to do it.
var nameFiller = map[string]bool{"do": true}

// OneStageWord is name as a stage's one word whatever it was written as: the
// name itself when [StageWord] takes it; else the first word of it that can
// be a name, fillers left out (`do through` is `through`, `deep review` is
// `deep`); else the first of its ask's ([stageName]). A word already in taken
// gives way to the next, so two stages never share a name; when every word
// is taken it answers the first candidate anyway.
func OneStageWord(name, ask string, taken map[string]bool) string {
	if w, err := StageWord(name); err == nil {
		return w
	}
	var cands []string
	for _, f := range strings.Fields(strings.ToLower(name)) {
		f = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return r
			}
			return -1
		}, f)
		if f == "" || nameFiller[f] || stageLead[f] || stageStop[f] {
			continue
		}
		if r := []rune(f); len(r) > stageWordMost {
			f = string(r[:stageWordMost])
		}
		if w, err := StageWord(f); err == nil {
			cands = append(cands, w)
		}
	}
	if strings.TrimSpace(ask) != "" {
		cands = append(cands, stageName(ask))
	}
	cands = append(cands, "stage")
	for _, c := range cands {
		if !taken[c] {
			return c
		}
	}
	return cands[0]
}

// NormalizeStageNames makes every stage name on it one word
// ([OneStageWord]), and renames its run's phase of the same name with it so
// the run still finds its place. An item whose names are all one word comes
// back as it was. A stage with no name stays nameless: its ask names it.
func NormalizeStageNames(it *Item) {
	taken := map[string]bool{}
	for _, s := range it.Stages {
		if w, err := StageWord(s.Name); err == nil {
			taken[w] = true
		}
	}
	for i, s := range it.Stages {
		if strings.TrimSpace(s.Name) == "" {
			continue
		}
		if _, err := StageWord(s.Name); err == nil {
			continue
		}
		old := s.Name
		w := OneStageWord(old, s.Ask, taken)
		taken[w] = true
		it.Stages[i].Name = w
		if it.Stream != nil {
			for j := range it.Stream.Phases {
				if it.Stream.Phases[j].Name == old {
					it.Stream.Phases[j].Name = w
				}
			}
		}
	}
	// A phase with no stage of its own (a sent-back `prove 2` from before
	// it was `prove2`) is made one word the same way.
	if it.Stream != nil {
		for j, ph := range it.Stream.Phases {
			if strings.TrimSpace(ph.Name) == "" {
				continue
			}
			if _, err := StageWord(ph.Name); err == nil {
				taken[strings.ToLower(ph.Name)] = true
				continue
			}
			w := OneStageWord(strings.ReplaceAll(ph.Name, " ", ""), ph.Note, taken)
			taken[w] = true
			it.Stream.Phases[j].Name = w
		}
	}
}
