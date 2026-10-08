package factory

import "context"

// Seam is every door the surface has onto the factory, as a struct of funcs
// so a fixture, a local engine and a hosted one are the same shape (the pattern
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
	// BankNote is Bank answering what happened to the line: one of the
	// Recipe* note sentences (recipechange.go), a pull request for the team
	// where the checkout is a git repository. Nil where Bank is nil.
	BankNote func(repo, sentence string) (string, error)

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
	// Edit changes the item's stages by the manager's (or anybody's) edit,
	// within the bounds [Edit] holds: before a run when nothing of the item has
	// started, and only the stages not yet started during one. It answers the
	// item as saved and the lines the edit recorded ([Item.Adapted]); a
	// refused edit changes nothing and answers why. A running item's runner
	// reads the new stages at its next round.
	Edit func(ctx context.Context, id int, e RunEdit) (Item, []string, error)

	// THE FLOOR'S OWN SETTINGS (settings.go), each a door like every other:
	// nil is a key the floor does not draw.
	//
	// Repos is the repo picker's read: the repositories the floor watches,
	// `owner/name`, and every repository the connected forge account can see,
	// most recently pushed first, each with its open count and, when this
	// machine knows one, its checkout. available is nil when nothing lists
	// them.
	Repos func(ctx context.Context) (watched []string, available []RepoInfo, err error)
	// SetRepos replaces the watched repositories.
	SetRepos func(repos []string) error
	// GitHub says who this machine reaches GitHub as, and by which way; a
	// zero link is no token at all.
	GitHub func(ctx context.Context) (GitHubLink, error)
	// GHLogin is the login `gh auth status` answers, "" when gh is absent or
	// not logged in. It is asked only to offer gh; nothing is kept by asking.
	GHLogin func(ctx context.Context) (string, error)
	// ConnectGitHub keeps a way to reach GitHub: "" is consent to use gh's own
	// login, and anything else is a token kept in the profile.
	ConnectGitHub func(ctx context.Context, token string) error
	// RecipeAt is a repository's recipe as its file says it, the lines that
	// did not load, and the folder it was read from ("" when this machine does
	// not know where the repository is checked out, and the recipe is then the
	// default one).
	RecipeAt func(repo string) (r Recipe, problems []Problem, dir string, err error)
	// SaveRecipe writes a whole recipe to the repository's recipe file.
	SaveRecipe func(repo string, r Recipe) error
	// SetRail sets the day's rail, the most the floor may spend in a day; 0
	// takes it off.
	SetRail func(usd float64) error

	// Talk is the item's own conversation: the one already made, or one made
	// now — inside the item's own team, with the item in front of it — and
	// kept on the item ([Item.Talk]), so a second ask answers the same one.
	// chat is the conversation's session file, which is how the surface opens
	// a conversation. NOTHING IS MADE UNTIL A PERSON ASKS: no item has one by
	// default.
	Talk func(ctx context.Context, id int) (chat string, err error)

	// Mark sets or takes off the floor's own marks on items by id, kept by
	// the store, so a mark made outside this window (the foreman's, through
	// `factory_floor`) is on every window's next Load as [Snapshot.Marked].
	// Only a new item takes a mark; anything else is refused.
	Mark func(ids []int, on bool) error
	// Foreman is the floor's own conversation, the foreman's: the one already
	// made, or one made now, and the same one on every ask after. chat is its
	// session file. NOTHING IS MADE UNTIL A PERSON ASKS (`m` on the floor).
	Foreman func(ctx context.Context) (chat string, err error)

	// Refresh reads one item again from its source now (for a GitHub item: the
	// issue or pull request by its number, its last comments, and a pull
	// request's files and checks), folds what it read into the floor, and
	// takes the item's read off so the triage reads it again. An item that
	// came from the terminal or a chat has no source to read, so only its
	// read is taken off.
	Refresh func(ctx context.Context, id int) error
	// RefreshAll is Refresh for every item on the floor except the dismissed.
	// With a ctx marked by [DryRun] it does NOTHING and answers how many items
	// it would read again and what their triage would cost in dollars (the
	// count times the last average read, or [DefaultReadCost] each when no
	// read was priced yet), so the floor can say the cost before `U` spends
	// it. Unmarked, it does the whole of it and answers the same two figures.
	RefreshAll func(ctx context.Context) (count int, est float64, err error)
	// Open is the item's page on its source, for a person's browser, or the
	// error `this item is not on github` for work that was never there.
	Open func(id int) (url string, err error)

	// Clone checks a repository out on this machine, `owner/name` (or the
	// short name the floor shows, when only one watched repository has it),
	// into the factory's own folder, and records the folder as the
	// repository's checkout, so the next Load's [Snapshot.Checkouts] and every
	// run read it. dir is the folder. It may take a minute; the surface asks
	// it off the loop, and only when a person said yes to it. A REPOSITORY
	// THIS MACHINE ALREADY KNOWS A FOLDER FOR IS NEVER CLONED AGAIN: the door
	// answers that folder.
	Clone func(ctx context.Context, repo string) (dir string, err error)
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
	case "edit":
		return s.Edit != nil
	case "repos":
		return s.Repos != nil
	case "setrepos":
		return s.SetRepos != nil
	case "github":
		return s.GitHub != nil
	case "ghlogin":
		return s.GHLogin != nil
	case "connectgithub":
		return s.ConnectGitHub != nil
	case "recipeat":
		return s.RecipeAt != nil
	case "saverecipe":
		return s.SaveRecipe != nil
	case "setrail":
		return s.SetRail != nil
	case "talk":
		return s.Talk != nil
	case "mark":
		return s.Mark != nil
	case "foreman":
		return s.Foreman != nil
	case "refresh":
		return s.Refresh != nil
	case "refreshall":
		return s.RefreshAll != nil
	case "open":
		return s.Open != nil
	case "clone":
		return s.Clone != nil
	}
	return false
}
