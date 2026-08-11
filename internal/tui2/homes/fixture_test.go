package homes

import (
	"strconv"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The fixtures every test in this package draws from. One state, filled at
// every extreme the renderers branch on — a let-go belief, a proposed charter,
// a probationary one, a failed service with a truncated log — so a sweep that
// walks widths also walks the branches.

var fixedNow = time.Date(2026, 8, 10, 21, 8, 0, 0, time.UTC)

func rich() State {
	return State{
		Expanded: true,
		Now:      fixedNow,
		Notebook: Notebook{
			Total:     500,
			AtCeiling: true,
			Beliefs: []Belief{
				{
					ID: "41", Body: "santosh prefers replies in Simplified Technical English",
					Scope: "user", Kind: "preference", Trust: "strong",
					Learned: fixedNow.Add(-72 * time.Hour), Uses: 12, HasUses: true,
					Evidence: []string{"job-3", "#1180"},
				},
				{
					ID: "40", Body: "the CJK filename test on this box is a Linux path-length limit,\nnot a renderer bug",
					Scope: "repo:/home/santosh/src/aforge-v2", Kind: "lesson", Trust: "steady",
					Learned: fixedNow.Add(-20 * time.Minute),
				},
				{
					ID: "12", Body: "the head answers every question itself",
					Scope: "env", Kind: "fact", Trust: "tentative", Retired: true,
					Learned: fixedNow.Add(-900 * time.Hour),
				},
				{
					ID: "9", Body: "maybe the compositor owns the interruption budget",
					Scope: "user", Kind: "fact", Provisional: true,
				},
			},
		},
		Self: Self{
			Today: Today{SpendUSD: 8.65, HasSpend: true, Learned: 3, Practiced: 42 * time.Minute},
			Routes: []Route{
				{ID: RouteCrafts, Count: 2, HasCount: true, Items: []Item{
					{ID: "c1", Name: "wisp-parity", Detail: "compile, run, judge, report", Note: "9 runs",
						Path: "/home/santosh/.aforge/crafts/wisp-parity"},
					{ID: "c2", Name: "release-notes", Detail: "", Note: "2 runs"},
				}},
				{ID: RouteCompetence, Count: 1, HasCount: true, Items: []Item{
					{ID: "s1", Name: "repo:aforge-v2", Detail: "strong · 41 runs · 2 failures", Inert: true},
				}},
				{ID: RouteBeliefs, Count: 500, HasCount: true, AtCeiling: true},
				{ID: RouteSkills, Count: 0, HasCount: true},
				{ID: RouteWatches, Count: 2, HasCount: true, Items: []Item{
					{ID: "ch1", Name: "keep chat-v2 green", Detail: "every weekday at 9am", Life: LifeWorking},
					{ID: "ch2", Name: "watch the spend", Needs: true, Detail: "waiting to be stood up"},
				}},
				{ID: RouteServices, Count: 1, HasCount: true},
				{ID: RoutePractice, Items: []Item{
					{ID: "p1", Name: "practice: renderer widths", Detail: "learned 2", Note: "18m", Life: LifeSettled},
				}},
				{ID: RouteDials, Items: []Item{
					{ID: "d1", Name: "demand vs curiosity", Note: "80%", Inert: true},
					{ID: "d2", Name: "propose new skills", Note: "on", Inert: true},
				}},
			},
		},
		Standing: Standing{
			TenureAt: 3,
			Charters: []Charter{
				{
					ID: "ch1", Invariant: "keep chat-v2 green", Cadence: "every weekday at 9am",
					State: CharterActive, Probation: true, Greens: 2,
					LastLine:  "make check was green; nothing to do",
					LastFired: fixedNow.Add(-26 * time.Hour), NextDue: fixedNow.Add(11 * time.Hour),
					Today: 1, CostPerRun: 0.0017, HasCost: true,
					Verbs: []Verb{
						{ID: "charter.pause", Label: "pause", Key: "p"},
						{ID: "charter.probation", Label: "probation", Key: "b"},
						{ID: "charter.cadence", Label: "cadence", Key: "c"},
						{ID: "charter.retire", Label: "retire", Key: "r"},
					},
				},
				{
					ID: "ch2", Invariant: "watch the spend and tell me before it doubles",
					Cadence: "every hour", State: CharterProposed,
					Verbs: []Verb{
						{ID: "charter.pause", Label: "pause", Key: "p", Disabled: "visitor window — only the resident may act"},
						{ID: "charter.retire", Label: "retire", Key: "r", Disabled: "visitor window — only the resident may act"},
					},
				},
				{ID: "ch3", Invariant: "nightly backup", Cadence: "at 03:00", State: CharterRetired},
			},
		},
		Services: Services{
			Services: []Service{
				{
					ID: "sv1", Name: "vite", Command: "npm run dev -- --host", Health: ":5173",
					Life: LifeWorking, AutoRestart: true, Restarts: 3,
					Since: fixedNow.Add(-3 * time.Hour), LogPath: "/home/santosh/.aforge/logs/vite.log",
					Log:     []string{"ready in 412 ms", "hmr update /src/App.tsx", "\x1b[31mwarn\x1b[0m stale chunk"},
					Dropped: 4812,
					Verbs: []Verb{
						{ID: "service.stop", Label: "stop", Key: "s"},
						{ID: "service.restart", Label: "restart", Key: "r"},
						{ID: "service.auto-restart", Label: "auto-restart", Key: "a"},
					},
				},
				{
					ID: "sv2", Name: "pg", Command: "postgres -D /var/lib/pg", Life: LifeFailed,
					Restarts: 11, LogPath: "/home/santosh/.aforge/logs/pg.log",
				},
				{ID: "sv3", Name: "quiet", Life: LifePaused},
			},
		},
	}
}

// hostile is the same shape filled with everything a renderer must survive:
// control bytes, a lone escape, newlines inside a name, an enormous count, a
// zero clock and a body of nothing but spaces.
func hostile() State {
	return State{
		Expanded: true,
		Visitor:  "visitor window — only the resident may act",
		Notebook: Notebook{
			Total: 1 << 30,
			Beliefs: []Belief{
				{ID: "1", Body: "\x1b[2J\x1b[Hcleared your screen", Scope: "\x07bell", Trust: "\n"},
				{ID: "2", Body: "   ", Kind: "\x1b]0;title\x07"},
				{ID: "3", Body: "a\nb\nc", Retired: true, Provisional: true, Evidence: []string{"\x1b[1m"}},
			},
		},
		Self: Self{Routes: []Route{
			{ID: RouteCrafts, Count: -4, HasCount: true, Items: []Item{
				{Name: "\x1b[31m", Detail: "\x00\x00", Note: "\r\n", Path: "\x1b[H/tmp/x"},
			}},
		}},
		Standing: Standing{TenureAt: -1, Charters: []Charter{
			{ID: "", Invariant: "\x1b[5m", Cadence: "\x1b[?25l", State: CharterState(200), Probation: true, Greens: -3,
				Verbs: []Verb{{Label: "\x1b[0m", Key: "\x1b"}, {Label: ""}}},
		}},
		Services: Services{Services: []Service{
			{ID: "x", Name: "\x1b[2J", Command: "\x00", Life: Lifecycle(200), Dropped: -1,
				Log: []string{"\x1b[H", "", "\x1b]8;;http://x\x07link\x1b]8;;\x07"}},
		}},
	}
}

// wide builds a state with many rows, so a sweep meets the height budget from
// above as well as from below.
func wide(n int) State {
	s := State{Expanded: true, Now: fixedNow}
	for i := 0; i < n; i++ {
		id := strconv.Itoa(i)
		s.Notebook.Beliefs = append(s.Notebook.Beliefs, Belief{ID: id, Body: "belief " + id, Scope: "user"})
		s.Standing.Charters = append(s.Standing.Charters, Charter{ID: id, Invariant: "charter " + id, State: CharterActive})
		s.Services.Services = append(s.Services.Services, Service{ID: id, Name: "svc" + id, Life: LifeWorking})
	}
	s.Notebook.Total = n
	return s
}

// stylers are the painting configurations every sweep crosses: no styler at
// all, every profile, both focus states, and both glyph tiers.
func stylers() map[string]*tokens.Styler {
	out := map[string]*tokens.Styler{"nil": nil}
	for _, p := range []tokens.Profile{tokens.NoColor, tokens.ANSI16, tokens.ANSI256, tokens.TrueColor} {
		for _, f := range []tokens.Focus{tokens.FocusNormal, tokens.FocusDimmed} {
			for _, g := range []tokens.GlyphSet{tokens.Plain, tokens.NerdFont} {
				out[p.String()+"/"+focusName(f)+"/"+g.String()] = tokens.NewStylerIn(p, f, g)
			}
		}
	}
	return out
}

func focusName(f tokens.Focus) string {
	if f == tokens.FocusDimmed {
		return "dim"
	}
	return "normal"
}

// selections enumerates every row every home can be scoped to, plus the
// surface row and one id that is not in the state at all.
func selections(s State) []Selection {
	out := []Selection{}
	for _, h := range All() {
		out = append(out, Selection{Home: h}, Selection{Home: h, Row: "nope"})
		for _, row := range Scope(s, h).Rows[1:] {
			out = append(out, Selection{Home: h, Row: row.ID})
		}
	}
	out = append(out, Selection{Home: HomeNone}, Selection{Home: Home(200)})
	return out
}
