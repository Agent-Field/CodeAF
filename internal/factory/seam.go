package factory

import "time"

// Seam is every door the surface has onto the factory, as a struct of funcs
// so a mock, a local engine and a hosted one are the same shape (the pattern
// tui3.TeamsSeam set). Every func may touch disk, the network and the clock:
// the surface calls them OFF THE LOOP and folds the result in as a message.
// Load is the one read; everything else is a verb that changes the floor and
// is followed by a Load.
//
// A nil func is a door that does not exist, and the surface draws no key for
// it: a capability that cannot work is absent, not broken.
type Seam struct {
	// Load copies the floor. It is the only read.
	Load func() (Snapshot, error)

	// Launch puts an item on a bench, or the queue when the benches are full.
	Launch func(id int) error
	// Stop ends a stream and keeps its branch; the item returns to new.
	Stop func(id int) error
	// Pause holds one bench without ending it; calling it again resumes.
	Pause func(id int) error
	// Dismiss hides an item until it changes.
	Dismiss func(id int) error

	// Answer resolves a waiting question: yes, no, or words.
	Answer func(id int, yes bool, words string) error
	// Steer hands words into a running stream. Words that name an effort, a
	// gate or a round count change the item; the rest is folded into the work.
	Steer func(id int, words string) error

	// SignOff ships a landed item. Edited says the person changed something
	// first; the engine counts clean sign-offs toward a habit, and the bool
	// says a habit is due to be offered.
	SignOff func(id int, edited bool) (habitDue bool, err error)
	// SendBack loops a landed item into its stream with words: "prove restart
	// survival". No new brief.
	SendBack func(id int, words string) error
	// Reverify runs every check on a landed item again.
	Reverify func(id int) error
	// Bank writes a sentence into the repo's habits.
	Bank func(repo, sentence string) error

	// New makes an item from words typed in the terminal, on a repo. Chips
	// lift out of the sentence. The id comes back so the surface can open it.
	New func(repo, words string) (id int, err error)
	// Sync opens a terminal-made item on the forge too, or stops syncing it.
	Sync func(id int, on bool) error
	// AskAuthor posts the triage questions to the item's author, as a draft
	// the person has already seen.
	AskAuthor func(id int) error

	// SetStage switches one of the item's stages on or off.
	SetStage func(id, index int, on bool) error
	// AddStage appends a stage from words: "after review, make it neater".
	AddStage func(id int, words string) error
	// BankStages writes the item's stages back onto its repo's recipe.
	BankStages func(id int) error
	// SetGate, SetCap and SetEffort are the item's chips.
	SetGate   func(id int, g Gate) error
	SetCap    func(id int, usd float64) error
	SetEffort func(id int, stage int, effort string) error

	// Tick advances a mock clock by d. A real engine leaves it nil and the
	// surface draws no speed, no sleep.
	Tick func(d time.Duration) error
	// Sleep jumps the clock by d and starts a new shift, so the handover
	// shows what happened while nobody looked. Mock only.
	Sleep func(d time.Duration) error
}

// Has says whether a door exists, by the door's name: the field's name in
// lower case (`launch`, `signoff`, `addstage`, `tick`). A name that is not a
// door answers false, so a misspelt door draws no key rather than a key that
// calls nil.
func (s Seam) Has(door string) bool {
	switch door {
	case "load":
		return s.Load != nil
	case "launch":
		return s.Launch != nil
	case "stop":
		return s.Stop != nil
	case "pause":
		return s.Pause != nil
	case "dismiss":
		return s.Dismiss != nil
	case "answer":
		return s.Answer != nil
	case "steer":
		return s.Steer != nil
	case "signoff":
		return s.SignOff != nil
	case "sendback":
		return s.SendBack != nil
	case "reverify":
		return s.Reverify != nil
	case "bank":
		return s.Bank != nil
	case "new":
		return s.New != nil
	case "sync":
		return s.Sync != nil
	case "askauthor":
		return s.AskAuthor != nil
	case "setstage":
		return s.SetStage != nil
	case "addstage":
		return s.AddStage != nil
	case "bankstages":
		return s.BankStages != nil
	case "setgate":
		return s.SetGate != nil
	case "setcap":
		return s.SetCap != nil
	case "seteffort":
		return s.SetEffort != nil
	case "tick":
		return s.Tick != nil
	case "sleep":
		return s.Sleep != nil
	}
	return false
}
