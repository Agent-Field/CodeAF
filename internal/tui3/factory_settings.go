package tui3

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE FLOOR'S OWN SETTINGS ────────────────────────────────────────────────
//
// THERE IS NO FACTORY SETTINGS PAGE. Each setting lives where a person looks
// when they need it, and this file is the three that live on the floor:
//
//	R   which repositories the floor watches: a list over the floor, the
//	    repositories GitHub shows the connected account, space to watch one
//	E   the recipe page: the item page with no item, one tab per kind of
//	    work, the stages a repository runs and `b` to save them to its file
//	$   the day's rail: one typing row, read back by the handover's money
//
// and the connect prompt `R` asks first when no GitHub token resolves, which
// the settings place's `github` row runs too (settings_github.go).
//
// EVERY ONE IS A SEAM DOOR, asked OFF THE LOOP like every verb on the floor
// (factory_keys.go), and A NIL DOOR IS A KEY THAT IS NOT DRAWN: a floor whose
// store keeps no repositories offers no `R`, one that keeps no rail offers no
// `$`, and a recipe whose checkout is unknown is drawn but not edited.

// factoryPollWords is how often github is read, and the figure is the poll's
// own (cmd/codeaf's factoryPollEvery, and factory.md's `## connecting
// github`). A read floor with nothing open says it ([factoryBareReadWords]);
// the picker's receipt does not, because a person who just saved wants to
// hear that the read has begun, not when the next one is.
const factoryPollWords = "github polls every minute"

// factoryReadingNowWords is the picker's receipt's second clause: the save
// starts the first read, and the handover and the bare floor say where it is.
const factoryReadingNowWords = "reading them now"

// factoryForgeWait is how long the picker's read may take: a token's whole
// list of repositories is several pages on a busy account.
const factoryForgeWait = 20 * time.Second

// factoryGHWait is how long `gh auth status` may take before the floor stops
// waiting for it and offers the token row instead.
const factoryGHWait = 3 * time.Second

// factoryRecipeKinds are the recipe page's tabs, in the order they are drawn.
var factoryRecipeKinds = []factory.Kind{factory.KindIssue, factory.KindPR, factory.KindCI}

// The new typing rows' kinds, after factory_keys.go's own.
const (
	factoryAskToken       factoryAskKind = iota + 100 // ConnectGitHub with a token
	factoryAskRail                                    // SetRail
	factoryAskRecipeStage                             // a stage added to the recipe page
	factoryAskKnobs                                   // a stage's knobs in words
)

// factoryPicker is `R`'s list, standing over the floor while it is open.
type factoryPicker struct {
	// login is who GitHub says the token is, for the heading; "" when the seam
	// has no GitHub door to ask.
	login string
	rows  []factoryPickRow
	// on is the watched set as the person has it now, by lower-cased name,
	// which is GitHub's own rule for a repository's name, and was is the set
	// the picker opened with: the WATCHING section. THE SECTIONS DO NOT MOVE
	// UNDER A TICK, so a repository ticked now stays in its owner's group
	// until the picker opens again and the cursor never jumps.
	on, was map[string]bool
	// byOpen orders every section by how many issues and pull requests are
	// open, the most first, instead of by the last push (`ctrl+o`).
	byOpen bool
	cursor int
	top    int
	shown  int
	// hits is the walk position each body row of the last draw holds, -1 for
	// a row that holds no repository, so the pointer lands on the row a
	// person saw ([app.factoryPickHover]).
	hits []int
	// query is the filter, typed straight into the list.
	query string
}

// factoryPickRow is one repository the picker lists.
type factoryPickRow struct {
	full    string
	owner   string
	private bool
	pushed  time.Time
	// open is how many issues and pull requests are open on it, -1 when the
	// forge did not say; dir is where it is checked out on this machine, ""
	// when that is not known.
	open int
	dir  string
}

// factoryPickSection is one block of the picker: WATCHING, or one owner's
// repositories, as indexes into the picker's rows in the order drawn.
type factoryPickSection struct {
	heading string
	rows    []int
}

// factoryRecipePage is `E`'s page, standing over the floor while it is open.
// recipe is the person's copy, changed in place by the page's keys and
// written only by `b`.
type factoryRecipePage struct {
	repo     string
	kind     int
	recipe   factory.Recipe
	problems []factory.Problem
	// dir is where the repository is checked out, "" when this machine does
	// not know, and read whether the seam has answered yet.
	dir  string
	read bool
}

// ── the keys ────────────────────────────────────────────────────────────────

// factorySettingsKey is `R`, `E` and `$` on the floor, and false for every
// other key or a door the seam does not have.
func (a *app) factorySettingsKey(k string) (tea.Cmd, bool) {
	if a.fp.open {
		return nil, false
	}
	switch k {
	case "R", "shift+r":
		if !a.factory.Has("repos") || !a.factory.Has("setrepos") {
			return nil, false
		}
		a.pageMsg = ""
		return a.factoryOpenRepos(), true
	case "E", "shift+e":
		if len(a.fp.snap.Repos) == 0 {
			return nil, false
		}
		return a.factoryOpenRecipe(), true
	case "$":
		if !a.factory.Has("setrail") {
			return nil, false
		}
		ask := factoryAsk{kind: factoryAskRail, label: "day rail ›", example: "“$60 · 0 takes it off”"}
		if r := a.fp.snap.Rail; r > 0 {
			ask.text = factoryRailWord(r)
		}
		a.factoryOpenAsk(ask)
		return nil, true
	}
	return nil, false
}

// factorySettingsOwns is the picker's, the gh offer's and the recipe page's
// claim on the whole keyboard while one stands, read before the floor's own
// keys so a letter on the recipe page never launches the item underneath it.
// THE TYPING ROW OUTRANKS ALL BUT THE PICKER: a row opened on the recipe page
// is answered by factory_keys.go's [app.factoryOwns]. The router keeps its
// walk between places and its alt chords, as every box does.
func (a *app) factorySettingsOwns(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.mapShowing || a.bar.on {
		return nil, false
	}
	if k := msg.String(); k == "tab" || k == "shift+tab" || msg.Key().Mod&tea.ModAlt != 0 {
		return nil, false
	}
	switch {
	case a.fp.pick != nil:
		return a.factoryPickKey(msg), true
	case a.fp.act.ask != nil:
		return nil, false
	case a.fp.ghOffer != "":
		return a.factoryOfferKey(msg), true
	case a.fp.recipe != nil:
		return a.factoryRecipeKey(msg), true
	}
	return nil, false
}

// ── R: the repo picker ──────────────────────────────────────────────────────

// factoryOpenRepos is `R`: who the token is, then the list. NO TOKEN IS THE
// CONNECT PROMPT and not an empty list, because an empty list would say this
// account can see nothing, which is not what is true.
//
// GITHUB AND gh ARE ASKED WHILE THE NOTE LINE SAYS SO, `⠋ asking gh…`, until
// the list or the prompt stands (factory_busy.go).
func (a *app) factoryOpenRepos() tea.Cmd {
	seam := a.factory
	a.fp.act.doing = "asking gh…"
	if seam.GitHub == nil || seam.ConnectGitHub == nil {
		a.fp.act.doing = "asking github…"
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), factoryForgeWait)
		defer cancel()
		var link factory.GitHubLink
		if seam.GitHub != nil && seam.ConnectGitHub != nil {
			var err error
			link, err = seam.GitHub(ctx)
			if err != nil || !link.Connected() {
				login := factoryGHLogin(ctx, seam)
				return func(bool) tea.Cmd {
					a.fp.act.doing = ""
					a.factoryConnectPrompt(login)
					return nil
				}
			}
		}
		watched, available, err := seam.Repos(ctx)
		return func(bool) tea.Cmd {
			a.fp.act.doing = ""
			if err != nil {
				a.factorySay("github did not list the repositories · " + strings.TrimSpace(err.Error()))
				return nil
			}
			a.factoryOpenPicker(link.Login, watched, available)
			return nil
		}
	})
}

// factoryGHLogin is who `gh auth status` says is logged in, asked with
// [factoryGHWait] and "" when gh is absent, logged out or slow.
func factoryGHLogin(ctx context.Context, seam factory.Seam) string {
	if seam.GHLogin == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, factoryGHWait)
	defer cancel()
	login, err := seam.GHLogin(ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(login)
}

// factoryOpenPicker stands the list over the floor. A WATCHED REPOSITORY
// GITHUB DID NOT LIST IS STILL A ROW, so a repository the token can no longer
// see is still one a person can stop watching.
func (a *app) factoryOpenPicker(login string, watched []string, available []factory.RepoInfo) {
	p := &factoryPicker{login: login, on: map[string]bool{}, was: map[string]bool{}}
	listed := map[string]bool{}
	for _, r := range available {
		listed[strings.ToLower(r.Full)] = true
	}
	for _, w := range watched {
		name := strings.ToLower(w)
		p.on[name], p.was[name] = true, true
		if !listed[name] {
			p.rows = append(p.rows, factoryPickRow{full: w, owner: factoryRepoOwner(w), open: -1})
		}
	}
	for _, r := range available {
		if strings.TrimSpace(r.Full) == "" {
			continue
		}
		owner := r.Owner
		if owner == "" {
			owner = factoryRepoOwner(r.Full)
		}
		p.rows = append(p.rows, factoryPickRow{full: r.Full, owner: owner, private: r.Private, pushed: r.Pushed, open: r.Open, dir: r.Dir})
	}
	a.fp.pick = p
	a.touch()
}

// factoryRepoOwner is a repository's first path segment: `agentfield` for
// `agentfield/codeaf`, and "" for a name with no owner.
func factoryRepoOwner(full string) string {
	if i := strings.IndexByte(full, '/'); i >= 0 {
		return full[:i]
	}
	return ""
}

// matches says whether the filter keeps r. KEEP IT PREDICTABLE: `owner/`
// narrows to that owner and anything after the slash is looked for in the
// name; plain words are looked for anywhere in `owner/name`. Case never
// matters, and nothing is scored or reordered by how well it matched.
func (p *factoryPicker) matches(r factoryPickRow) bool {
	q := strings.ToLower(strings.TrimSpace(p.query))
	if q == "" {
		return true
	}
	full := strings.ToLower(r.full)
	i := strings.IndexByte(q, '/')
	if i < 0 {
		return strings.Contains(full, q)
	}
	owner, name := q[:i], q[i+1:]
	if owner != "" && strings.ToLower(r.owner) != owner {
		return false
	}
	if j := strings.IndexByte(full, '/'); j >= 0 {
		full = full[j+1:]
	}
	return strings.Contains(full, name)
}

// sections is the picker's blocks as drawn: WATCHING, what was watched when
// the picker opened, then one block per owner, owners ordered by their most
// recently pushed repository. Inside each block the repositories go by the
// last push, or by how many are open under `ctrl+o`. A block the filter empties is
// not drawn.
func (p *factoryPicker) sections() []factoryPickSection {
	var watching []int
	groups := map[string][]int{}
	var owners []string
	for i, r := range p.rows {
		if !p.matches(r) {
			continue
		}
		if p.was[strings.ToLower(r.full)] {
			watching = append(watching, i)
			continue
		}
		k := strings.ToLower(r.owner)
		if _, ok := groups[k]; !ok {
			owners = append(owners, k)
		}
		groups[k] = append(groups[k], i)
	}
	latest := func(rows []int) time.Time {
		var t time.Time
		for _, i := range rows {
			if p.rows[i].pushed.After(t) {
				t = p.rows[i].pushed
			}
		}
		return t
	}
	sort.SliceStable(owners, func(i, j int) bool { return latest(groups[owners[i]]).After(latest(groups[owners[j]])) })
	var out []factoryPickSection
	if len(watching) > 0 {
		out = append(out, factoryPickSection{heading: "watching", rows: p.ordered(watching)})
	}
	for _, k := range owners {
		rows := p.ordered(groups[k])
		out = append(out, factoryPickSection{heading: p.rows[rows[0]].owner, rows: rows})
	}
	return out
}

// ordered is rows by the picker's order: the last push, the newest first, or
// under `ctrl+o` the open count, the most first and the last push after it.
func (p *factoryPicker) ordered(rows []int) []int {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := p.rows[rows[i]], p.rows[rows[j]]
		if p.byOpen && a.open != b.open {
			return a.open > b.open
		}
		return a.pushed.After(b.pushed)
	})
	return rows
}

// visible is the picker's rows the filter keeps, as indexes, in the order
// the cursor walks them: section by section, top to bottom.
func (p *factoryPicker) visible() []int {
	var out []int
	for _, s := range p.sections() {
		out = append(out, s.rows...)
	}
	return out
}

// anyOpen says whether any repository carries an open count, which is when
// ordering by it means anything.
func (p *factoryPicker) anyOpen() bool {
	for _, r := range p.rows {
		if r.open > 0 {
			return true
		}
	}
	return false
}

// keep puts the cursor back on row after the list reorders, or on the top
// when the row is no longer listed.
func (p *factoryPicker) keep(row int) {
	p.cursor = 0
	for i, v := range p.visible() {
		if v == row {
			p.cursor = i
			return
		}
	}
}

// watching is the repositories the person has ticked, in the list's order.
func (p *factoryPicker) watching() []string {
	var out []string
	for _, r := range p.rows {
		if p.on[strings.ToLower(r.full)] {
			out = append(out, r.full)
		}
	}
	return out
}

// factoryPickKey is a key while the picker stands. TYPING FILTERS: every
// printable key the picker does not bind goes into the filter, so a person who
// knows the name types it. The bound keys are the arrows, `space` (watch),
// `enter` (save), `backspace`, `esc` (clear the filter, then close without
// saving) and `ctrl+o` (order by open, when something carries an open count),
// so `o` stays a letter and a name that starts with one is typed like any
// other (one key, one meaning). A `/` with
// nothing typed yet is answered and kept out, for the person who tries it
// first; after words it is the owner's slash.
func (a *app) factoryPickKey(msg tea.KeyPressMsg) tea.Cmd {
	p := a.fp.pick
	vis := p.visible()
	switch k := msg.String(); k {
	case "esc":
		if p.query != "" {
			p.query, p.cursor = "", 0
		} else {
			a.fp.pick = nil
		}
	case "enter":
		return a.factorySaveRepos()
	case "up", "ctrl+p":
		p.cursor = moveCursor(p.cursor, -1, len(vis))
	case "down", "ctrl+n":
		p.cursor = moveCursor(p.cursor, 1, len(vis))
	case "space", " ":
		if p.cursor >= 0 && p.cursor < len(vis) {
			name := strings.ToLower(p.rows[vis[p.cursor]].full)
			p.on[name] = !p.on[name]
		}
	case "backspace":
		if r := []rune(p.query); len(r) > 0 {
			p.query, p.cursor = string(r[:len(r)-1]), 0
		}
	case "ctrl+u":
		p.query, p.cursor = "", 0
	case "ctrl+k":
		// THE CARET STANDS AT THE END OF THE WORDS, so the kill to the end
		// has nothing after it to take; the key is answered and keeps them.
	case "ctrl+o":
		if p.anyOpen() {
			row := -1
			if p.cursor >= 0 && p.cursor < len(vis) {
				row = vis[p.cursor]
			}
			p.byOpen = !p.byOpen
			p.keep(row)
		}
	case "/":
		if p.query != "" {
			p.query, p.cursor = p.query+"/", 0
		}
	default:
		if t := msg.Key().Text; t != "" && t != " " && msg.Key().Mod&(tea.ModCtrl|tea.ModMeta|tea.ModSuper) == 0 {
			p.query, p.cursor = p.query+t, 0
		}
	}
	a.touch()
	return nil
}

// factorySaveRepos is `enter` on the picker: the ticked repositories are the
// watched ones now, and the note line says how many and that they are being
// read.
//
// FROM THE SAVE UNTIL THE FLOOR SHOWS THE READ, THE FLOOR SAYS IT IS READING
// (recording, 2026-10-08: the foot said `reading them now` while the head
// above still said quiet for the one to three seconds before the next beat).
// A non-empty save stands [factoryPage.readingSince] and reads the floor every
// second until it clears ([app.factoryReadSoon]); an empty one owes no read,
// so it stands nothing and retires any earlier save's.
func (a *app) factorySaveRepos() tea.Cmd {
	list := a.fp.pick.watching()
	saved := a.now()
	a.fp.act.doing = "saving…"
	return a.factoryDoThen(func(s factory.Seam) error { return s.SetRepos(list) }, func(err error) tea.Cmd {
		if err != nil {
			return nil
		}
		a.fp.pick = nil
		a.fp.readingGen++
		if len(list) == 0 {
			a.fp.readingSince, a.fp.readingRepos = time.Time{}, 0
			a.factorySay(factoryWatchingWords(0))
			return nil
		}
		// THE RECEIPT IS THE READ'S STATE, NOT A NOTE OF ITS OWN: the note
		// line draws it from readingSince ([app.factoryReadNote]), so it
		// turns and clears in the same frame the head and the bare line do.
		a.pageMsg = ""
		a.fp.readingSince, a.fp.readingRepos = saved, len(list)
		a.factoryFoldFirstRead()
		return a.factoryReadSoonArm()
	})
}

// factoryWatchingWords is the picker's receipt: how many are watched and that
// the read has begun. The progress itself is the handover's
// ([app.factoryHeadFresh]) and the bare floor's ([factoryBareReadingWords]).
func factoryWatchingWords(n int) string {
	if n == 0 {
		return "watching no repositories · the floor keeps what chat and n bring"
	}
	return factoryWatchedWords(n) + rowSep + factoryReadingNowWords
}

// ── the first read, from the save ───────────────────────────────────────────

// factoryFirstReadWords is the handover's fresh clause, after the spinner,
// from the picker's save until the floor shows the read.
const factoryFirstReadWords = "reading the repositories you watch"

// factoryFirstReadWait is how long a save's reading moment spins with no
// snapshot showing the read. Past it the floor does not go back to its own
// words: the read was asked for and has not answered, and the floor says that
// ([factoryNoWordWords]) until a snapshot shows the read.
const factoryFirstReadWait = 30 * time.Second

// factoryNoWordWords is the handover's clause once [factoryFirstReadWait] has
// passed with no word from the read, and factoryBareNoWordWords the bare
// floor's line under it: what is true (asked, not answered) and what a person
// can do. NO SPINNER: nothing is known to be moving (owner screenshots,
// 2026-10-08 15:14 and 15:15, where the head went back to `quiet` forty-six
// seconds after a save while no poller was alive and the foot still said
// `reading them now`).
const (
	factoryNoWordWords     = "no word from the read yet"
	factoryBareNoWordWords = "the read was asked for and has not answered · R to check the repositories · if codeaf was just updated, restart it"
)

// factoryReadSoonEvery is the floor's one-second beat, quicker than its own
// ([homeEvery]), armed only while [app.factoryWantsSecondBeat] says so.
const factoryReadSoonEvery = time.Second

// factoryReadSoonMsg is one beat of that re-read, carrying the save it is for.
type factoryReadSoonMsg struct{ gen int }

// factoryFirstReading says whether the save's reading moment spins now: a
// read asked for, inside [factoryFirstReadWait], with no word yet.
func (a *app) factoryFirstReading() bool {
	at := a.fp.readingSince
	return !at.IsZero() && a.now().Sub(at) < factoryFirstReadWait
}

// factoryWantsSecondBeat says whether the floor is read every second rather
// than on its three-second beat. ONE BEAT, TWO REASONS: a save's reading
// moment stands, or THE OPEN ITEM PAGE HAS A STAGE RUNNING (owner's
// screenshot, 2026-10-08: `plan · 15s` and its log tail moved once in three
// seconds while a person sat watching it work), so its time counts and its
// log grows as the stage works ([app.factoryPageRunning]).
func (a *app) factoryWantsSecondBeat() bool {
	return a.factoryFirstReading() || a.factoryPageRunning()
}

// factoryNoWord says whether a save's read was asked for, the wait has passed
// and no snapshot has shown it: the state the floor names with
// [factoryNoWordWords].
func (a *app) factoryNoWord() bool {
	at := a.fp.readingSince
	return !at.IsZero() && a.now().Sub(at) >= factoryFirstReadWait
}

// factoryReadNote is the note line's receipt for the save, drawn from the one
// state the head and the bare line read: `watching 3 repositories · reading
// them now` while the moment spins, `watching 3 repositories` once there is
// no word, and "" once none is owed; when the read becomes known
// [app.factoryFoldFirstRead] leaves `watching 3 repositories` on the line. A
// sentence a later key put on the line ([app.pageMsg]) outranks it.
func (a *app) factoryReadNote() string {
	n := a.fp.readingRepos
	switch {
	case n <= 0:
		return ""
	case a.factoryFirstReading():
		return factoryWatchingWords(n)
	case a.factoryNoWord():
		return factoryWatchedWords(n)
	}
	return ""
}

// factoryFoldFirstRead clears the save's read once the snapshot shows it is
// known: a source mid-poll (its own clause says where), a source that
// answered after the save, or any item on the floor. The wait passing clears
// nothing; it turns the moment into no word ([app.factoryNoWord]).
func (a *app) factoryFoldFirstRead() {
	at := a.fp.readingSince
	if at.IsZero() {
		return
	}
	snap := a.fp.snap
	known := len(snap.Items) > 0
	for _, src := range snap.Sources {
		if src.Polling || src.Polled.After(at) {
			known = true
		}
	}
	if known {
		// The receipt keeps what is still true once the read is known, how
		// many are watched, and drops `reading them now` with the head's
		// clause; the rows or the source's own clause carry the read now.
		if n := a.fp.readingRepos; n > 0 && a.pageMsg == "" && a.at(pageFactory) {
			a.pageMsg = factoryWatchedWords(n)
		}
		a.fp.readingSince, a.fp.readingRepos = time.Time{}, 0
	}
}

// factoryWatchedWords is the receipt with no read in it: how many are watched.
func factoryWatchedWords(n int) string {
	return "watching " + itoa(n) + " " + factoryPlural(n, "repository", "repositories")
}

// factoryReadSoonArm arms the next one-second re-read for the save standing.
func (a *app) factoryReadSoonArm() tea.Cmd {
	gen := a.fp.readingGen
	a.fp.readingArmed = true
	return surfaceTick(factoryReadSoonEvery, func(time.Time) tea.Msg { return factoryReadSoonMsg{gen: gen} })
}

// factoryReadSoonWake arms the beat when the floor wants it and none is in
// the air: the loop asks it after every message, so an item page opened on a
// running stage, or a stage that starts under an open page, starts the beat.
func (a *app) factoryReadSoonWake() tea.Cmd {
	if a.fp.readingArmed || !a.factoryWantsSecondBeat() {
		return nil
	}
	return a.factoryReadSoonArm()
}

// factoryReadSoon is that beat, arriving: the floor is read and the beat
// re-armed while [app.factoryWantsSecondBeat] holds, and nothing once the
// reading moment has cleared and no stage runs on an open page. A beat a
// later save replaced asks nothing. The beat that finds the wait passed
// redraws once, so head, bare line and note turn to no word in one frame
// ([app.factoryNoWord]), and then stops unless a stage runs on the open page:
// the floor's own beat ([homeEvery]) goes on reading, and a snapshot that
// shows the read clears no word as it clears the moment.
func (a *app) factoryReadSoon(gen int) tea.Cmd {
	if gen != a.fp.readingGen {
		return nil
	}
	a.fp.readingArmed = false
	if !a.fp.readingSince.IsZero() && !a.factoryFirstReading() {
		a.touch()
	}
	if !a.factoryWantsSecondBeat() {
		return nil
	}
	return tea.Batch(a.factoryRead(), a.factoryReadSoonArm())
}

// The picker's fact columns, left to right ([factoryPickCols]).
const (
	factoryPickColOpen = iota
	factoryPickColPrivate
	factoryPickColHere
	factoryPickColPushed
)

// factoryPickCol is one fact column: its cells, and what a row says in it,
// "" for nothing.
type factoryPickCol struct {
	w    int
	cell func(r factoryPickRow, now time.Time) string
}

// factoryPickCols are the facts that choose: how much is waiting, whether it
// is private, whether it is checked out here (`here`, where the floor's
// stages can run), and when it was last pushed to.
var factoryPickCols = []factoryPickCol{
	factoryPickColOpen: {factoryPickOpenW, func(r factoryPickRow, _ time.Time) string {
		if r.open <= 0 {
			return ""
		}
		return factoryOpenCount(r.open) + " open"
	}},
	factoryPickColPrivate: {factoryPickPrivateW, func(r factoryPickRow, _ time.Time) string {
		if r.private {
			return "private"
		}
		return ""
	}},
	factoryPickColHere: {factoryPickHereW, func(r factoryPickRow, _ time.Time) string {
		if r.dir != "" {
			return "here"
		}
		return ""
	}},
	factoryPickColPushed: {factoryPickPushedW, func(r factoryPickRow, now time.Time) string {
		if ago := factoryPushedAgo(now, r.pushed); ago != "" {
			return "pushed " + ago
		}
		return ""
	}},
}

// factoryPickDrop is the order the fact columns give way on a narrow picker:
// the least deciding first, and the open count, what a maintainer chooses
// by, last.
var factoryPickDrop = []int{factoryPickColPrivate, factoryPickColPushed, factoryPickColHere, factoryPickColOpen}

// factoryOpenCount is an open count in at most four cells: `12`, `999`,
// `12k`.
func factoryOpenCount(n int) string {
	const thousand = 1000
	if n < thousand {
		return itoa(n)
	}
	return itoa(n/thousand) + "k"
}

// factoryPickColumns is the fact columns the picker draws over rows at
// measure, and the name's width beside them. A COLUMN NO ROW HAS ANYTHING IN
// IS NOT DRAWN (the emptiness law), and while the name would be narrower than
// [factoryPickNameMinW] the columns give way in [factoryPickDrop]'s order.
func (p *factoryPicker) columns(rows []int, now time.Time, measure int) ([]int, int) {
	keep := map[int]bool{}
	for c, col := range factoryPickCols {
		for _, i := range rows {
			if col.cell(p.rows[i], now) != "" {
				keep[c] = true
				break
			}
		}
	}
	nameW := func() int {
		w := measure - factoryPickMarkW
		for c := range keep {
			w -= factoryGutter + factoryPickCols[c].w
		}
		return w
	}
	for _, c := range factoryPickDrop {
		if nameW() >= factoryPickNameMinW {
			break
		}
		delete(keep, c)
	}
	var out []int
	for c := range factoryPickCols {
		if keep[c] {
			out = append(out, c)
		}
	}
	return out, max(nameW(), 0)
}

// factoryPickLine is one line of the picker's list: a section's heading, the
// blank between two sections, or a repository's row.
type factoryPickLine struct {
	heading string
	count   int
	row     int // index into the picker's rows, -1 for a heading or a blank
	walk    int // the row's place in [factoryPicker.visible], -1 otherwise
}

// factoryPickerBody is the picker as exactly room rows of exactly width
// cells: its heading with how many are watched and listed at the right, the
// filter line, then the sections, each under its heading, the cursor's row on
// the cursor ground, and at the foot the cursor's repository's checkout.
//
//	watch · repositories github sees as santoshkumarradha   1 watching · 143 listed
//	› ▌ type to filter
//
//	WATCHING · 1 ──────────────────────────────────────────────────────────────
//	[x] agentfield/codeaf                       12 open  private  here   pushed 2d
//
//	AGENTFIELD · 1 ────────────────────────────────────────────────────────────
//	[ ] agentfield/agentfield                    3 open                  pushed 5h
//
//	checked out at /work/codeaf
func (a *app) factoryPickerBody(width, room int) []placeRow {
	pal, p := a.pal, a.fp.pick
	measure := max(width-factoryMargins, 0)
	heading := "watch" + rowSep + "repositories"
	if p.login != "" {
		heading += " github sees as " + p.login
	}
	var counts []string
	if n := len(p.watching()); n > 0 {
		counts = append(counts, itoa(n)+" watching")
	}
	if n := len(p.rows); n > 0 {
		counts = append(counts, itoa(n)+" listed")
	}
	lines := []string{
		factorySpread(pal.muted(heading), pal.muted(strings.Join(counts, rowSep)), measure),
		a.factoryPickFilter(measure),
		"",
	}
	head := len(lines)
	secs := p.sections()
	var vis []int
	for _, s := range secs {
		vis = append(vis, s.rows...)
	}
	p.shown = 0
	walks := map[int]int{}
	foot := ""
	switch {
	case len(vis) == 0 && p.query != "":
		lines = append(lines, pal.dim(fit("no repository matches "+p.query, measure)))
	case len(vis) == 0:
		lines = append(lines, pal.dim(fit("the repositories github shows this account are listed here", measure)))
	default:
		p.cursor = moveCursor(p.cursor, 0, len(vis))
		foot = a.factoryPickFoot(p.rows[vis[p.cursor]], measure)
		left := room - len(lines)
		if foot != "" {
			left -= factoryActionRows
		}
		now := a.fp.snap.Now
		if now.IsZero() {
			now = time.Now()
		}
		cols, nameW := p.columns(vis, now, measure)
		var list []factoryPickLine
		cur, walk := 0, 0
		for i, s := range secs {
			if i > 0 {
				list = append(list, factoryPickLine{row: -1, walk: -1})
			}
			list = append(list, factoryPickLine{heading: s.heading, count: len(s.rows), row: -1, walk: -1})
			for _, r := range s.rows {
				if walk == p.cursor {
					cur = len(list)
				}
				list = append(list, factoryPickLine{row: r, walk: walk})
				walk++
			}
		}
		p.top = placeTop(p.top, cur, len(list), max(left, 0))
		// A SECTION'S FIRST ROW NEVER STANDS WITHOUT ITS HEADING above it.
		if p.top > 0 && p.top == cur && list[cur-1].heading != "" {
			p.top--
		}
		for i := p.top; i < len(list) && i-p.top < left; i++ {
			l := list[i]
			switch {
			case l.heading != "":
				lines = append(lines, a.factoryHeading(factoryRailRow{heading: l.heading, count: l.count}, measure))
			case l.row < 0:
				lines = append(lines, "")
			default:
				walks[len(lines)] = l.walk
				lines = append(lines, a.factoryPickRowText(p.rows[l.row], cols, nameW, now, measure, l.walk == p.cursor))
				p.shown++
			}
		}
	}
	rows := make([]placeRow, room)
	p.hits = make([]int, room)
	for i := range rows {
		text := ""
		if i < len(lines) && lines[i] != "" {
			text = factoryMarginPad() + lines[i]
		}
		rows[i] = placeRow{text: factoryPad(text, width), hit: -1}
		p.hits[i] = -1
		if walk, ok := walks[i]; ok {
			p.hits[i] = walk
		}
	}
	if foot != "" && room >= head+factoryActionRows {
		rows[room-1] = placeRow{text: factoryPad(factoryMarginPad()+foot, width), hit: -1}
		p.hits[room-1] = -1
	}
	return rows
}

// factoryPickHover is the pointer resting on screen row y over the picker:
// the repository drawn there takes the cursor, as a row on the floor does
// ([app.factoryHover]). It reports whether a repository's row took it.
func (a *app) factoryPickHover(y int) bool {
	p, row := a.fp.pick, y-placeHeadRows
	if row < 0 || row >= len(p.hits) || p.hits[row] < 0 {
		return false
	}
	if p.hits[row] != p.cursor {
		p.cursor = p.hits[row]
		a.touch()
	}
	return true
}

// factoryPickRowText is one repository's row, exactly measure cells: its mark,
// its full name in the name's column, and its facts each right-aligned in its
// own column. The cursor's row wears the cursor ground and nothing else
// changes on it.
func (a *app) factoryPickRowText(r factoryPickRow, cols []int, nameW int, now time.Time, measure int, cur bool) string {
	pal := a.pal
	mark, name := pal.dim("[ ]"), pal.muted(fit(r.full, nameW))
	if a.fp.pick.on[strings.ToLower(r.full)] {
		mark, name = pal.ink("[x]"), pal.ink(fit(r.full, nameW))
	}
	row := factoryPad(factoryPad(mark, factoryPickMarkW)+name, factoryPickMarkW+nameW)
	for _, c := range cols {
		col := factoryPickCols[c]
		cell := fit(col.cell(r, now), col.w)
		row += factorySpaces(factoryGutter + col.w - ansi.StringWidth(cell))
		switch c {
		case factoryPickColOpen, factoryPickColHere:
			// THE FACTS THAT CHOOSE are one step louder than the rest.
			row += pal.muted(cell)
		default:
			row += pal.dim(cell)
		}
	}
	row = factoryPad(row, measure)
	if cur {
		return pal.cursorRow(row, measure)
	}
	return row
}

// factoryPickFilter is the filter line, in the typing row's look
// (factory_keys.go): the prompt mark, the words and the cursor, or the
// cursor and a dim `type to filter` while nothing is typed.
func (a *app) factoryPickFilter(measure int) string {
	pal, p := a.pal, a.fp.pick
	label := pal.accent(a.icon(tokens.GPromptChat)) + " "
	if p.query == "" {
		return fit(label+pal.cursor(" ", 1)+" "+pal.dim("type to filter"), measure)
	}
	return fit(label+pal.ink(p.query)+pal.cursor(" ", 1), measure)
}

// factoryPickFoot is the picker's last line, about the repository under the
// cursor: where it is checked out, or, in the recipe page's own words
// ([factoryNoCheckoutWords]), that this machine does not know, and what that
// means for it. THE FLOOR'S STAGES RUN ONLY IN A CHECKOUT, so a repository
// without one is watched and read, and its stages wait until it is cloned.
func (a *app) factoryPickFoot(r factoryPickRow, measure int) string {
	if r.dir != "" {
		return a.pal.dim(fit("checked out at "+r.dir, measure))
	}
	return a.pal.dim(fit(factoryNoCheckoutWords(r.full)+rowSep+"it is watched and read, and its stages wait until it is cloned", measure))
}

// factoryPushedAgo is how long ago, in the largest unit that is at least one:
// `40m`, `5h`, `2d`, `3w`, `4mo`, `2y`. Nothing for an unknown time.
func factoryPushedAgo(now, then time.Time) string {
	if now.IsZero() || then.IsZero() {
		return ""
	}
	d := now.Sub(then)
	day := 24 * time.Hour
	switch {
	case d < time.Hour:
		return strconv.Itoa(max(int(d/time.Minute), 1)) + "m"
	case d < day:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d < 14*day:
		return strconv.Itoa(int(d/day)) + "d"
	case d < 60*day:
		return strconv.Itoa(int(d/(7*day))) + "w"
	case d < 730*day:
		return strconv.Itoa(int(d/(30*day))) + "mo"
	}
	return strconv.Itoa(int(d/(365*day))) + "y"
}

// factoryPickerHint is the hint line while the picker stands: the keys, `ctrl+o`
// only where an open count is drawn and saying the order it would change to,
// and `type to filter` while nothing is typed.
func (a *app) factoryPickerHint() string {
	p := a.fp.pick
	if p.query != "" {
		return "space watch · enter save · esc clear"
	}
	parts := []string{"space watch", "enter save"}
	if p.anyOpen() {
		if p.byOpen {
			parts = append(parts, "ctrl+o by pushed")
		} else {
			parts = append(parts, "ctrl+o by open")
		}
	}
	return strings.Join(append(parts, "esc close", "type to filter"), rowSep)
}

// ── the connect prompt ──────────────────────────────────────────────────────

// factoryConnectPrompt asks how to reach GitHub: gh's own login when gh
// answered with one, and a token otherwise. A SEAM THAT CANNOT KEEP AN ANSWER
// ASKS NOTHING, and says so.
func (a *app) factoryConnectPrompt(login string) {
	a.fp.pick = nil
	if !a.factory.Has("connectgithub") {
		a.factorySay("github is not connected on this machine")
		return
	}
	if login != "" {
		a.fp.ghOffer = login
		a.pageMsg = ""
		a.touch()
		return
	}
	a.factoryOpenAsk(factoryAsk{kind: factoryAskToken, label: "github token ›", example: "“a token from github.com/settings/tokens”"})
}

// factoryConnectAsk is the connect prompt asked from somewhere that has not
// read gh yet: the settings place's `github` row.
func (a *app) factoryConnectAsk() tea.Cmd {
	seam := a.factory
	return a.offLoop(func() func(bool) tea.Cmd {
		login := factoryGHLogin(context.Background(), seam)
		return func(bool) tea.Cmd {
			a.factoryConnectPrompt(login)
			return nil
		}
	})
}

// factoryOfferKey is a key while gh's login is offered: `y` uses it, `n`
// asks for a token instead, `esc` asks nothing.
func (a *app) factoryOfferKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "y":
		a.fp.ghOffer = ""
		return a.factoryConnect("")
	case "n":
		a.fp.ghOffer = ""
		a.factoryConnectPrompt("")
	case "esc":
		a.fp.ghOffer = ""
		a.touch()
	}
	return nil
}

// factoryConnect keeps a way to reach GitHub ("" is gh's own login) and, once
// it is kept, opens the picker it was asked for. THE TOKEN IS NEVER SAID: a
// refusal says what went wrong, never the words typed.
func (a *app) factoryConnect(token string) tea.Cmd {
	connect := a.factory.ConnectGitHub
	if connect == nil {
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), factoryForgeWait)
		defer cancel()
		err := connect(ctx, token)
		return func(bool) tea.Cmd {
			if err != nil {
				a.factorySay(strings.TrimSpace(err.Error()))
				return nil
			}
			a.factorySay("github connected")
			if a.factory.Has("repos") && a.factory.Has("setrepos") {
				return a.factoryOpenRepos()
			}
			return nil
		}
	})
}

// factoryOfferRows is the gh offer as the foot draws it.
func (a *app) factoryOfferRows(measure int) []string {
	login := a.fp.ghOffer
	if login == "" || measure <= 0 {
		return nil
	}
	pal := a.pal
	ask, keys := "connect github: gh is logged in as "+login+",", "use it? "
	tail := pal.accent("[y]") + pal.dim(" · [n] a token instead")
	// ONE LINE WHERE IT FITS, and broken after the login where it does not:
	// the pane beside the rows is narrower than the sentence, and a question
	// cut before its key is a question nobody can answer.
	if ansi.StringWidth(ask+" "+keys+"[y]") <= measure {
		return []string{fit(pal.ink(ask+" "+keys)+tail, measure)}
	}
	return []string{pal.ink(fit(ask, measure)), fit(pal.ink(keys)+tail, measure)}
}

// ── E: the recipe page ──────────────────────────────────────────────────────

// factoryRecipeRepo is the repository `E` opens: the one the floor's `[ ]`
// line shows, or the floor's only one; "" when the floor shows all of several.
func (a *app) factoryRecipeRepo() string {
	repos := a.fp.snap.Repos
	if r := a.fp.repo; r > 0 && r <= len(repos) {
		return repos[r-1].Name
	}
	if len(repos) == 1 {
		return repos[0].Name
	}
	return ""
}

// factoryOpenRecipe is `E`. The page draws at once from the recipe the floor
// already read, and the file is read again off the loop for what the floor
// does not carry: the lines that did not load, and where the file is.
func (a *app) factoryOpenRecipe() tea.Cmd {
	repo := a.factoryRecipeRepo()
	if repo == "" {
		a.factorySay("pick a repository with [ ] first · a recipe is one repository's")
		return nil
	}
	base := factory.DefaultRecipe()
	if r, ok := a.fp.snap.RepoNamed(repo); ok {
		base = r.Recipe
	}
	a.pageMsg = ""
	a.fp.stage = 0
	a.fp.recipe = &factoryRecipePage{repo: repo, recipe: factoryCopyRecipe(base)}
	a.touch()
	read := a.factory.RecipeAt
	if read == nil {
		a.fp.recipe.read = true
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		r, probs, dir, err := read(repo)
		return func(bool) tea.Cmd {
			p := a.fp.recipe
			if p == nil || p.repo != repo {
				return nil
			}
			p.read = true
			if err != nil {
				a.factorySay("the recipe file could not be read · " + strings.TrimSpace(err.Error()))
			}
			p.recipe, p.problems, p.dir = factoryCopyRecipe(r), probs, dir
			a.touch()
			return nil
		}
	})
}

// factoryCopyRecipe is r with its own stage lists, so the page's changes never
// reach the snapshot it was read from.
func factoryCopyRecipe(r factory.Recipe) factory.Recipe {
	out := factory.Recipe{
		Stages: factory.CopyStages(r.Stages),
		Policy: append([]string(nil), r.Policy...),
		Habits: append([]string(nil), r.Habits...),
	}
	if r.ByKind != nil {
		out.ByKind = map[factory.Kind][]factory.Stage{}
		for k, s := range r.ByKind {
			out.ByKind[k] = factory.CopyStages(s)
		}
	}
	return out
}

// kindNow is the tab the page is on.
func (p *factoryRecipePage) kindNow() factory.Kind {
	return factoryRecipeKinds[moveCursor(p.kind, 0, len(factoryRecipeKinds))]
}

// stages is the tab's stages.
func (p *factoryRecipePage) stages() []factory.Stage { return p.recipe.For(p.kindNow()) }

// setStages writes the tab's stages back onto the page's recipe, under the
// tab's own kind, so a change to `pr` never becomes the issue list it fell
// back to.
func (p *factoryRecipePage) setStages(stages []factory.Stage) {
	if p.recipe.ByKind == nil {
		p.recipe.ByKind = map[factory.Kind][]factory.Stage{}
	}
	p.recipe.ByKind[p.kindNow()] = stages
}

// factoryRecipeEdits says whether the page's keys change anything: the seam
// can save a recipe, and this machine knows where the repository is checked
// out. A PAGE THAT CANNOT BE SAVED IS NOT EDITED, because a change nothing can
// keep is a change that is lost on `esc`.
func (a *app) factoryRecipeEdits() bool {
	p := a.fp.recipe
	return p != nil && p.dir != "" && a.factory.Has("saverecipe")
}

// factoryRecipeNoDir is the note line's sentence on a page that cannot be
// saved because its checkout is unknown, and "" otherwise.
func (a *app) factoryRecipeNoDir() string {
	p := a.fp.recipe
	if p == nil || !p.read || p.dir != "" {
		return ""
	}
	return factoryNoCheckoutWords(p.repo)
}

// factoryNoCheckoutWords is the sentence for a repository this machine has
// no checkout of, said by the recipe page and the repo picker alike.
func factoryNoCheckoutWords(repo string) string {
	return "codeaf does not know where " + repo + " is checked out"
}

// factoryRecipeKey is a key on the recipe page. EVERY KEY IS THE PAGE'S while
// it stands, so a letter meant for a stage never reaches an item underneath.
func (a *app) factoryRecipeKey(msg tea.KeyPressMsg) tea.Cmd {
	p := a.fp.recipe
	k := msg.String()
	switch k {
	case "esc":
		a.fp.recipe, a.fp.stage = nil, 0
		a.pageMsg = ""
		a.touch()
		return nil
	case "[", "h":
		p.kind = (p.kind + len(factoryRecipeKinds) - 1) % len(factoryRecipeKinds)
		a.fp.stage = 0
	case "]", "l":
		p.kind = (p.kind + 1) % len(factoryRecipeKinds)
		a.fp.stage = 0
	case "up", "ctrl+p":
		a.fp.stage = moveCursor(a.fp.stage, -1, len(p.stages()))
	case "down", "ctrl+n":
		a.fp.stage = moveCursor(a.fp.stage, 1, len(p.stages()))
	}
	if !a.factoryRecipeEdits() {
		a.touch()
		return nil
	}
	stages := factory.CopyStages(p.stages())
	switch k {
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if at := int(k[0] - '1'); at < len(stages) {
			stages[at].On = !stages[at].On
			p.setStages(stages)
		}
	case "e":
		if at := a.fp.stage; at >= 0 && at < len(stages) {
			stages[at].Effort = factoryNextEffort(stages[at].Effort)
			p.setStages(stages)
		}
	case "s":
		a.factoryOpenAsk(factoryAsk{kind: factoryAskRecipeStage, label: "+ stage ›", example: "“after review, make it neater”"})
	case "w":
		if a.fp.stage < len(stages) {
			a.factoryOpenAsk(factoryAsk{kind: factoryAskKnobs, label: stages[a.fp.stage].Name + " ›", example: "“until clean, max 3, effort strong”"})
		}
	case "b":
		return a.factorySaveRecipe()
	}
	a.touch()
	return nil
}

// factoryRecipeAdd is the `s` row's words: a stage placed by them ("after
// review, …"), the way an item's `s` places one.
func (a *app) factoryRecipeAdd(words string) {
	p := a.fp.recipe
	if p == nil {
		return
	}
	if factory.ParseStage(words).Ask == "" {
		a.factorySay("say what the stage should do")
		return
	}
	p.setStages(factory.AddStageWords(factory.CopyStages(p.stages()), words))
	a.touch()
}

// factoryRecipeKnobs is the `w` row's words, read as the recipe file's own
// knobs on the selected stage: `until clean, max 3, effort strong`. THE FILE'S
// READER IS THE ONE THAT READS THEM, by writing the stage's line with the words
// after it and parsing that, so a knob means here exactly what it means in
// `.codeaf/factory.md`, and a word it cannot read is its own sentence.
func (a *app) factoryRecipeKnobs(words string) {
	p := a.fp.recipe
	if p == nil {
		return
	}
	stages := factory.CopyStages(p.stages())
	at := a.fp.stage
	if at < 0 || at >= len(stages) {
		return
	}
	line := strings.TrimPrefix(factory.StageLines(stages[at : at+1])[0], "1. ")
	for _, seg := range factoryKnobSegments(words) {
		line += " · " + seg
	}
	kind := p.kindNow()
	got, probs := factory.Parse("## " + string(kind) + "\n1. " + line + "\n")
	if len(probs) > 0 {
		a.factorySay(probs[0].Why)
		return
	}
	if s := got.For(kind); len(s) > 0 {
		stages[at] = s[0]
		p.setStages(stages)
	}
	a.touch()
}

// factoryKnobSegments splits knob words on commas and the file's own dots. A
// proof's list keeps its commas: everything after `proof` is that one knob.
func factoryKnobSegments(words string) []string {
	words = strings.ReplaceAll(words, "·", ",")
	var out []string
	lower := strings.ToLower(words)
	proof := ""
	if i := strings.Index(lower, "proof "); i >= 0 && (i == 0 || strings.ContainsRune(", ", rune(lower[i-1]))) {
		words, proof = words[:i], strings.TrimSpace(words[i:])
	}
	for _, seg := range strings.Split(words, ",") {
		if seg = strings.TrimSpace(seg); seg != "" {
			out = append(out, seg)
		}
	}
	if proof != "" {
		out = append(out, proof)
	}
	return out
}

// factorySaveRecipe is `b`: the whole recipe, every tab, written to the
// repository's file through the seam, and the floor read again.
func (a *app) factorySaveRecipe() tea.Cmd {
	p := a.fp.recipe
	repo, r := p.repo, factoryCopyRecipe(p.recipe)
	a.fp.act.doing = "saving…"
	return a.factoryDo(func(s factory.Seam) error { return s.SaveRecipe(repo, r) }, func(err error) {
		if err == nil {
			a.factorySay("saved to " + factory.RecipeFile + rowSep + "new work on " + factoryRepoShort(repo) + " runs it")
		}
	})
}

// factoryRecipeBody is the recipe page as exactly room rows of exactly width
// cells: the kind tabs with the repository at the right, the first line of the
// file that did not load when one did not, a blank, and the stages beside the
// stage's pane as the item page draws them (or one line of stages above it
// under [factoryStageFloor]).
//
//	Factory › codeaf › recipe                               issue · pr · ci
//	line 7: until is one of done, clean, green, proven
//
//	● plan              │ plan · chat · gate plan · when large
//	○ write             │ make the change
func (a *app) factoryRecipeBody(width, room int) []placeRow {
	pal, p := a.pal, a.fp.recipe
	measure := max(width-factoryMargin, 0)
	lead := factorySpaces(factoryMargin)
	var tabs []string
	for i, k := range factoryRecipeKinds {
		if i == p.kind {
			tabs = append(tabs, pal.ink(string(k)))
		} else {
			tabs = append(tabs, pal.dim(string(k)))
		}
	}
	// THE HEAD IS THE TRAIL OF CRUMBS the item page wears (factory_item.go's
	// [app.factoryCrumbs]), `Factory › codeaf › recipe`, with the kind tabs
	// at the right.
	lines := []string{lead + factorySpread(a.factoryCrumbs(p.repo)+pal.ink("recipe"), strings.Join(tabs, pal.dim(rowSep)), measure)}
	if len(p.problems) > 0 {
		pr := p.problems[0]
		words := "line " + itoa(pr.Line) + ": " + pr.Why
		if n := len(p.problems) - 1; n > 0 {
			words += rowSep + itoa(n) + " more"
		}
		lines = append(lines, lead+pal.dim(fit(words, measure)))
	}
	lines = append(lines, "")
	stages := p.stages()
	views := make([]factoryStageView, 0, len(stages))
	for _, st := range stages {
		views = append(views, factoryStageView{stage: st, state: factory.PhasePending, off: !st.On})
	}
	a.fp.stage = moveCursor(a.fp.stage, 0, len(views))
	left := room - len(lines)
	switch {
	case left <= 0 || len(views) == 0:
	case width < factoryStageFloor:
		lines = append(lines, lead+a.factoryStageStrip(views, measure))
		for _, line := range a.factoryRecipePane(views, measure, left-1) {
			lines = append(lines, lead+line)
		}
	default:
		paneW := width - factoryRailW - 1
		rail := a.factoryStageRail(views, left)
		pane := a.factoryRecipePane(views, max(paneW-factoryMargin, 0), left)
		sep := pal.dim(a.linearMark("│", "|"))
		for i := 0; i < left; i++ {
			right := ""
			if pane[i] != "" {
				right = lead + pane[i]
			}
			lines = append(lines, rail[i]+sep+factoryPad(right, paneW))
		}
	}
	rows := make([]placeRow, room)
	for i := range rows {
		text := ""
		if i < len(lines) {
			text = lines[i]
		}
		rows[i] = placeRow{text: factoryPad(text, width), hit: -1}
	}
	return rows
}

// factoryRecipePane is the selected stage as exactly room lines of at most
// measure cells: its knobs, its ask in ink, the rule, where it runs, and the
// verbs' rows on the last lines, as [app.factoryStagePane] draws an item's.
func (a *app) factoryRecipePane(views []factoryStageView, measure, room int) []string {
	out := make([]string, max(room, 0))
	if room <= 0 || a.fp.stage >= len(views) {
		return out
	}
	pal := a.pal
	v := views[a.fp.stage]
	lines := []string{
		pal.muted(fit(factoryKnobs(v.stage), measure)),
		pal.ink(fit(strings.TrimSpace(v.stage.Ask), measure)),
		pal.dim(strings.Repeat(a.linearMark("─", "-"), measure)),
	}
	switch {
	case v.off:
		lines = append(lines, pal.dim(fit("switched off in this recipe", measure)))
	default:
		after := "runs first"
		for i := a.fp.stage - 1; i >= 0; i-- {
			if !views[i].off {
				after = "runs after " + views[i].stage.Name
				break
			}
		}
		lines = append(lines, pal.dim(fit(after, measure)))
	}
	if len(v.stage.Proof) > 0 {
		lines = append(lines, pal.dim(fit("shows "+strings.Join(v.stage.Proof, ", "), measure)))
	}
	foot := a.factoryFootRows(measure)
	if room <= len(foot) {
		copy(out, foot[len(foot)-room:])
		return out
	}
	body := room - len(foot)
	for i := 0; i < body && i < len(lines); i++ {
		out[i] = lines[i]
	}
	copy(out[body:], foot)
	return out
}

// factoryRecipeHint is the hint line on the recipe page: the tabs and the
// walk, the edits only where the page can be saved, and the way back.
func (a *app) factoryRecipeHint() string {
	parts := []string{"[ ] kind", "↑↓ stages"}
	if a.factoryRecipeEdits() {
		parts = append(parts, "1-9 stages", "s stage", "e effort", "w in words", "b save")
	}
	return strings.Join(append(parts, "esc floor"), " · ")
}

// ── $: the day rail ─────────────────────────────────────────────────────────

// factorySetRail is the `$` row's words: a dollar figure, or 0 or `off` to
// take the rail off.
func (a *app) factorySetRail(words string) tea.Cmd {
	usd, ok := factoryRailWords(words)
	if !ok {
		a.factorySay("a rail is a dollar figure, like $60")
		return nil
	}
	return a.factoryDo(func(s factory.Seam) error { return s.SetRail(usd) }, func(err error) {
		switch {
		case err != nil:
		case usd == 0:
			a.factorySay("the day rail is off")
		default:
			a.factorySay("the day rail is " + factoryRailWord(usd))
		}
	})
}

// factoryRailWords reads a rail as typed: `$60`, `60`, `1,200`, `0`, `off`.
func factoryRailWords(words string) (float64, bool) {
	w := strings.ToLower(strings.TrimSpace(words))
	switch w {
	case "off", "none", "no rail":
		return 0, true
	}
	w = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(w, "$"), " a day"))
	w = strings.ReplaceAll(w, ",", "")
	usd, err := strconv.ParseFloat(w, 64)
	if err != nil || usd < 0 {
		return 0, false
	}
	return usd, true
}

// factoryAskSecret says the typing row's words are a secret, drawn masked.
func factoryAskSecret(ask *factoryAsk) bool { return ask != nil && ask.kind == factoryAskToken }

// factoryMask is a secret's words as the row draws them: one mark a rune.
func (a *app) factoryMask(text string) string {
	bullet := "•"
	if a.pal.ascii || a.linear {
		bullet = "*"
	}
	return strings.Repeat(bullet, len([]rune(text)))
}

// factorySettingsSubmit hands the new typing rows' words to their doors, and
// answers false for a row that is not one of them.
func (a *app) factorySettingsSubmit(ask factoryAsk, words string) (tea.Cmd, bool) {
	switch ask.kind {
	case factoryAskToken:
		return a.factoryConnect(words), true
	case factoryAskRail:
		return a.factorySetRail(words), true
	case factoryAskRecipeStage:
		a.factoryRecipeAdd(words)
		return nil, true
	case factoryAskKnobs:
		a.factoryRecipeKnobs(words)
		return nil, true
	}
	return nil, false
}

// factorySettingsVerb is the hint line's verb for the new typing rows.
func factorySettingsVerb(kind factoryAskKind) string {
	switch kind {
	case factoryAskToken:
		return "connect"
	case factoryAskRail:
		return "set the rail"
	case factoryAskRecipeStage:
		return "add the stage"
	case factoryAskKnobs:
		return "turn the knobs"
	}
	return ""
}

// factorySettingsHint is the floor's settings keys, for the hint line: only
// the doors the seam has, and `E` only where there is a repository.
func (a *app) factorySettingsHint() []string {
	var out []string
	if a.factory.Has("repos") && a.factory.Has("setrepos") {
		out = append(out, "R repos")
	}
	if len(a.fp.snap.Repos) > 0 {
		out = append(out, "E recipe")
	}
	if a.factory.Has("setrail") {
		out = append(out, "$ rail")
	}
	return out
}
