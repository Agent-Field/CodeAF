package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/config"
)

// `/budget` — THE KEYBOARD DOOR ONTO THE LIMITS.
//
// It is a door and not a second editor (docs/design/spending/DESIGN.md): bare,
// it opens the Spending tab with the cursor on the row a person almost always
// means; with an amount, it writes THROUGH THE SAME REGISTRY ROW the tab writes
// through, so what /budget lands and what the panel lands are byte-identical.
//
// IT IS THE DOOR THAT WORKS FROM WHERE THE PERSON IS STANDING. Every letter on
// this surface belongs to a text box — the composer, a place's own box, the
// panel's search — which is the product and not a compromise (verbstrip.go's
// first law), so the door that a refused turn can name has to be one a person
// can type into the box they are already looking at. That is this.
//
// THE ROWS IT NAMES ARE THE ROWS THAT EXIST. `day`, `conversation`, `plan` and
// `practice` are the four rails a person can turn; a task and a standing firing
// are named on the tab as READINGS because this build enforces them somewhere a
// settings row cannot reach (settingspend.go), and a command that accepted
// `task 20` would be writing a number nothing reads.

// budgetRows is the word a person types against the registry row it names, and
// the words are the tab's own labels rather than the keys behind them.
var budgetRows = []struct {
	words []string
	key   string
}{
	{[]string{"day", "daily", "today"}, config.KeyDailyBudget},
	{[]string{"conversation", "chat", "session"}, config.KeySpendRail},
	{[]string{"plan", "plans", "ask"}, config.KeyPlanConsent},
	{[]string{"practice"}, config.KeyPracticeBudget},
}

// budgetRowFor reads the first word of an argument as a row name.
func budgetRowFor(word string) (string, bool) {
	word = strings.ToLower(strings.TrimSpace(word))
	for _, row := range budgetRows {
		for _, spelling := range row.words {
			if spelling == word {
				return row.key, true
			}
		}
	}
	return "", false
}

// budgetWords is what a refusal offers back: the four rows, spelled the way the
// command takes them. It is built from the table rather than typed out, so a
// fifth rail cannot be added without the refusal learning about it.
func budgetWords() string {
	said := make([]string, 0, len(budgetRows))
	for _, row := range budgetRows {
		said = append(said, row.words[0])
	}
	return strings.Join(said, ", ")
}

// budget is /budget. The shapes are the design's own table:
//
//	/budget            the tab, on `per day`
//	/budget 50         the day's limit
//	/budget none       …removed
//	/budget plan 20    one row by name
//	/budget plan       the tab, on that row
func (a *app) budget(rest string) tea.Cmd {
	a.noticeEvent(eventBudgetShown)
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return a.openSpending(config.KeyDailyBudget)
	}
	key, amount := config.KeyDailyBudget, rest
	if word, tail, _ := strings.Cut(rest, " "); true {
		if named, ok := budgetRowFor(word); ok {
			key, amount = named, strings.TrimSpace(tail)
		}
	}
	// A ROW NAMED WITH NO FIGURE IS A QUESTION, and the answer to a question is
	// the row itself: `/budget plan` opens the tab on `per plan` rather than
	// refusing for want of a number nobody said they had.
	if amount == "" {
		return a.openSpending(key)
	}
	row, ok := a.registry().Row(key)
	if !ok {
		a.note("that limit is not on this machine")
		return nil
	}
	if err := row.Apply(amount); err != nil {
		// THE REFUSAL IS THE ROW'S OWN WORDS and never a second sentence about
		// the same rule (internal/config's writers refuse in plain language),
		// with the rows this command knows named after it so a mistyped row name
		// is answered rather than parsed as a mistyped amount.
		a.note(err.Error() + " · rows: " + budgetWords())
		return nil
	}
	a.refreshSettings()
	// AND IT SAYS WHAT IT LANDED, in the words the tab uses for that row, because
	// a command that writes silently is a command a person runs twice.
	a.note(budgetWord(a.registry(), key))
	return nil
}

// budgetWord is one row as a receipt: its label on the Spending tab, and what it
// now reads. Both are read back through the registry rather than composed from
// what was typed, so `/budget 0`, `/budget none` and `/budget ∞` all answer with
// the same `no limit` the row itself shows.
func budgetWord(registry *config.Settings, key string) string {
	label := key
	if meta, ok := settingUI[key]; ok && meta.label != "" {
		label = meta.label
	}
	row, ok := registry.Row(key)
	if !ok {
		return label
	}
	value := row.Value()
	if figure := spendValue(row); figure.full != "" {
		value = figure.full
	}
	return label + " · " + value
}
