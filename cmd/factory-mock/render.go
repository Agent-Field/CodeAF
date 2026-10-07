package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func (a *app) p(text string, t tokens.Token) string { return a.st.PaintToken(text, t) }
func (a *app) dim(text string) string               { return a.p(text, tokens.TextTertiary) }
func (a *app) mid(text string) string               { return a.p(text, tokens.TextSecondary) }
func (a *app) hi(text string) string                { return a.p(text, tokens.TextPrimary) }

func (a *app) hair(n int) string {
	if n <= 0 {
		return ""
	}
	return a.dim(strings.Repeat(tokens.GlyphFrameEdge, n))
}

// header draws a dim section title with a hairline out to the width:
// NEEDS YOU · 1 ─────────────
func (a *app) header(title string, note string) string {
	t := strings.ToUpper(title)
	if note != "" {
		t += " " + tokens.GlyphSeparator + " " + note
	}
	left := a.dim(t) + " "
	return left + a.hair(a.width-width(left)-2)
}

func (a *app) band(row string) string {
	return a.st.PaintRowOn(pad(fit(row, a.width-1), a.width-1), tokens.Band)
}

func (a *app) View() tea.View {
	var lines []string
	lines = append(lines, a.topLine())
	body := a.height - 4
	if body < 5 {
		body = 5
	}
	var content []string
	switch a.scr {
	case scFloor:
		content = a.floor(body)
	case scCard:
		content = a.peek(body)
	case scStream:
		content = a.stream(body)
	case scSignoff:
		content = a.signoff()
	case scHelp:
		content = a.help()
	}
	if a.habit != nil {
		content = append(append(content, ""), a.habitPrompt()...)
	}
	for len(content) < body {
		content = append(content, "")
	}
	if len(content) > body {
		content = content[:body]
	}
	for _, l := range content {
		lines = append(lines, fit(l, a.width-1))
	}
	lines = append(lines, a.hugBar(), a.hugInput(), a.toastLine())
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}

func (a *app) topLine() string {
	tab := a.p(tokens.GlyphStepDone, tokens.Cyan) + " " + a.hi("factory")
	var tabs []string
	// Streams you walked into sit beside the tab like conversations do.
	if a.cur != nil {
		g := a.dim(tokens.GlyphSeparator)
		switch a.cur.State {
		case StRunning:
			g = a.p(tokens.GlyphWorking, tokens.Cyan)
		case StNeedsYou:
			g = a.p(tokens.GlyphNeedsHuman, tokens.Amber)
		case StLanded:
			g = a.p(tokens.GlyphSettled, tokens.Green)
		}
		tabs = append(tabs, g+" "+a.mid(fit(a.cur.Ref()+" "+a.cur.Title, 28)))
	}
	left := tab + "   " + strings.Join(tabs, "   ")
	sp := fmt.Sprintf("%d×", int(a.w.Speed/(200*time.Millisecond)))
	if a.paused {
		sp = "paused"
	}
	right := a.dim(a.w.Now.Format("Mon 15:04") + " " + tokens.GlyphSeparator + " " + sp)
	return twoSides(left, right, a.width-1)
}

func (a *app) hugBar() string {
	repo := tokens.GlyphHome + " all repos"
	if a.repo >= 0 {
		r := a.w.Repos[a.repo]
		repo = tokens.GlyphHome + " " + r.Name
		if r.Team != "" {
			repo = "◆ to " + r.Team + " manager"
		}
	}
	running := a.w.Count(StRunning)
	left := a.mid(repo) + a.dim(fmt.Sprintf(" %s %d repos %s benches %d/%d", tokens.GlyphSeparator, len(a.w.Repos), tokens.GlyphSeparator, running, a.w.Benches))
	if a.filter != "" {
		left += a.dim(" "+tokens.GlyphSeparator+" ") + a.p(tokens.GlyphFilter+" "+a.filter, tokens.Cyan)
	}
	rail := a.p(money(a.w.Daily), tokens.Green) + a.dim(fmt.Sprintf(" / $%.0f today", a.w.Rail))
	if a.w.Daily == 0 {
		rail = a.dim(fmt.Sprintf("$— / $%.0f today", a.w.Rail))
	}
	row := twoSides(left, rail, a.width-1)
	return a.st.PaintRowOn(pad(row, a.width-1), tokens.HugGroundBar)
}

func (a *app) hugInput() string {
	var row string
	if a.typing != "" {
		label := map[string]string{"filter": tokens.GlyphFilter + " filter", "compose": tokens.GlyphPromptChat + " new work", "steer": tokens.GlyphPromptSteer + " steer", "answer": tokens.GlyphReplyIn + " answer", "sendback": tokens.GlyphReplyIn + " send back", "words": tokens.GlyphPromptChat + " in words", "ask": tokens.GlyphPromptChat + " ask", "step": "+ step"}[a.typing]
		hint := ""
		switch a.typing {
		case "compose":
			hint = "   e.g. “fix the spend ledger double count, two review rounds, security, $8, plan first”"
		case "filter":
			hint = "   e.g. risky · cheap bugs · strangers · spend · ui prs"
		case "words":
			hint = "   e.g. “review with kimi, three rounds, don't touch auth”"
		case "step":
			hint = "   e.g. “after review, make the code neater” · “before proof, screenshot the page”"
		case "sendback":
			hint = "   empty = “prove " + firstFail(a.cur) + "”"
		}
		row = a.p(label, tokens.Cyan) + " " + a.hi(a.input) + a.p(tokens.GlyphHugEdge, tokens.Cyan) + a.dim(hint)
	} else {
		var keys string
		switch a.scr {
		case scFloor:
			keys = "enter open · space mark · L launch · p plan first · r run · a ask author · d hide · n new · / filter · [ ] repo · A backlog · S sleep 8h · > < speed · ? keys"
		case scCard:
			keys = "enter go · 1-9 steps · s add step · t gate · m M N models · + - rounds · c cap · x security · w words · g github · a ask author · b bank · esc"
			if a.cur != nil && a.cur.State != StNew && a.cur.State != StDismissed {
				keys = "enter walk in · s steer · y n a answer · p pause · x stop · m reviewer · esc floor"
			}
		case scStream:
			keys = "s steer · y n answer · a answer in words · p pause · x stop · m reviewer · k j scroll · esc floor"
		case scSignoff:
			keys = "enter default · a approve anyway · c send back · o re-verify · d diff · s stream · esc"
		default:
			keys = "any key returns"
		}
		row = a.dim(tokens.GlyphPromptChat+" ") + a.dim(keys)
	}
	return a.st.PaintRowOn(pad(fit(row, a.width-1), a.width-1), tokens.HugGroundInput)
}

func (a *app) toastLine() string {
	if a.toast != "" && time.Since(a.toastAt) < 6*time.Second {
		return " " + a.p(a.toast, tokens.Amber)
	}
	return ""
}

// ---- floor

func (a *app) shiftBlock() []string {
	s := a.w.Shift
	since := a.w.Now.Sub(s.Since)
	var out []string
	out = append(out, a.header("◆ handover", fmt.Sprintf("since %s %s %s %s %s", s.Since.Format("15:04"), tokens.GlyphSeparator, dur(since), tokens.GlyphSeparator, money(s.Spent))))
	ship := a.p(tokens.GlyphSettled, tokens.Green) + " " + a.hi(fmt.Sprintf("%d shipped", s.Shipped))
	if len(s.Shipping) > 0 {
		n := s.Shipping
		if len(n) > 4 {
			n = n[:4]
		}
		ship += a.dim(" " + strings.Join(n, " "))
	}
	ship += a.dim(fmt.Sprintf("  %s  %d arrived  %s  %d questions handled", tokens.GlyphSeparator, s.Arrived, tokens.GlyphSeparator, s.Handled))
	waiting := a.w.Count(StNeedsYou)
	var ask string
	if waiting > 0 {
		ask = a.p(tokens.GlyphNeedsHuman, tokens.Amber) + " " + a.p(fmt.Sprintf("%d waiting on you", waiting), tokens.Amber)
	} else {
		ask = a.dim("nothing waits on you")
	}
	// The hour strip: the last 24 hours of phase completions, now at the right.
	var hs []int
	h := a.w.Now.Hour()
	for i := 23; i >= 0; i-- {
		hs = append(hs, s.Hours[(h-i+24)%24])
	}
	mx := 1
	for _, x := range hs {
		if x > mx {
			mx = x
		}
	}
	for i := range hs {
		hs[i] = hs[i] * 7 / mx
	}
	strip := a.dim("24h ") + a.p(spark(hs), tokens.Cyan)
	out = append(out, "  "+ship, "  "+twoSides(ask, strip, a.width-3), "")
	return out
}

func (a *app) floor(body int) []string {
	a.buildRows()
	top := a.shiftBlock()
	var rows []string
	for i, r := range a.rows {
		var line string
		switch r.kind {
		case rowHeader:
			line = a.header(r.section, r.text)
		case rowBlank:
			line = ""
		case rowText:
			line = "  " + a.dim(r.text)
		case rowItem:
			line = a.itemRow(r.item, r.section)
			if i == a.cursor {
				line = a.band(line)
			}
		}
		rows = append(rows, line)
	}
	avail := body - len(top)
	if avail < 3 {
		avail = 3
	}
	// Keep the cursor on screen.
	if a.cursor < a.scroll {
		a.scroll = a.cursor
	}
	if a.cursor >= a.scroll+avail {
		a.scroll = a.cursor - avail + 1
	}
	if a.scroll > len(rows)-avail {
		a.scroll = max(0, len(rows)-avail)
	}
	if a.scroll < 0 {
		a.scroll = 0
	}
	end := min(len(rows), a.scroll+avail)
	out := append(top, rows[a.scroll:end]...)
	if end < len(rows) {
		out[len(out)-1] = a.dim(fmt.Sprintf("  %s %d more below", tokens.GlyphTruncated, len(rows)-end))
	}
	return out
}

func short(r *Repo) string {
	if i := strings.Index(r.Name, "/"); i >= 0 {
		return r.Name[i+1:]
	}
	return r.Name
}

func (a *app) phaseStrip(s *Stream, long bool) string {
	var b []string
	for _, ph := range s.Phases {
		g := a.dim(tokens.GlyphStepPending)
		switch ph.State {
		case PhDone:
			g = a.mid(tokens.GlyphStepDone)
		case PhRunning:
			g = a.p(tokens.GlyphStepRunning, tokens.Cyan)
		case PhFailed:
			g = a.p(tokens.GlyphFailedCell, tokens.Coral)
		case PhWaiting:
			g = a.p(tokens.GlyphNeedsHuman, tokens.Amber)
		}
		if long {
			name := ph.Name
			t := a.dim(name)
			if ph.State == PhRunning {
				t = a.hi(name)
				if ph.Left > 0 {
					t += a.dim(" " + dur(ph.Left))
				}
			} else if ph.State == PhDone {
				t = a.mid(name)
			} else if ph.State == PhWaiting {
				t = a.p(name, tokens.Amber)
			}
			b = append(b, g+" "+t)
		} else {
			b = append(b, g)
		}
	}
	if long {
		return strings.Join(b, "  ")
	}
	return strings.Join(b, "")
}

func (a *app) itemRow(it *Item, section string) string {
	w := a.width - 1
	sep := a.dim(" " + tokens.GlyphSeparator + " ")
	repo := a.dim(short(it.Repo))
	ref := a.mid(padLeft(it.Ref(), 5))
	title := a.hi(it.Title)
	switch it.State {
	case StNeedsYou:
		lead := a.p(tokens.GlyphNeedsHuman, tokens.Amber)
		right := a.dim("[y/n]  " + ago(a.w.Now, it.Changed))
		left := lead + " " + ref + " " + fit(title, 30) + "  " + repo + sep + a.p(it.Question, tokens.Amber)
		return twoSides(left, right, w)
	case StRunning:
		s := it.Stream
		cur := ""
		if s.Cur < len(s.Phases) {
			ph := s.Phases[s.Cur]
			cur = ph.Name
			if ph.Left > 0 {
				cur += " " + dur(ph.Left)
			}
			if ph.Note != "" && strings.Contains(ph.Note, "findings") {
				cur += " · " + ph.Note
			}
		}
		lead := a.p(tokens.GlyphWorking, tokens.Cyan)
		if s.Paused {
			lead = a.mid(tokens.GlyphPaused)
		}
		left := lead + " " + ref + " " + pad(fit(title, 34), 34) + "  " + pad(repo, 11) + " " + a.phaseStrip(s, false) + "  " + a.mid(cur)
		right := a.p(money(s.Spent), tokens.Green) + a.dim(fmt.Sprintf("/$%.0f  ", it.Order.Cap)) + a.p(spark(s.Activity[len(s.Activity)-8:]), tokens.Cyan) + a.dim("  "+dur(a.w.Now.Sub(s.Started)))
		return twoSides(left, right, w)
	case StQueued:
		left := a.dim(tokens.GlyphQueued) + " " + ref + " " + pad(fit(title, 34), 34) + "  " + pad(repo, 11) + " " + a.dim("queued "+tokens.GlyphSeparator+" benches full")
		right := a.dim(fmt.Sprintf("~$%.0f  %s", it.Triage.Est, ago(a.w.Now, it.Changed)))
		return twoSides(left, right, w)
	case StLanded:
		ok, bad := 0, 0
		for _, c := range append(append([]Claim{}, it.Proof...), it.Policy...) {
			if c.OK {
				ok++
			} else {
				bad++
			}
		}
		proof := a.p(fmt.Sprintf("%d%s", ok, tokens.GlyphSettled), tokens.Green)
		if bad > 0 {
			proof += " " + a.p(fmt.Sprintf("%d%s", bad, tokens.GlyphFailed), tokens.Coral)
		}
		lead := a.p(tokens.GlyphSettled, tokens.Green)
		if bad > 0 {
			lead = a.p(tokens.GlyphFailed, tokens.Coral)
		}
		left := lead + " " + ref + " " + pad(fit(title, 34), 34) + "  " + pad(repo, 11) + " proof " + proof + sep + a.mid("sign-off "+tokens.GlyphPointer)
		right := a.p(money(it.Stream.Spent), tokens.Green) + a.dim("  "+ago(a.w.Now, it.Changed))
		return twoSides(left, right, w)
	case StMerged:
		left := a.dim(tokens.GlyphStepDone) + " " + a.dim(padLeft(it.Ref(), 5)) + " " + a.dim(fit(it.Title, 34)) + "  " + repo + sep + a.dim("merged "+it.Changed.Format("15:04"))
		right := a.dim(money(it.Stream.Spent))
		return twoSides(left, right, w)
	}
	// New.
	lead := " "
	switch {
	case it.Marked:
		lead = a.p(tokens.GlyphStepDone, tokens.Cyan)
	case it.Kind == KindCI:
		lead = a.p(tokens.GlyphFailed, tokens.Coral)
	}
	var meta []string
	if it.Kind == KindPR {
		meta = append(meta, "pr", it.Checks)
	} else if it.Kind == KindCI {
		meta = append(meta, "ci red")
	} else {
		meta = append(meta, it.Triage.Type, it.Triage.Size)
	}
	meta = append(meta, fmt.Sprintf("~$%.0f", it.Triage.Est))
	who := it.Author
	if it.Tier == TierStranger {
		who += " (stranger)"
	}
	meta = append(meta, who)
	tags := ""
	if it.Triage.Readiness < 55 && it.Kind == KindIssue {
		tags += " " + a.p("thin", tokens.Amber)
	}
	if it.Triage.DupOf != 0 {
		tags += " " + a.dim(fmt.Sprintf("dup #%d?", it.Triage.DupOf))
	}
	if hasLabel(it, "factory") {
		tags += " " + a.p("factory", tokens.Cyan)
	}
	if it.Origin == OriginChat {
		tags += " " + a.p("from chat "+tokens.GlyphPointer, tokens.Cyan)
	}
	if it.Origin == OriginTerminal {
		if it.Synced {
			tags += " " + a.dim("github "+tokens.GlyphSettled)
		} else {
			tags += " " + a.dim("terminal only")
		}
	}
	left := lead + " " + ref + " " + pad(fit(title, 44), 44) + "  " + pad(repo, 11) + " " + a.dim(strings.Join(meta, " "+tokens.GlyphSeparator+" ")) + tags
	right := a.dim(ago(a.w.Now, it.Created))
	return twoSides(left, right, w)
}

// ---- card

func wrap(s string, w int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		line := ""
		for _, wd := range words {
			if line != "" && width(line)+1+width(wd) > w {
				out = append(out, line)
				line = wd
				continue
			}
			if line == "" {
				line = wd
			} else {
				line += " " + wd
			}
		}
		out = append(out, line)
	}
	return out
}

func (a *app) card() []string {
	it := a.cur
	o := it.Order
	sep := a.dim(" " + tokens.GlyphSeparator + " ")
	var L []string
	src := "github"
	if it.Origin == OriginChat {
		src = "from a chat " + tokens.GlyphPointer + " the room already exists"
	}
	if it.Origin == OriginTerminal {
		src = "terminal only"
		if it.Synced {
			src = "github " + tokens.GlyphSettled
		}
	}
	title := a.mid(it.Ref()) + " " + a.hi(it.Title)
	right := a.dim(strings.Join([]string{it.Repo.Name, it.Kind.String(), it.Author + " (" + it.Tier.String() + ")", ago(a.w.Now, it.Created), src}, " "+tokens.GlyphSeparator+" "))
	L = append(L, twoSides(title, right, a.width-1))
	shown := 0
	for _, l := range wrap(it.Body, a.width-6) {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if shown >= 2 {
			L[len(L)-1] += a.dim(" " + tokens.GlyphEllipsis)
			break
		}
		L = append(L, "  "+a.mid(l))
		shown++
	}
	t := it.Triage
	facts := []string{t.Type, t.Size, t.Area, fmt.Sprintf("ready %d%%", t.Readiness), fmt.Sprintf("~$%.0f", t.Est), "risk " + t.Risk}
	if it.Kind == KindPR {
		facts = []string{"pr", it.Diff, it.Checks, fmt.Sprintf("~$%.0f to review", t.Est)}
	}
	if t.DupOf != 0 {
		facts = append(facts, fmt.Sprintf("maybe a dup of #%d", t.DupOf))
	}
	L = append(L, "  "+a.mid(strings.Join(facts, " "+tokens.GlyphSeparator+" ")))
	L = append(L, "  "+a.dim(tokens.GlyphThought)+" "+a.hi(t.Read))
	if len(t.Questions) > 0 {
		L = append(L, "  "+a.p(tokens.GlyphNeedsHuman, tokens.Amber)+" "+a.p("thin", tokens.Amber)+a.dim(" "+tokens.GlyphSeparator+" it would ask "+it.Author+": ")+a.mid(strings.Join(t.Questions, " / "))+a.dim("   [a] ask, nothing posts without you"))
	}
	if it.Tier == TierStranger {
		L = append(L, "  "+a.p(tokens.GlyphNeedsHuman, tokens.Amber)+" "+a.dim("a stranger's words: the factory may draft, never ship, on these"))
	}
	L = append(L, "")
	chip := func(key, label, val string) string {
		return a.dim(label+" ") + a.hi(val) + a.dim(" ["+key+"]")
	}
	sec := "off"
	if o.Security {
		sec = "on"
	}
	L = append(L, "  "+strings.Join([]string{
		chip("N", "plan", o.PlanModel), chip("m", "write", o.WriteModel), chip("M", "review", fmt.Sprintf("%s ×%d", o.ReviewModel, o.Rounds)) + a.dim(" [+/-]"),
	}, "   "))
	L = append(L, "  "+strings.Join([]string{chip("x", "security", sec), chip("c", "cap", fmt.Sprintf("$%.0f", o.Cap)), chip("t", "gate", o.Gate.String())}, "   "))
	if len(o.Constraints) > 0 {
		L = append(L, "  "+a.dim("constraints ")+a.mid(strings.Join(o.Constraints, sep)))
	}
	// Steps: sentences at moments, toggled by number. No graph.
	for i, st := range o.Steps {
		g := a.dim(tokens.GlyphStepPending)
		txt := a.dim(st.When + ": " + st.Text)
		if st.On {
			g = a.p(tokens.GlyphStepDone, tokens.Cyan)
			txt = a.dim(st.When+": ") + a.mid(st.Text)
		}
		L = append(L, "  "+a.dim(fmt.Sprintf("[%d] ", i+1))+g+" "+txt)
	}
	L = append(L, "  "+a.dim("[s] add a step in words · “after review, make it neater”   [b] bank these steps for "+short(it.Repo)))
	if len(it.Repo.Policy) > 0 {
		L = append(L, "  "+a.dim("must show: "+strings.Join(it.Repo.Policy, " "+tokens.GlyphSeparator+" ")))
	}
	gate := map[Gate]string{GatePlan: "comes back with the plan · nothing is written until you say go", GateShip: "runs to a PR · you sign off on the proof sheet", GateNone: "self-ships when the proof is green · you read the shift report"}[o.Gate]
	L = append(L, "  "+a.p("[enter] go", tokens.Green)+a.dim("  "+tokens.GlyphSeparator+" "+gate))
	L = append(L, "  "+a.dim("[w] chips in words    [g] also open on github    [d] hide    [esc] floor"))
	return L
}

// ---- stream

func (a *app) stream(body int) []string {
	it := a.cur
	s := it.Stream
	var L []string
	if s == nil {
		return []string{"  " + a.dim("not on a bench")}
	}
	lead := a.p(tokens.GlyphWorking, tokens.Cyan)
	switch it.State {
	case StNeedsYou:
		lead = a.p(tokens.GlyphNeedsHuman, tokens.Amber)
	case StLanded:
		lead = a.p(tokens.GlyphSettled, tokens.Green)
	case StMerged:
		lead = a.mid(tokens.GlyphStepDone)
	case StNew:
		lead = a.p(tokens.GlyphStopped, tokens.Coral)
	}
	if s.Paused {
		lead = a.mid(tokens.GlyphPaused)
	}
	elapsed := a.w.Now.Sub(s.Started)
	if !s.Ended.IsZero() {
		elapsed = s.Ended.Sub(s.Started)
	}
	title := lead + " " + a.mid(it.Ref()) + " " + a.hi(it.Title)
	right := a.dim(fmt.Sprintf("%s %s bench %d %s %s %s ", short(it.Repo), tokens.GlyphSeparator, s.Bench, tokens.GlyphSeparator, dur(elapsed), tokens.GlyphSeparator)) + a.p(money(s.Spent), tokens.Green) + a.dim(fmt.Sprintf(" / $%.0f", it.Order.Cap))
	L = append(L, twoSides(title, right, a.width-1))
	L = append(L, "  "+a.phaseStrip(s, true))
	L = append(L, "  "+a.dim("recipe ")+a.dim(it.Order.Recipe()))
	L = append(L, a.hair(a.width-2))
	// The log, tail by default.
	avail := body - len(L) - 1
	if it.State == StNeedsYou {
		avail -= 3
	}
	if avail < 3 {
		avail = 3
	}
	log := s.Log
	top := a.logTop
	if top < 0 || top > len(log)-avail {
		top = max(0, len(log)-avail)
		if a.logTop >= 0 {
			a.logTop = top
		}
	}
	end := min(len(log), top+avail)
	for _, l := range log[top:end] {
		tone := tokens.TextSecondary
		g := tokens.TextTertiary
		switch l.Tone {
		case "ask":
			tone, g = tokens.Amber, tokens.Amber
		case "fail":
			tone, g = tokens.TextPrimary, tokens.Coral
		case "ok":
			g = tokens.Green
		case "said":
			tone = tokens.TextPrimary
			g = tokens.Cyan
		case "write":
			g = tokens.TextSecondary
		}
		L = append(L, "  "+a.dim(l.At.Format("15:04"))+" "+a.p(l.Glyph, g)+" "+a.p(l.Text, tone))
	}
	if it.State == StNeedsYou {
		L = append(L, "", "  "+a.p(tokens.GlyphNeedsHuman+" "+it.Question, tokens.Amber), "  "+a.dim("[y] yes   [n] no   [a] answer in words   "+tokens.GlyphSeparator+"   it waits; the other benches do not"))
	}
	if it.State == StLanded {
		L = append(L, "", "  "+a.p("[enter] the proof sheet", tokens.Green))
	}
	return L
}

// ---- sign-off

func (a *app) signoff() []string {
	it := a.cur
	s := it.Stream
	var L []string
	L = append(L, a.header("sign-off", it.Ref()+" "+tokens.GlyphSeparator+" "+it.Title+" "+tokens.GlyphSeparator+" "+short(it.Repo)))
	claimTitle := "its claims"
	if it.Kind == KindPR {
		claimTitle = "their claims, from the PR body"
	}
	L = append(L, "  "+a.dim(claimTitle)+strings.Repeat(" ", max(1, a.width-40-width(claimTitle)))+a.dim("verified against the build"))
	var failed *Claim
	for i := range it.Proof {
		c := &it.Proof[i]
		g := a.p(tokens.GlyphSettled, tokens.Green)
		txt := a.hi(c.Text)
		ev := a.dim(c.Evidence)
		if !c.OK {
			g = a.p(tokens.GlyphFailed, tokens.Coral)
			txt = a.hi(c.Text) + a.p(" — NOT proven", tokens.Coral)
			ev = a.p(c.Evidence, tokens.Coral)
			if failed == nil {
				failed = c
			}
		}
		if c.Medium == "screenshot" {
			ev += a.p(" [▦ view]", tokens.Cyan)
		}
		L = append(L, "    "+twoSides(g+" "+txt, ev, a.width-6))
	}
	if failed != nil && failed.Medium == "benchmark" {
		L = append(L, "      "+a.dim("main        12ms/frame"), "      "+a.mid("this PR     ")+a.p("52ms/frame", tokens.Coral)+a.dim("  · crosses at the rail-count math · 4 runs, warm"))
	}
	if len(it.Policy) > 0 {
		L = append(L, "", a.header("team policy", "every PR on "+short(it.Repo)+" must show"))
		for _, c := range it.Policy {
			g := a.p(tokens.GlyphSettled, tokens.Green)
			if !c.OK {
				g = a.p(tokens.GlyphFailed, tokens.Coral)
				if failed == nil {
					failed = &c
				}
			}
			L = append(L, "    "+twoSides(g+" "+a.mid(c.Text), a.dim(c.Evidence+" "+tokens.GlyphSeparator+" policy"), a.width-6))
		}
	}
	L = append(L, "")
	diff := it.Diff
	if diff == "" {
		diff = fmt.Sprintf("+%d −%d", 60+s.Findings*7, 12)
	}
	L = append(L, "  "+a.dim(fmt.Sprintf("%s %s %s %s %s %s diff %s — the appendix", dur(s.Ended.Sub(s.Started)), tokens.GlyphSeparator, it.Order.WriteModel+" + "+it.Order.ReviewModel+fmt.Sprintf(" ×%d", it.Order.Rounds), tokens.GlyphSeparator, money(s.Spent), tokens.GlyphSeparator, diff))+a.dim("   [d] open diff"))
	L = append(L, "")
	if failed == nil {
		L = append(L, "  "+a.p("[enter] ship", tokens.Green)+a.dim("   [c] send back   [o] re-verify   [s] the stream"))
	} else {
		L = append(L, "  "+a.p("[enter] send back — “prove "+failed.Text+"”", tokens.Amber)+a.dim("   [a] ship anyway   [o] re-verify   [s] the stream"))
		L = append(L, "  "+a.dim("a "+tokens.GlyphFailed+" makes the blocking action the default key"))
	}
	return L
}

func (a *app) habitPrompt() []string {
	it := a.habit
	return []string{
		"  " + a.p(tokens.GlyphAccentRail, tokens.Cyan) + " " + a.hi("habit forming") + a.dim(fmt.Sprintf(" — %d sign-offs without edits on %s", 3, short(it.Repo))),
		"  " + a.p(tokens.GlyphAccentRail, tokens.Cyan) + " " + a.mid("factory PRs from your own issues self-ship when the proof is green?") + a.dim("  [y] bank it  [n] not yet"),
	}
}

func (a *app) help() []string {
	var L []string
	L = append(L, a.header("keys", ""))
	rows := map[screen][][2]string{
		scFloor: {
			{"j k", "move"}, {"enter", "open the row: a card, a stream, or a proof sheet"}, {"space", "mark · L launches everything marked, benches first, the rest queue"},
			{"p", "plan first: it comes back with the plan before any code"}, {"r", "run: to a PR, you sign off"}, {"a", "ask the author the two questions a thin issue needs"},
			{"y n", "answer a waiting question without opening it"}, {"x", "stop a stream, branch kept"}, {"d", "hide until it changes"},
			{"n", "new work in words · chips lift out of the sentence"}, {"/", "filter in words: risky · cheap bugs · strangers · spend · ui prs"}, {"[ ]", "one repo at a time"}, {"A", "the whole backlog, not just the last three days"},
			{"S", "sleep eight hours and read the shift report"}, {"> <", "faster, slower"}, {"P", "pause the clock"}, {"q", "quit"},
		},
		scCard:    {{"enter", "go, or walk into a running room"}, {"1-9", "toggle a step"}, {"s", "add a step in words: a time word, then what"}, {"b", "bank the steps for every item on this repo"}, {"t", "gate: plan · ship · none"}, {"m M N", "writer · reviewer · planner"}, {"+ -", "review rounds"}, {"c", "cap"}, {"x", "security pass"}, {"w", "say it in words; chips change"}, {"g", "also open it on github"}, {"a", "ask the author"}, {"d", "hide"}},
		scStream:  {{"s", "steer in words; a model name or “two rounds” changes the recipe live"}, {"y n a", "answer the question"}, {"p", "pause this bench"}, {"x", "stop, branch kept"}, {"m", "switch the reviewer from the next round"}, {"k j G", "scroll the log"}},
		scSignoff: {{"enter", "the default: ship when all green, send back when a ✕ is on the sheet"}, {"a", "ship anyway"}, {"c", "send back in words"}, {"o", "re-verify every check"}, {"d", "the diff, as an appendix"}},
	}
	for _, r := range rows[a.helpFor] {
		L = append(L, "  "+a.hi(pad(r[0], 8))+a.mid(r[1]))
	}
	L = append(L, "", "  "+a.dim("the floor never does work · every stream is an ordinary codeaf conversation you can walk into"))
	return L
}

// ---- peek: a sheet over the floor

func (a *app) peek(body int) []string {
	var sheet []string
	it := a.cur
	switch it.State {
	case StNew, StDismissed:
		sheet = a.card()
	case StLanded:
		sheet = a.landedPeek()
	default:
		sheet = a.streamPeek()
	}
	h := len(sheet) + 1
	if h > body-6 {
		h = body - 6
		sheet = sheet[:h-1]
	}
	floor := a.floor(body - h)
	out := append([]string{}, floor...)
	out = append(out, a.st.PaintRowOn(pad(a.hair(a.width-1), a.width-1), tokens.Sheet))
	for _, l := range sheet {
		out = append(out, a.st.PaintRowOn(pad(fit(l, a.width-1), a.width-1), tokens.Sheet))
	}
	return out
}

func (a *app) streamPeek() []string {
	it := a.cur
	s := it.Stream
	var L []string
	if s == nil {
		return []string{"  " + a.dim("queued · a bench frees it")}
	}
	elapsed := a.w.Now.Sub(s.Started)
	if !s.Ended.IsZero() {
		elapsed = s.Ended.Sub(s.Started)
	}
	title := a.mid(it.Ref()) + " " + a.hi(it.Title)
	right := a.dim(fmt.Sprintf("%s %s bench %d %s %s %s ", short(it.Repo), tokens.GlyphSeparator, s.Bench, tokens.GlyphSeparator, dur(elapsed), tokens.GlyphSeparator)) + a.p(money(s.Spent), tokens.Green) + a.dim(fmt.Sprintf(" / $%.0f", it.Order.Cap))
	L = append(L, twoSides(title, right, a.width-1))
	L = append(L, "  "+a.phaseStrip(s, true))
	n := len(s.Log)
	from := max(0, n-5)
	for _, l := range s.Log[from:] {
		g := tokens.TextTertiary
		tone := tokens.TextSecondary
		switch l.Tone {
		case "ask":
			tone, g = tokens.Amber, tokens.Amber
		case "fail":
			g = tokens.Coral
		case "ok":
			g = tokens.Green
		case "said":
			tone, g = tokens.TextPrimary, tokens.Cyan
		}
		L = append(L, "  "+a.dim(l.At.Format("15:04"))+" "+a.p(l.Glyph, g)+" "+a.p(l.Text, tone))
	}
	if it.State == StNeedsYou {
		L = append(L, "  "+a.p("[y] yes   [n] no   [a] in words", tokens.Amber)+a.dim("   "+tokens.GlyphSeparator+"   [enter] walk into the room   [s] steer   [x] stop"))
	} else {
		L = append(L, "  "+a.p("[enter] walk into the room", tokens.Cyan)+a.dim("   [s] steer in words   [p] pause   [x] stop   [m] reviewer   [esc] floor"))
	}
	return L
}

func (a *app) landedPeek() []string {
	it := a.cur
	var L []string
	title := a.mid(it.Ref()) + " " + a.hi(it.Title)
	L = append(L, twoSides(title, a.dim(short(it.Repo)+" "+tokens.GlyphSeparator+" landed "+ago(a.w.Now, it.Changed)+" ago"), a.width-1))
	for _, c := range it.Proof {
		g := a.p(tokens.GlyphSettled, tokens.Green)
		if !c.OK {
			g = a.p(tokens.GlyphFailed, tokens.Coral)
		}
		L = append(L, "  "+twoSides(g+" "+a.mid(c.Text), a.dim(c.Evidence), a.width-4))
	}
	L = append(L, "  "+a.p("[enter] the proof sheet", tokens.Green)+a.dim("   [d] hide   [esc] floor"))
	return L
}
