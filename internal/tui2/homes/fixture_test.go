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
					Evidence: []Evidence{
						{Name: "ship the pricing page", Room: "node-3", When: fixedNow.Add(-73 * time.Hour)},
						{When: fixedNow.Add(-80 * time.Hour)},
					},
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
				{
					ID: "7", Body: "shorter commit lines", Kind: "preference",
					Class: BeliefTaste, Status: "forming", Channel: ChannelInferred,
					Learned: fixedNow.Add(-40 * time.Hour),
				},
				{
					ID: "6", Body: "you usually accept first drafts", Kind: "trait",
					Class: BeliefTrait, Samples: 14, HasSamples: true,
					Learned: fixedNow.Add(-25 * time.Hour),
				},
				{
					ID: "5", Body: "run the migration before the seed script", Kind: "playbook",
					Class: BeliefPlaybook, Scope: "repo:/home/santosh/src/aforge-v2",
					Trust: "steady", Channel: ChannelDistilled,
					Learned: fixedNow.Add(-200 * time.Hour),
				},
			},
		},
		Knowhow: Knowhow{
			Crafts: []Craft{
				{
					ID: "release-notes", Name: "release-notes",
					Description: "collect merged PRs, draft the notes, verify links, deliver",
					Proved:      4, Against: 1, HasRecord: true,
					CostPerRun: 0.31, HasCost: true, Version: 3,
					Updated: fixedNow.Add(-8 * 24 * time.Hour),
					Dir:     "/home/santosh/.aforge/craft/workflows",
					Ceilings: CraftCeilings{
						CostUSD: 0.50, HasCost: true, WallClock: 10 * time.Minute,
					},
					Steps: []CraftStep{
						{Brief: "gather merged PRs since last tag"},
						{Brief: "draft the notes", Needs: []string{"1"}, Model: "sonnet"},
						{Brief: "check every link resolves", Verify: true, Skill: "linkcheck"},
						{Brief: "deliver", Needs: []string{"2", "3"}},
					},
					History: []CraftVersion{
						{Version: 3, Subject: "tightened the link check", When: fixedNow.Add(-48 * time.Hour)},
						{Version: 2, Subject: "added the verify step", When: fixedNow.Add(-144 * time.Hour)},
						{Version: 1, Subject: "forged from \"ship 0.4\"", When: fixedNow.Add(-216 * time.Hour)},
					},
					Verbs: []Verb{
						{ID: "craft.run", Label: "run", Key: "r"},
						{ID: "craft.revert", Label: "revert", Key: "v"},
						{ID: "craft.retire", Label: "retire", Key: "x"},
					},
				},
				{
					ID: "fetch-pr-context", Name: "fetch-pr-context",
					Description: "pull the PR, its checks and its review threads",
					Version:     1, Updated: fixedNow.Add(-26 * time.Hour),
				},
			},
			Skills: []Skill{
				{
					ID: "sk1", Name: "imgshrink", Body: "shrink a PNG without touching its palette",
					Path: "/home/santosh/.aforge/skills/imgshrink/run.sh",
					Uses: 11, HasUses: true, Learned: fixedNow.Add(-21 * 24 * time.Hour),
					Verbs: []Verb{{ID: "skill.retire", Label: "retire", Key: "x"}},
				},
				{
					ID: "sk2", Name: "pdfsplit", Body: "split a PDF on its bookmarks",
					Retired: true, Note: "superseded by the pdf tool the workspace ships",
					Learned: fixedNow.Add(-90 * 24 * time.Hour),
				},
			},
		},
		Practice: Practice{
			Today: Today{SpendUSD: 8.65, HasSpend: true, Learned: 3, Practiced: 42 * time.Minute},
			Competence: Competence{
				Strongest: "repo:aforge-v2", Frontier: "tool:docker",
			},
			Questions: []Question{
				{
					ID: "q1", Body: "how flaky is the e2e suite", Scope: "repo:/home/santosh/src/aforge-v2",
					Life: QuestionPracticing, Runs: 2, CostUSD: 0.12, HasCost: true,
					Asked: fixedNow.Add(-70 * time.Hour),
					Attempts: []Attempt{
						{CostUSD: 0.04, HasCost: true, Delta: -0.12, HasDelta: true, When: fixedNow.Add(-60 * time.Hour)},
						{CostUSD: 0.08, HasCost: true, When: fixedNow.Add(-2 * time.Hour)},
					},
				},
				{
					ID: "q2", Body: "uv beats pip in this repo", Life: QuestionResolved,
					Runs: 1, Asked: fixedNow.Add(-96 * time.Hour),
					Note: "settled: uv, on this machine's python",
				},
				{ID: "q3", Body: "why does the CJK test flap", Life: QuestionAsked},
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
				{ID: "3", Body: "a\nb\nc", Retired: true, Provisional: true,
					Evidence: []Evidence{{Name: "\x1b[1m", Room: "\x1b[2J"}, {}}},
				{ID: "4", Body: "\x1b[?25l", Class: BeliefTaste, Status: "\x00", Channel: BeliefChannel(200)},
				{ID: "5", Body: "\r\r\r", Class: BeliefTrait, Samples: -3, HasSamples: true},
				{ID: "6", Body: "\x1b]8;;http://x\x07", Class: BeliefClass(200)},
			},
		},
		Knowhow: Knowhow{
			Crafts: []Craft{{
				ID: "", Name: "\x1b[2J", Description: "a\nb", Proved: -2, Against: -1, HasRecord: true,
				CostPerRun: -3, HasCost: true, Version: -9,
				Dir:      "\x1b[H/tmp/x",
				Ceilings: CraftCeilings{CostUSD: -1, HasCost: true, WallClock: -time.Hour},
				Steps: []CraftStep{
					{Brief: "\x00", Needs: []string{"\x1b[1m", ""}, Model: "\r", Skill: "\n", Verify: true},
				},
				History: []CraftVersion{{Version: -1, Subject: "\x07", When: time.Time{}}},
				Verbs:   []Verb{{Label: "\x1b[0m", Key: "\x1b"}, {Label: ""}},
			}},
			Skills: []Skill{{
				ID: "s", Name: "\x1b]0;title\x07", Body: "   ", Path: "\x00/x",
				Uses: -4, HasUses: true, Retired: true, Note: "\n\n",
			}},
		},
		Practice: Practice{
			Today:      Today{SpendUSD: -1, HasSpend: true, Learned: -2, Practiced: -time.Hour},
			Competence: Competence{Strongest: "\x1b[5m", Frontier: "\x00"},
			Questions: []Question{{
				ID: "q", Body: "a\nb\nc", Scope: "\x1b[2J", Life: QuestionLife(200),
				Runs: -1, CostUSD: -2, HasCost: true, Note: "\x07",
				Attempts: []Attempt{{CostUSD: -1, HasCost: true, Delta: -99999.5, HasDelta: true}},
			}},
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
		s.Knowhow.Crafts = append(s.Knowhow.Crafts, Craft{ID: id, Name: "craft" + id, Version: i + 1})
		s.Knowhow.Skills = append(s.Knowhow.Skills, Skill{ID: id, Name: "tool" + id})
		s.Practice.Questions = append(s.Practice.Questions, Question{ID: id, Body: "gap " + id})
		s.Standing.Charters = append(s.Standing.Charters, Charter{ID: id, Invariant: "charter " + id, State: CharterActive})
		s.Services.Services = append(s.Services.Services, Service{ID: id, Name: "svc" + id, Life: LifeWorking})
	}
	s.Notebook.Total = n
	return s
}

// pageRows is every row id the NOTEBOOK PAGE draws, in draw order. It is the
// drill's own enumeration: the rail's [selections] walks the four rooms, and
// after the split those are no longer the same set.
func pageRows(s State) []string {
	out := []string{}
	for i := range s.Notebook.Beliefs {
		out = append(out, BeliefRowPrefix+s.Notebook.Beliefs[i].ID)
	}
	for i := range s.Knowhow.Crafts {
		out = append(out, CraftRowPrefix+s.Knowhow.Crafts[i].ID)
	}
	for i := range s.Knowhow.Skills {
		out = append(out, SkillRowPrefix+s.Knowhow.Skills[i].ID)
	}
	for i := range s.Practice.Questions {
		out = append(out, QuestionRowPrefix+s.Practice.Questions[i].ID)
	}
	return out
}

// drillSample is ONE row of each kind, which is what a width sweep needs from
// the drill: the four bodies are four renderers, and the fortieth belief walks
// exactly the same code as the first. The full set is walked where it is cheap
// (the id sweep, at one width).
func drillSample(s State) []string {
	out := []string{""}
	if len(s.Notebook.Beliefs) > 0 {
		out = append(out, BeliefRowPrefix+s.Notebook.Beliefs[0].ID)
	}
	if len(s.Knowhow.Crafts) > 0 {
		out = append(out, CraftRowPrefix+s.Knowhow.Crafts[0].ID)
	}
	if len(s.Knowhow.Skills) > 0 {
		out = append(out, SkillRowPrefix+s.Knowhow.Skills[0].ID)
	}
	if len(s.Practice.Questions) > 0 {
		out = append(out, QuestionRowPrefix+s.Practice.Questions[0].ID)
	}
	return out
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
