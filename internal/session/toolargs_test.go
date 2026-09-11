package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE DEFECT THIS FILE EXISTS FOR, in the owner's own screen:
//
//	tasks — Invalid arguments: json: cannot unmarshal number 10.0 into Go struct
//	field tasksArguments.limit of type int
//
// Eleven times, in one turn. Two things are pinned here: 10.0 is ten, and no
// refusal ever again names a Go struct.

// argumentsForTest mirrors the shapes the belt's tools actually decode into: a
// plain int, a pointer int, a list of ids, text, a truth, a fraction, a raw id
// that means to take both spellings, and one nested object (`stand`'s rails).
type argumentsForTest struct {
	Limit     int             `json:"limit"`
	Lines     *int            `json:"lines"`
	DependsOn []uint64        `json:"depends_on"`
	Query     string          `json:"query"`
	Wide      bool            `json:"wide"`
	PerRun    float64         `json:"per_run_usd"`
	ID        json.RawMessage `json:"id"`
	Rails     struct {
		MaxPerDay int `json:"max_per_day"`
	} `json:"rails"`
	// Dimensions is the shape of an answer's comparison axes: a text table.
	Dimensions map[string]string `json:"dimensions"`
	// Pick is `ask`'s own pick, a type that decodes itself.
	Pick *Pick `json:"pick"`
	// Options is the shape of `ask`'s answers — a list of objects that each carry
	// a list — because that is the argument a provider was seen wrap in a string.
	Options []struct {
		Key    string   `json:"key"`
		Blocks []string `json:"blocks"`
	} `json:"options"`
}

func TestDecodeToolArgumentsTakesTheLooseFormsAndRefusesTheRestInWordsAModelCanAct(t *testing.T) {
	for _, one := range []struct {
		name string
		args string
		// want is checked against the decoded value when refusal is empty.
		check func(*testing.T, argumentsForTest)
		// refusal is the exact sentence, when the call must be refused.
		refusal string
	}{
		{
			name:  "a plain integer is a plain integer",
			args:  `{"limit":10}`,
			check: func(t *testing.T, got argumentsForTest) { equalInt(t, "limit", got.Limit, 10) },
		},
		{
			name:  "THE DEFECT: a whole number sent as a float is that number",
			args:  `{"limit":10.0}`,
			check: func(t *testing.T, got argumentsForTest) { equalInt(t, "limit", got.Limit, 10) },
		},
		{
			name:  "exponent notation is a whole number too",
			args:  `{"limit":1e2}`,
			check: func(t *testing.T, got argumentsForTest) { equalInt(t, "limit", got.Limit, 100) },
		},
		{
			name:  "a negative whole float keeps its sign",
			args:  `{"limit":-3.0}`,
			check: func(t *testing.T, got argumentsForTest) { equalInt(t, "limit", got.Limit, -3) },
		},
		{
			name:    "a fraction is refused, and the refusal is the corrected call",
			args:    `{"limit":10.5}`,
			refusal: `limit takes a whole number: send {"limit":10}, not 10.5`,
		},
		{
			name:    "a numeric string is refused rather than converted, and says what to send",
			args:    `{"limit":"10"}`,
			refusal: `limit takes a whole number: send {"limit":10}, not "10"`,
		},
		{
			name:    "text that is not a number at all has no corrected call to offer",
			args:    `{"limit":"ten"}`,
			refusal: `limit takes a whole number; "ten" is not one`,
		},
		{
			name:    "a truth where a number belongs",
			args:    `{"limit":true}`,
			refusal: `limit takes a whole number; true is not one`,
		},
		{
			name:  "an absent argument is not a fault",
			args:  `{"query":"reconciler"}`,
			check: func(t *testing.T, got argumentsForTest) { equalInt(t, "limit", got.Limit, 0) },
		},
		{
			name: "a null argument leaves the value at its zero",
			args: `{"limit":null,"lines":null}`,
			check: func(t *testing.T, got argumentsForTest) {
				equalInt(t, "limit", got.Limit, 0)
				if got.Lines != nil {
					t.Fatalf("lines: want nil, got %d", *got.Lines)
				}
			},
		},
		{
			name: "a pointer int takes the loose form too, and absent stays absent",
			args: `{"lines":40.0}`,
			check: func(t *testing.T, got argumentsForTest) {
				if got.Lines == nil {
					t.Fatal("lines: want 40, got nil")
				}
				equalInt(t, "lines", *got.Lines, 40)
			},
		},
		{
			name: "a list of ids takes the loose form item by item",
			args: `{"depends_on":[7.0,8]}`,
			check: func(t *testing.T, got argumentsForTest) {
				if len(got.DependsOn) != 2 || got.DependsOn[0] != 7 || got.DependsOn[1] != 8 {
					t.Fatalf("depends_on: want [7 8], got %v", got.DependsOn)
				}
			},
		},
		{
			name: "THE ASK DEFECT: a list sent as a JSON string holding the list is that list",
			args: `{"options":"[{\"key\":\"1\",\"blocks\":[\"a\"]},{\"key\":\"2\"}]"}`,
			check: func(t *testing.T, got argumentsForTest) {
				if len(got.Options) != 2 || got.Options[0].Key != "1" || got.Options[1].Key != "2" {
					t.Fatalf("options: want two answers keyed 1 and 2, got %+v", got.Options)
				}
				if len(got.Options[0].Blocks) != 1 || got.Options[0].Blocks[0] != "a" {
					t.Fatalf("options[0].blocks: want [a], got %v", got.Options[0].Blocks)
				}
			},
		},
		{
			name:  "an object sent as a JSON string holding the object is that object, leaves and all",
			args:  `{"rails":"{\"max_per_day\": 3.0}"}`,
			check: func(t *testing.T, got argumentsForTest) { equalInt(t, "max_per_day", got.Rails.MaxPerDay, 3) },
		},
		{
			name: "a string wrapped inside a list that was itself wrapped is unwrapped at each level it asks for",
			args: `{"options":"[{\"key\":\"1\",\"blocks\":\"[\\\"a\\\",\\\"b\\\"]\"}]"}`,
			check: func(t *testing.T, got argumentsForTest) {
				if len(got.Options) != 1 || len(got.Options[0].Blocks) != 2 || got.Options[0].Blocks[1] != "b" {
					t.Fatalf("options: want one answer with blocks [a b], got %+v", got.Options)
				}
			},
		},
		{
			name: "THE ASK DEFECT AS IT IS MOSTLY SEEN: the list and every field after it arrive inside one string",
			args: `{"limit":3,"query":"what","options":"[{\"key\":\"1\",\"blocks\":[\"a\"]}], \"depends_on\": [7.0], \"rails\": {\"max_per_day\": 2.0}, \"query\": \"inner\"}"}`,
			check: func(t *testing.T, got argumentsForTest) {
				if len(got.Options) != 1 || got.Options[0].Key != "1" {
					t.Fatalf("options: want the one answer that was inside the string, got %+v", got.Options)
				}
				if len(got.DependsOn) != 1 || got.DependsOn[0] != 7 {
					t.Fatalf("depends_on: the field after the swallowed list was lost: %v", got.DependsOn)
				}
				equalInt(t, "max_per_day", got.Rails.MaxPerDay, 2)
				// What the model wrote at the top level stands; the copy inside the
				// string never overwrites it.
				equalText(t, "query", got.Query, "what")
				equalInt(t, "limit", got.Limit, 3)
			},
		},
		{
			name: "THE PICK DEFECT: a pick written as nothing but its key, as a number or as text",
			args: `{"pick":1}`,
			check: func(t *testing.T, got argumentsForTest) {
				if got.Pick == nil || got.Pick.Key != "1" {
					t.Fatalf("pick: want key 1, got %+v", got.Pick)
				}
			},
		},
		{
			name: "a pick as text is that key too",
			args: `{"pick":"2"}`,
			check: func(t *testing.T, got argumentsForTest) {
				if got.Pick == nil || got.Pick.Key != "2" {
					t.Fatalf("pick: want key 2, got %+v", got.Pick)
				}
			},
		},
		{
			name: "a pick's object form is walked by the one decoder",
			args: `{"pick":{"key":"3","reason":"why"}}`,
			check: func(t *testing.T, got argumentsForTest) {
				if got.Pick == nil || got.Pick.Key != "3" || got.Pick.Reason != "why" {
					t.Fatalf("pick: want key 3 with its reason, got %+v", got.Pick)
				}
			},
		},
		{
			name:    "and a key written as a number inside it is refused by the one rule, in the decoder's words",
			args:    `{"pick":{"key":3}}`,
			refusal: `key takes text: send {"key":"3"}, not 3`,
		},
		{
			name:    "a pick's confidence written as a number is refused in the decoder's words",
			args:    `{"pick":{"key":"1","confidence":true}}`,
			refusal: `confidence takes text: send {"confidence":"true"}, not true`,
		},
		{
			name: "a swallowed tail closed with a bracket after its brace is still that tail",
			args: `{"options":"[{\"key\":\"1\"}], \"depends_on\": [7]}]"}`,
			check: func(t *testing.T, got argumentsForTest) {
				if len(got.Options) != 1 || len(got.DependsOn) != 1 || got.DependsOn[0] != 7 {
					t.Fatalf("want the answer and the field after it, got %+v %v", got.Options, got.DependsOn)
				}
			},
		},
		{
			name: "THE SPILL LAW: a complete list followed by a leaked control token and prose is that list",
			args: `{"depends_on":"[7, 8]\n<｜DSML｜>I recommend the first, because"}`,
			check: func(t *testing.T, got argumentsForTest) {
				if len(got.DependsOn) != 2 || got.DependsOn[1] != 8 {
					t.Fatalf("depends_on: want [7 8] with the spill dropped, got %v", got.DependsOn)
				}
			},
		},
		{
			name: "a backslash before a real newline inside the wrapped string is the newline it meant",
			args: badEscapeArguments,
			check: func(t *testing.T, got argumentsForTest) {
				if len(got.Options) != 1 || len(got.Options[0].Blocks) != 1 || got.Options[0].Blocks[0] != "top\n│ mid\nbottom" {
					t.Fatalf("want the diagram with its line breaks, got %+v", got.Options)
				}
			},
		},
		{
			name:    "any other bad escape inside the wrapped string is still refused",
			args:    `{"depends_on":"[7, \"\\q\"]"}`,
			refusal: `depends_on takes a list; it arrived as text, "[7, \"\\q\"]" — send the value itself, not a string holding it`,
		},
		{
			name: "and a swallowed tail followed by prose is that tail, with nothing read out of the prose",
			args: `{"depends_on":"[7], \"limit\": 2} and \"limit\": 9 is what I meant"}`,
			check: func(t *testing.T, got argumentsForTest) {
				if len(got.DependsOn) != 1 || got.DependsOn[0] != 7 {
					t.Fatalf("depends_on: want [7], got %v", got.DependsOn)
				}
				equalInt(t, "limit", got.Limit, 2)
			},
		},
		{
			name: "a tail that never closes is not an object, so only the list at its head is read",
			args: `{"depends_on":"[7], \"limit\": 1"}`,
			check: func(t *testing.T, got argumentsForTest) {
				if len(got.DependsOn) != 1 || got.DependsOn[0] != 7 {
					t.Fatalf("depends_on: want [7], got %v", got.DependsOn)
				}
				equalInt(t, "limit", got.Limit, 0)
			},
		},
		{
			name:    "a string that is not holding a list is refused, and the refusal says it arrived as text",
			args:    `{"depends_on":"seven and eight"}`,
			refusal: `depends_on takes a list; it arrived as text, "seven and eight" — send the value itself, not a string holding it`,
		},
		{
			name:    "a string holding an object where a list was wanted is not unwrapped into the wrong shape",
			args:    `{"depends_on":"{\"first\":7}"}`,
			refusal: `depends_on takes a list; it arrived as text, "{\"first\":7}" — send the value itself, not a string holding it`,
		},
		{
			name:    "a number where a list was wanted still reads as it always did",
			args:    `{"depends_on":7}`,
			refusal: `depends_on takes a list`,
		},
		{
			name:  "a nested object is walked to its leaves",
			args:  `{"rails":{"max_per_day":3.0}}`,
			check: func(t *testing.T, got argumentsForTest) { equalInt(t, "max_per_day", got.Rails.MaxPerDay, 3) },
		},
		{
			name:    "a nested leaf is refused by its own name",
			args:    `{"rails":{"max_per_day":3.5}}`,
			refusal: `max_per_day takes a whole number: send {"max_per_day":3}, not 3.5`,
		},
		{
			name: "a genuine fraction is left alone",
			args: `{"per_run_usd":0.15}`,
			check: func(t *testing.T, got argumentsForTest) {
				if got.PerRun != 0.15 {
					t.Fatalf("per_run_usd: want 0.15, got %v", got.PerRun)
				}
			},
		},
		{
			name:    "a number where a named text argument belongs says how to quote it",
			args:    `{"query":7}`,
			refusal: `query takes text: send {"query":"7"}, not 7`,
		},
		{
			name: "THE AXIS DEFECT: a number in a cell of a text table is that number's spelling",
			args: `{"dimensions":{"complexity":1,"cost":0.50,"speed":"fast"}}`,
			check: func(t *testing.T, got argumentsForTest) {
				equalText(t, "complexity", got.Dimensions["complexity"], "1")
				equalText(t, "cost", got.Dimensions["cost"], "0.50")
				equalText(t, "speed", got.Dimensions["speed"], "fast")
			},
		},
		{
			name:    "but a truth in a cell is still refused, in the cell's own name",
			args:    `{"dimensions":{"cheap":true}}`,
			refusal: `cheap takes text: send {"cheap":"true"}, not true`,
		},
		{
			name:    "a word where a truth belongs",
			args:    `{"wide":"yes"}`,
			refusal: `wide takes true or false; "yes" is not one`,
		},
		{
			name: "A RAW ARGUMENT KEEPS BOTH SPELLINGS: tasks takes 7 and \"7\" alike",
			args: `{"id":7}`,
			check: func(t *testing.T, got argumentsForTest) {
				if strings.TrimSpace(string(got.ID)) != "7" {
					t.Fatalf("id: want 7, got %q", string(got.ID))
				}
			},
		},
		{
			name:  "an unknown argument is ignored, exactly as it always was",
			args:  `{"limit":2,"nonsense":{"deep":[1,2]}}`,
			check: func(t *testing.T, got argumentsForTest) { equalInt(t, "limit", got.Limit, 2) },
		},
		{
			name:    "bytes that are not JSON say so without naming a parser",
			args:    `{"limit":`,
			refusal: "the arguments are not valid JSON — send one JSON object",
		},
		{
			name:    "arguments that are not an object at all",
			args:    `[1,2,3]`,
			refusal: "the arguments must be one JSON object",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			var got argumentsForTest
			err := decodeToolArguments(json.RawMessage(one.args), &got)
			if one.refusal != "" {
				if err == nil {
					t.Fatalf("want refusal %q, got none", one.refusal)
				}
				if err.Error() != one.refusal {
					t.Fatalf("refusal:\n want %q\n  got %q", one.refusal, err.Error())
				}
				assertNoMachineryInRefusal(t, err.Error())
				return
			}
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			one.check(t, got)
		})
	}
}

// THE REFUSAL IS FOR THE MODEL AND NOBODY ELSE READS GO. Every sentence this
// decoder can produce is checked against the vocabulary that broke the owner's
// session: a struct name, a package, a Go type, the word "unmarshal".
func assertNoMachineryInRefusal(t *testing.T, sentence string) {
	t.Helper()
	for _, banned := range []string{
		"json: ", "unmarshal", "Go struct", "Go value", "of type int",
		"Arguments.", "reflect", "encoding/json",
	} {
		if strings.Contains(sentence, banned) {
			t.Fatalf("refusal %q carries machinery vocabulary %q — nobody outside can see it", sentence, banned)
		}
	}
}

// badEscapeArguments is the wrapped-string shape with the model's one bad
// escape in it: inside the held list, a diagram's line breaks are written as a
// backslash followed by a REAL newline. It is built rather than spelled because
// three layers of quoting in one literal is where a fixture goes wrong.
var badEscapeArguments = func() string {
	held := "[{\"key\":\"1\",\"blocks\":[\"top\\\n│ mid\\\nbottom\"]}]"
	wrapped, _ := json.Marshal(held)
	return `{"options":` + string(wrapped) + `}`
}()

func equalText(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: want %q, got %q", name, want, got)
	}
}

func equalInt(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: want %d, got %d", name, want, got)
	}
}

func TestDecodeToolArgumentsEmptiesTheTargetBeforeItsSecondPass(t *testing.T) {
	// The first pass fills what it can before reporting a fault, so a repair
	// that no longer mentions a field must not leave the first pass's leavings
	// behind. Here `query` decodes, `limit` faults, and the repaired bytes carry
	// both — the point is that nothing is decoded twice into a dirty target.
	var got argumentsForTest
	got.Limit = 99
	got.Query = "stale"
	if err := decodeToolArguments(json.RawMessage(`{"query":"fresh","limit":4.0}`), &got); err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
	equalInt(t, "limit", got.Limit, 4)
	if got.Query != "fresh" {
		t.Fatalf("query: want fresh, got %q", got.Query)
	}
}

func TestArgumentRepairReadsAToolsOwnRefusalBack(t *testing.T) {
	for _, one := range []struct {
		text   string
		repair string
		is     bool
	}{
		{text: `Invalid arguments: limit takes a whole number: send {"limit":10}, not 10.0`,
			repair: `limit takes a whole number: send {"limit":10}, not 10.0`, is: true},
		{text: "Invalid arguments: title is required\nand more", repair: "title is required", is: true},
		{text: "no such file or directory", is: false},
		{text: "Invalid arguments: ", is: false},
	} {
		repair, is := argumentRepair(one.text)
		if is != one.is {
			t.Fatalf("%q: want refusal=%v, got %v", one.text, one.is, is)
		}
		if is && repair != one.repair {
			t.Fatalf("%q: want repair %q, got %q", one.text, one.repair, repair)
		}
	}
}

// ── the two structural laws ─────────────────────────────────────────────────

// EVERY TOOL DECODES THROUGH ONE DOOR. The value of a single decoder is that a
// model learns one grammar of refusal; a tool that reaches past it hands back
// Go's words again, which is the fault this whole file exists for.
func TestNoToolDecodesItsOwnArgumentsWithEncodingJSON(t *testing.T) {
	for _, path := range toolArgumentSources(t) {
		if filepath.Base(path) == "toolargs.go" {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, banned := range []string{"json.Unmarshal(args,", "json.Unmarshal(call.Function.Arguments,"} {
			if strings.Contains(string(body), banned) {
				t.Fatalf("%s calls %s — tool arguments go through decodeToolArguments (toolargs.go), "+
					"so that a model reads one grammar of refusal and never Go's own words",
					filepath.Base(path), banned)
			}
		}
	}
}

// A WHOLE-NUMBER ARGUMENT IS DECLARED "integer", NEVER "number". JSON has no
// integers, so `"type":"number"` on an argument decoded into a Go int is an
// invitation to send the form the decoder used to refuse — which is exactly what
// a provider did, ten times over, on `tasks.limit`.
//
// The allowlist is the arguments that really are fractional — the ones whose
// USEFUL values are not whole. edit_video's `level` is a loudness whose own
// default is 0.3, and its `fade` is a length of time where half a second is an
// ordinary answer; declaring either an integer would refuse the value the tool
// itself reaches for.
func TestNoWholeNumberArgumentIsDeclaredANumber(t *testing.T) {
	// `ask`'s dial is the third kind of fractional argument: a RANGE rather than
	// a count. [Dial] is float64 at both ends and in the middle because the thing
	// on the dial is a quantity the asker chose — a share, a threshold, a rate —
	// and declaring its ends whole would refuse "0 to 1, default 0.3" outright.
	fractional := map[string]bool{"per_run_usd": true, "level": true, "fade": true,
		"min": true, "max": true, "default": true}
	for _, path := range toolArgumentSources(t) {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(body)
		for index := 0; ; {
			at := strings.Index(text[index:], `":{"type":"number"`)
			if at < 0 {
				break
			}
			at += index
			// Walk back to the opening quote of the property name.
			start := strings.LastIndexByte(text[:at], '"')
			name := text[start+1 : at]
			index = at + 1
			if fractional[name] {
				continue
			}
			t.Fatalf(`%s declares %q as "type":"number". An argument decoded into a whole `+
				`number is declared "type":"integer", or a provider that renders every number `+
				`as a float sends a form the schema invited (toolargs.go)`, filepath.Base(path), name)
		}
	}
}

// EVERY SCHEMA ON THE BELT IS ONE WELL-FORMED JSON DOCUMENT, AND NONE CARRIES A
// REFERENCE. The first half is here because the second half's own first cut
// broke it: a block spliced into the template with one brace too many left
// `ask` with a schema encoding/json could not read, and the belt's answer to
// that is to keep the tool off the shelf — `The questions tools could not be
// loaded: … malformed schema` — so the model wrote the question out as prose and
// the person never saw one. Every unit test was green. A schema is bytes the
// model reads and this program never does, so nothing but a test can notice it
// is broken.
//
// `$ref` into `$defs` is legal JSON
// Schema and it is the one shape a provider is free to flatten, ignore or
// mis-render, because nothing else on this belt ever used it and the models are
// steered by what the belt has always looked like. `ask` was the only tool that
// did, and it was the only tool deepseek-v4-flash could not call: three calls
// running on 2026-09-10 arrived with the answers list rendered as a string,
// every one refused, and the person never saw a question. A shape used in one
// place is written out in full at every place it stands (askBlockSchemaJSON),
// which costs bytes on the wire and nothing else.
func TestEveryBeltSchemaIsWellFormedAndCarriesNoReference(t *testing.T) {
	agent := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir()}}
	agent.tools = agent.belt()
	tools := agent.offeredTools()
	if len(tools) == 0 {
		t.Fatal("the belt is empty")
	}
	for _, tool := range tools {
		schema := string(tool.Schema)
		if !json.Valid(tool.Schema) {
			t.Errorf("%s's schema is not one well-formed JSON document; it ends %q", tool.Name, schema[max(0, len(schema)-80):])
			continue
		}
		for _, mark := range []string{`"$ref"`, `"$defs"`, `"definitions"`} {
			if strings.Contains(schema, mark) {
				t.Errorf("%s's schema carries %s — write the shape out in full where it stands "+
					"(see askBlockSchemaJSON); a reference is the one schema shape a provider has "+
					"been seen fail to follow", tool.Name, mark)
			}
		}
	}
}

// AND THE FOUR READERS TAKEN FROM ANOTHER PROJECT ARE HELD TO THE SAME
// DECLARATION. internal/exec/bare's `read`, `grep`, `find` and `ls` are on this
// belt and are the most-called hands on it; their offsets and limits are Go ints
// and had the same `"type":"number"` invitation. They keep their own parser — so
// their refusals are still that parser's words, which is the one part of this
// class fault this package cannot reach — but nothing there may invite the form
// again.
func TestTheBorrowedReadersAlsoDeclareTheirWholeNumbers(t *testing.T) {
	fractional := map[string]bool{"timeout": true}
	body, err := os.ReadFile(filepath.Join("..", "exec", "bare", "tools.go"))
	if err != nil {
		t.Fatalf("read bare/tools.go: %v", err)
	}
	text := string(body)
	for index := 0; ; {
		at := strings.Index(text[index:], `":{"type":"number"`)
		if at < 0 {
			break
		}
		at += index
		name := text[strings.LastIndexByte(text[:at], '"')+1 : at]
		index = at + 1
		if fractional[name] {
			continue
		}
		t.Fatalf(`internal/exec/bare/tools.go declares %q as "type":"number"; an argument `+
			`decoded into a whole number is declared "type":"integer"`, name)
	}
}

// toolArgumentSources is every non-test Go file of this package.
func toolArgumentSources(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		paths = append(paths, name)
	}
	return paths
}
