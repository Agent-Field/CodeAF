package draft

import (
	"errors"
	"strings"
	"testing"
)

// cmd is one verb call; body is what a heredoc would send on stdin.
type cmd struct {
	verb string
	args []string
	body string
}

func c(verb string, args ...string) cmd { return cmd{verb: verb, args: args} }

func (x cmd) withBody(body string) cmd { x.body = body; return x }

// planned is a draft that has an accepted plan and two records, the ground
// every build-phase verb is tried on.
func planned() []cmd {
	return []cmd{
		c("name", "CRM"),
		c("plan data", "deal", "Deal · name, stage"),
		c("plan data", "company", "Company · name"),
		c("plan screen", "pipeline", "Pipeline · deals"),
		c("plan agent", "CRM agent · helps"),
		c("plan ready"),
		c("record", "company", "--title", "name", "--label", "Company"),
		c("field", "company", "name", "text", "--required"),
		c("record", "deal", "--title", "name", "--label", "Deal"),
		c("field", "deal", "name", "text"),
		c("field", "deal", "stage", "enum", "--values", "Lead,Won"),
	}
}

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func play(t *testing.T, s *Store, calls []cmd) {
	t.Helper()
	for _, x := range calls {
		if _, err := s.Append(AuthorBuilder, x.verb, x.args, x.body); err != nil {
			t.Fatalf("setup %s %v: %v", x.verb, x.args, err)
		}
	}
}

const screenFor = `{"$schema":"https://codeaf.dev/schema/screen/1.json","screen":"pipeline","title":"Pipeline","body":{"part":"RecordList","id":"deals","label":"Deals","bind":{"records":"deal"},"props":{"columns":%s}}}`

func screenWith(columns string) string { return strings.Replace(screenFor, "%s", columns, 1) }

const goodAgent = "---\nname: CRM agent\ntools: [records.query]\n---\nYou help.\n"

const goodRun = `run: follow-up
title: Follow up
steps:
  - id: load
    records.query: {type: deal, query: "stage:Lead", limit: 5}
`

func TestVerbsAcceptAndRefuse(t *testing.T) {
	tests := []struct {
		name  string
		setup []cmd
		call  cmd
		line  string // the Entry.Line of an accepted verb
		err   string // the exact text of a refusal, "error: " included
		code  int
	}{
		{name: "name", call: c("name", "CRM", "--about", "pipeline"), line: "Name · CRM"},
		{name: "name too long", call: c("name", strings.Repeat("x", 41)), code: ExitRefused,
			err: "error: the app name must be 1 to 40 characters, got 41 — give a short name such as \"CRM\""},
		{name: "name missing", call: c("name"), code: ExitUsage,
			err: "error: expected 1 argument(s), got 0 — usage: codeaf app name \"<Name>\" [--about \"<one sentence>\"] [--tint <name>]"},
		{name: "unknown flag", call: c("name", "CRM", "--shout"), code: ExitUsage,
			err: "error: unknown flag --shout — usage: codeaf app name \"<Name>\" [--about \"<one sentence>\"] [--tint <name>]"},
		{name: "tint", call: c("tint", "sage"), line: "Tint · sage"},
		{name: "tint unknown", call: c("tint", "x"), code: ExitRefused,
			err: `error: tint "x" is not one of iris, sage, sand, tide, rose, graphite`},
		{name: "name with tint", call: c("name", "CRM", "--tint", "rose"), line: "Name · CRM"},
		{name: "name with bad tint", call: c("name", "CRM", "--tint", "x"), code: ExitRefused,
			err: `error: tint "x" is not one of iris, sage, sand, tide, rose, graphite`},
		{name: "plan data", call: c("plan data", "deal", "Deal · name"), line: "Deal · name"},
		{name: "plan data bad id", call: c("plan data", "Deal", "x"), code: ExitRefused,
			err: `error: plan data id "Deal" must be lowercase letters, digits, - or _, start with a letter, and be at most 48 characters`},
		{name: "plan data empty line", call: c("plan data", "deal", "  "), code: ExitRefused,
			err: "error: a plan line must not be empty — write one plain sentence the owner can read"},
		{name: "plan data duplicate", setup: []cmd{c("plan data", "deal", "a")}, call: c("plan data", "deal", "b"), code: ExitRefused,
			err: `error: plan data "deal" already exists — drop it first with: codeaf app plan drop data deal`},
		{name: "plan drop", setup: []cmd{c("plan data", "deal", "a")}, call: c("plan drop", "data", "deal"), line: "Removed plan data deal"},
		{name: "plan drop missing", call: c("plan drop", "data", "deal"), code: ExitRefused,
			err: `error: plan data "deal" does not exist (have: none yet)`},
		{name: "plan ready lacks", call: c("plan ready"), code: ExitRefused,
			err: `error: the plan is not ready: it needs a name (codeaf app name "<Name>"), at least one data line (plan data <id> "<line>"), at least one screen line (plan screen <id> "<line>"), an agent line (plan agent "<line>")`},
		{name: "plan ready", setup: planned()[:5], call: c("plan ready"), line: "Plan ready"},
		{name: "record unplanned", setup: planned()[:6], call: c("record", "lead"), code: ExitRefused,
			err: `error: record "lead" is not in the plan (data lines: deal, company) — plan it first with: codeaf app plan data lead "<line>", or add --new`},
		{name: "record new", setup: planned()[:6], call: c("record", "lead", "--new", "--label", "Lead"), line: "Lead · record added"},
		{name: "field", setup: planned()[:9], call: c("field", "deal", "name", "text"), line: "Deal · name"},
		{name: "field unknown record", setup: planned(), call: c("field", "lead", "x", "text"), code: ExitRefused,
			err: `error: record "lead" does not exist (records: company, deal) — make it first with: codeaf app record lead`},
		{name: "field ref target", setup: planned(), call: c("field", "deal", "owner", "ref", "--to", "person"), code: ExitRefused,
			err: `error: deal.owner: /to "person" is not a record yet (records: company, deal) — define record person first`},
		{name: "field enum without values", setup: planned(), call: c("field", "deal", "kind", "enum"), code: ExitRefused},
		{name: "field enum default", setup: planned(), call: c("field", "deal", "kind", "enum", "--values", "a,b", "--default", "c"), code: ExitRefused,
			err: `error: deal.kind: /default "c" is not one of its values (values: a, b)`},
		{name: "field currency", setup: planned(), call: c("field", "deal", "n", "number", "--currency", "USD"), code: ExitRefused,
			err: "error: deal.n: /currency only applies to a money field, this one is number"},
		{name: "field computed", setup: planned(), call: c("field", "deal", "x", "number", "--computed", "$now > $now - 7d"), line: "Deal · name, stage, x"},
		{name: "field computed typo", setup: planned(), call: c("field", "deal", "x", "number", "--computed", "stge + 1"), code: ExitRefused},
		{name: "field bad type", setup: planned(), call: c("field", "deal", "n", "numbr"), code: ExitRefused},
		{name: "screen", setup: planned(), call: c("screen", "pipeline").withBody(screenWith(`["name","stage"]`)), line: "Screen · Pipeline"},
		{name: "screen unknown field", setup: planned(), call: c("screen", "pipeline").withBody(screenWith(`["name","stge"]`)), code: ExitRefused,
			err: `error: screens/pipeline.json: /body/props/columns/1: "stge" is not a field of deal (fields: name, stage)`},
		{name: "screen id mismatch", setup: planned(), call: c("screen", "other").withBody(screenWith(`["name"]`)), code: ExitRefused,
			err: `error: screens/other.json: /screen: screen must equal the file name "other" — see: codeaf app schema screen`},
		{name: "screen unknown record", setup: planned()[:6], call: c("screen", "pipeline").withBody(screenWith(`["name"]`)), code: ExitRefused,
			err: `error: screens/pipeline.json: /body/bind/records: "deal" is not a record of this app (records: none yet) — define it first with: codeaf app record deal`},
		{name: "screen needs stdin", setup: planned(), call: c("screen", "pipeline"), code: ExitUsage,
			err: "error: the document must come on stdin, for example with a heredoc — usage: codeaf app screen <id> < screen.json"},
		{name: "run", setup: planned(), call: c("run", "follow-up").withBody(goodRun), line: "Run · Follow up"},
		{name: "run unknown field", setup: planned(), call: c("run", "follow-up").withBody(strings.Replace(goodRun, "stage:Lead", "stge:Lead", 1)), code: ExitRefused},
		{name: "run unknown record", setup: planned(), call: c("run", "follow-up").withBody(strings.Replace(goodRun, "type: deal", "type: dael", 1)), code: ExitRefused,
			err: `error: runs/follow-up.yaml: /steps/0/records.query/type: unknown record type "dael" (records: company, deal)`},
		{name: "agent", setup: planned(), call: c("agent").withBody(goodAgent), line: "Agent · CRM agent"},
		{name: "agent front matter", setup: planned(), call: c("agent").withBody("just text"), code: ExitRefused,
			err: "error: agent.md must start with --- front matter — see: codeaf app schema agent"},
		{name: "connect", call: c("connect", "gmail", "--can", "read,send", "--approve", "send"), line: "Gmail · read, send (send asks you first)"},
		{name: "connect approve", call: c("connect", "gmail", "--can", "read", "--approve", "send"), code: ExitRefused,
			err: `error: connect gmail: --approve "send" is not in --can (can: read) — approve only verbs the app may use`},
		{name: "trigger", setup: append(planned(), c("run", "follow-up").withBody(goodRun)),
			call: c("trigger", "mondays", "--run", "follow-up", "--schedule", "0 9 * * 1"), line: "Trigger · mondays starts follow-up"},
		{name: "trigger cron", setup: append(planned(), c("run", "follow-up").withBody(goodRun)),
			call: c("trigger", "mondays", "--run", "follow-up", "--schedule", "0 99 * * 1"), code: ExitRefused,
			err: `error: trigger mondays: /cron "0 99 * * 1": the hour field "99" is invalid: "99" must be a number from 0 to 23`},
		{name: "trigger run missing", setup: planned(), call: c("trigger", "mondays", "--run", "nope", "--schedule", "0 9 * * 1"), code: ExitRefused,
			err: `error: trigger mondays: /run "nope" is not a run yet (runs: none yet) — make it first with: codeaf app run nope < run.yaml`},
		{name: "command", setup: append(planned(), c("run", "follow-up").withBody(goodRun)),
			call: c("command", "nudge", "--label", "Nudge", "--for", "deal", "--run", "follow-up", "--param", `deals=$selection.deals.ids`), line: "Command · Nudge"},
		{name: "command do", setup: planned(),
			call: c("command", "won", "--label", "Won", "--for", "deal").withBody(`[{"records.update":{"ids":"$selection.deals.ids","set":{"stge":"Won"}}}]`), code: ExitRefused,
			err: `error: command won: /do/0/records.update/set/stge: "stge" is not a field of deal (fields: name, stage)`},
		{name: "command needs run or do", setup: planned(), call: c("command", "x", "--label", "X"), code: ExitRefused},
		{name: "constant", setup: planned(), call: c("constant", "deal", "weights").withBody(`{"Lead":0.1}`), line: "Deal · constant weights"},
		{name: "drop", setup: planned(), call: c("drop", "field", "deal.stage"), line: "Removed field deal.stage"},
		{name: "drop missing", setup: planned(), call: c("drop", "screen", "pipeline"), code: ExitRefused,
			err: `error: there is no screen "pipeline" to drop (screen: none yet)`},
		{name: "workers cannot write the phase", call: c("phase", "done"), code: ExitUsage, err: `error: unknown verb "phase"`},
		{name: "unknown verb", call: c("frobnicate"), code: ExitUsage, err: `error: unknown verb "frobnicate"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := openTemp(t)
			play(t, s, tc.setup)
			before, _ := s.Entries(0)
			e, err := s.Append(AuthorBuilder, tc.call.verb, tc.call.args, tc.call.body)
			after, _ := s.Entries(0)
			if tc.code == 0 {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				if e.Line != tc.line || e.Seq != len(before)+1 || e.Author != AuthorBuilder || e.At == "" {
					t.Fatalf("entry = %+v, want line %q seq %d", e, tc.line, len(before)+1)
				}
				return
			}
			assertRefusal(t, err, tc.code, tc.err)
			if len(after) != len(before) {
				t.Fatal("a refused verb changed the journal")
			}
		})
	}
}

func assertRefusal(t *testing.T, err error, code int, want string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want a draft error", err)
	}
	if e.Code != code {
		t.Fatalf("exit code = %d, want %d (%v)", e.Code, code, err)
	}
	if want != "" && e.Error() != want {
		t.Fatalf("error text\n got: %s\nwant: %s", e.Error(), want)
	}
	if !strings.HasPrefix(e.Error(), "error: ") {
		t.Fatalf("error text lacks its prefix: %s", e.Error())
	}
}

func TestOwnerMayEditAPlanLineInPlace(t *testing.T) {
	s := openTemp(t)
	play(t, s, []cmd{c("plan data", "deal", "old")})
	if _, err := s.Append(AuthorOwner, "plan data", []string{"deal", "new"}, ""); err != nil {
		t.Fatal(err)
	}
	st, _ := s.State()
	if len(st.Plan.Data) != 1 || st.Plan.Data[0].Line != "new" {
		t.Fatalf("plan data = %+v", st.Plan.Data)
	}
}

func TestOnlyCodeafWritesThePhase(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Append(AuthorOwner, "phase", []string{"done"}, "x"); err == nil {
		t.Fatal("the owner wrote the phase")
	}
	e, err := s.Append(AuthorCodeaf, "phase", []string{"done"}, "All built")
	if err != nil || e.Line != "Done · All built" {
		t.Fatalf("entry %+v err %v", e, err)
	}
	if st, _ := s.State(); st.Phase != PhaseDone || st.Done != "All built" {
		t.Fatalf("state = %+v", st)
	}
}

func TestFoldFollowsTheJournalAndPhases(t *testing.T) {
	s := openTemp(t)
	play(t, s, planned()[:5])
	if st, _ := s.State(); st.Phase != PhasePlanning {
		t.Fatalf("phase = %s", st.Phase)
	}
	play(t, s, planned()[5:6])
	if st, _ := s.State(); st.Phase != PhasePlanned {
		t.Fatalf("phase = %s", st.Phase)
	}
	play(t, s, planned()[6:])
	entries, _ := s.Entries(0)
	st := Fold(entries)
	if st.Phase != PhaseBuilding || len(st.Records) != 2 || strings.Join(st.FieldOrder["deal"], ",") != "name,stage" {
		t.Fatalf("state = %+v", st)
	}
	if tail, _ := s.Entries(len(entries) - 1); len(tail) != 1 || tail[0].Seq != len(entries) {
		t.Fatalf("Entries(since) = %+v", tail)
	}
	for i, e := range entries {
		if e.Seq != i+1 {
			t.Fatalf("entry %d has seq %d", i, e.Seq)
		}
	}
}

func TestFoldSkipsAVerbThisBinaryDoesNotKnow(t *testing.T) {
	st := Fold([]Entry{{Seq: 1, Verb: "from-the-future"}, {Seq: 2, Verb: "name", Args: []string{"CRM"}}})
	if st.Name != "CRM" {
		t.Fatalf("name = %q", st.Name)
	}
}

func TestRecordLineNamesTheFieldsInOrder(t *testing.T) {
	s := openTemp(t)
	play(t, s, planned())
	e, err := s.Append(AuthorBuilder, "field", []string{"deal", "amount", "money"}, "")
	if err != nil || e.Line != "Deal · name, stage, amount" {
		t.Fatalf("line %q err %v", e.Line, err)
	}
}

func TestAPlanLineIsAtMost120Characters(t *testing.T) {
	s := openTemp(t)
	long := strings.Repeat("x", 121)
	_, err := s.Append(AuthorBuilder, "plan data", []string{"deal", long}, "")
	if err == nil || !strings.Contains(err.Error(), "a plan line is at most 120 characters, got 121 — shorten it") {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.Append(AuthorBuilder, "plan data", []string{"deal", strings.Repeat("x", 120)}, ""); err != nil {
		t.Fatalf("a 120-character line must pass: %v", err)
	}
}
