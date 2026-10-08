package tui3

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
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

// factoryPollWords is what the picker says after it saves, and the figure is
// the poll's own (cmd/codeaf's factoryPollEvery, and factory.md's `##
// connecting github`).
const factoryPollWords = "github polls every minute"

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
	// which is GitHub's own rule for a repository's name.
	on     map[string]bool
	cursor int
	top    int
	shown  int
	// query is the `/` filter, and typing whether its box has the keyboard.
	query  string
	typing bool
}

// factoryPickRow is one repository the picker lists.
type factoryPickRow struct {
	full    string
	private bool
	pushed  time.Time
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

// factoryOpenPicker stands the list over the floor. THE WATCHED REPOSITORIES
// COME FIRST that GitHub did not list, so a repository the token can no longer
// see is still one a person can stop watching; then everything GitHub listed,
// most recently pushed first.
func (a *app) factoryOpenPicker(login string, watched []string, available []factory.RepoInfo) {
	p := &factoryPicker{login: login, on: map[string]bool{}}
	listed := map[string]bool{}
	for _, r := range available {
		listed[strings.ToLower(r.Full)] = true
	}
	for _, w := range watched {
		p.on[strings.ToLower(w)] = true
		if !listed[strings.ToLower(w)] {
			p.rows = append(p.rows, factoryPickRow{full: w})
		}
	}
	for _, r := range available {
		if strings.TrimSpace(r.Full) != "" {
			p.rows = append(p.rows, factoryPickRow{full: r.Full, private: r.Private, pushed: r.Pushed})
		}
	}
	a.fp.pick = p
	a.touch()
}

// factoryPickVisible is the picker's rows the filter keeps, as indexes.
func (p *factoryPicker) visible() []int {
	q := strings.ToLower(strings.TrimSpace(p.query))
	out := make([]int, 0, len(p.rows))
	for i, r := range p.rows {
		if q == "" || strings.Contains(strings.ToLower(r.full), q) {
			out = append(out, i)
		}
	}
	return out
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

// factoryPickKey is a key while the picker stands: the arrows walk, `space`
// watches, `/` filters, `enter` saves and `esc` clears the filter, then
// closes without saving anything.
func (a *app) factoryPickKey(msg tea.KeyPressMsg) tea.Cmd {
	p := a.fp.pick
	k := msg.String()
	if p.typing {
		switch k {
		case "esc":
			p.query, p.typing, p.cursor = "", false, 0
		case "enter":
			p.typing = false
		case "backspace":
			if r := []rune(p.query); len(r) > 0 {
				p.query = string(r[:len(r)-1])
				p.cursor = 0
			}
		case "ctrl+u":
			p.query, p.cursor = "", 0
		case "ctrl+k":
			// THE CARET STANDS AT THE END OF THE WORDS, so the kill to the end
			// has nothing after it to take; the key is answered and keeps them.
		default:
			if t := msg.Key().Text; t != "" && t != " " && msg.Key().Mod&(tea.ModCtrl|tea.ModMeta|tea.ModSuper) == 0 {
				p.query += t
				p.cursor = 0
			}
		}
		a.touch()
		return nil
	}
	vis := p.visible()
	switch k {
	case "esc":
		if p.query != "" {
			p.query, p.cursor = "", 0
		} else {
			a.fp.pick = nil
		}
	case "up", "ctrl+p":
		p.cursor = moveCursor(p.cursor, -1, len(vis))
	case "down", "ctrl+n":
		p.cursor = moveCursor(p.cursor, 1, len(vis))
	case "space", " ":
		if p.cursor >= 0 && p.cursor < len(vis) {
			name := strings.ToLower(p.rows[vis[p.cursor]].full)
			p.on[name] = !p.on[name]
		}
	case "/":
		p.typing = true
	case "enter":
		return a.factorySaveRepos()
	}
	a.touch()
	return nil
}

// factorySaveRepos is `enter` on the picker: the ticked repositories are the
// watched ones now, and the note line says how many and how often.
func (a *app) factorySaveRepos() tea.Cmd {
	list := a.fp.pick.watching()
	a.fp.act.doing = "saving…"
	return a.factoryDo(func(s factory.Seam) error { return s.SetRepos(list) }, func(err error) {
		if err != nil {
			return
		}
		a.fp.pick = nil
		a.factorySay(factoryWatchingWords(len(list)))
	})
}

// factoryWatchingWords is the picker's receipt.
func factoryWatchingWords(n int) string {
	if n == 0 {
		return "watching no repositories · the floor keeps what chat and n bring"
	}
	return "watching " + itoa(n) + " " + factoryPlural(n, "repository", "repositories") + rowSep + factoryPollWords
}

// factoryPickerBody is the picker as exactly room rows of exactly width cells:
// its heading with how many are watched at the right, the filter's line, and
// one row per repository, the cursor's on the cursor ground.
//
//	watch · repositories github sees as santoshkumarradha             3 watched
//	/ codeaf
//	[x] agentfield/codeaf                                 private · pushed 2d
//	[ ] santoshkumarradha/notes                                     pushed 3w
func (a *app) factoryPickerBody(width, room int) []placeRow {
	pal, p := a.pal, a.fp.pick
	measure := max(width-factoryMargin, 0)
	heading := "watch" + rowSep + "repositories"
	if p.login != "" {
		heading += " github sees as " + p.login
	}
	count := ""
	if n := len(p.watching()); n > 0 {
		count = itoa(n) + " watched"
	}
	lines := []string{factorySpread(pal.muted(heading), pal.muted(count), measure)}
	switch {
	case p.typing:
		lines = append(lines, pal.accent("/ ")+pal.ink(p.query)+pal.cursor(" ", 1))
	case p.query != "":
		lines = append(lines, pal.dim("/ "+p.query))
	default:
		lines = append(lines, "")
	}
	vis := p.visible()
	left := max(room-len(lines), 0)
	switch {
	case len(vis) == 0 && p.query != "":
		lines = append(lines, pal.dim("nothing matches"))
	case len(vis) == 0:
		lines = append(lines, pal.dim("the repositories github shows this account are listed here"))
	default:
		p.cursor = moveCursor(p.cursor, 0, len(vis))
		p.top = placeTop(p.top, p.cursor, len(vis), left)
		p.shown = 0
		now := a.fp.snap.Now
		if now.IsZero() {
			now = time.Now()
		}
		for i := p.top; i < len(vis) && p.shown < left; i++ {
			r := p.rows[vis[i]]
			on := p.on[strings.ToLower(r.full)]
			mark, name := pal.dim("[ ]"), pal.muted(r.full)
			if on {
				mark, name = pal.ink("[x]"), pal.ink(r.full)
			}
			row := factorySpread(mark+" "+name, pal.dim(factoryPickFacts(r, now)), measure)
			if i == p.cursor {
				row = pal.selected(factoryPad(row, measure), measure)
			}
			lines = append(lines, row)
			p.shown++
		}
	}
	rows := make([]placeRow, room)
	for i := range rows {
		text := ""
		if i < len(lines) && lines[i] != "" {
			text = " " + lines[i]
		}
		rows[i] = placeRow{text: factoryPad(text, width), hit: -1}
	}
	return rows
}

// factoryPickFacts is a repository's facts at the right of its row: private
// when it is, and when it was last pushed to. A repository GitHub did not
// list has neither, and says nothing.
func factoryPickFacts(r factoryPickRow, now time.Time) string {
	var parts []string
	if r.private {
		parts = append(parts, "private")
	}
	if ago := factoryPushedAgo(now, r.pushed); ago != "" {
		parts = append(parts, "pushed "+ago)
	}
	return strings.Join(parts, rowSep)
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

// factoryPickerHint is the hint line while the picker stands.
func (a *app) factoryPickerHint() string {
	if a.fp.pick.typing {
		return "type to filter · enter keep · esc clear"
	}
	out := "esc cancel"
	if a.fp.pick.query != "" {
		out = "esc clear"
	}
	return "↑↓ walk · space watch · / filter · enter save · " + out
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
	return "codeaf does not know where " + p.repo + " is checked out"
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
