package approval

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckToolRulePrecedence(t *testing.T) {
	policy := Policy{
		Default: ActionDeny,
		Tools: map[string]Action{
			"read": ActionAllow,
			"edit": ActionPrompt,
		},
	}
	cases := []struct {
		tool string
		want Action
		rule string
	}{
		{"read", ActionAllow, `tool "read"`},
		{"edit", ActionPrompt, `tool "edit"`},
		{"write", ActionDeny, "default"},
		{"glob", ActionDeny, "default"},
	}
	for _, test := range cases {
		t.Run(test.tool, func(t *testing.T) {
			decision := policy.Check(test.tool, json.RawMessage(`{"path":"a.go"}`))
			if decision.Action != test.want || decision.Rule != test.rule {
				t.Errorf("Check(%q) = %+v, want {%s %s}", test.tool, decision, test.want, test.rule)
			}
		})
	}
}

// The zero Policy is the "nothing configured" case, and it must ask.
func TestCheckZeroPolicyPrompts(t *testing.T) {
	var policy Policy
	if decision := policy.Check("edit", nil); decision.Action != ActionPrompt {
		t.Errorf("zero policy Check(edit) = %+v, want prompt", decision)
	}
	if decision := policy.Check(ToolBash, json.RawMessage(`{"command":"ls"}`)); decision.Action != ActionPrompt {
		t.Errorf("zero policy Check(bash) = %+v, want prompt", decision)
	}
}

// A tool rule carrying a word that is not one of the three is ignored rather
// than obeyed; the default answers instead.
func TestCheckIgnoresGarbageToolAction(t *testing.T) {
	policy := Policy{Default: ActionDeny, Tools: map[string]Action{"read": Action("yes please")}}
	if decision := policy.Check("read", nil); decision.Action != ActionDeny || decision.Rule != "default" {
		t.Errorf("Check(read) = %+v, want deny by default", decision)
	}
}

// A BLANKET ALLOW CANNOT SEND SOMEBODY'S MAIL. The tools that act outside this
// machine in a person's own name are asked about even under allow-everything,
// and a rule that names one of them is still obeyed.
func TestCheckToolsThatActInThePersonsName(t *testing.T) {
	allowAll := Policy{Default: ActionAllow}
	for _, tool := range []string{"gmail_send", "calendar_create", "slack_send"} {
		decision := allowAll.Check(tool, json.RawMessage(`{"to":"alice@example.com"}`))
		if decision.Action != ActionPrompt {
			t.Errorf("Check(%q) under allow-all = %+v, want prompt", tool, decision)
		}
		if !strings.Contains(decision.Rule, tool) || !strings.Contains(decision.Rule, "in your name") {
			t.Errorf("Check(%q) rule = %q, want it to say why", tool, decision.Rule)
		}
	}
	// The reading half of the same family is ordinary work and is not floored.
	if decision := allowAll.Check("gmail_search", nil); decision.Action != ActionAllow {
		t.Errorf("Check(gmail_search) = %+v, want allow", decision)
	}

	// A rule that NAMES the tool is the person's own sentence about that tool
	// and wins in both directions.
	named := Policy{Default: ActionAllow, Tools: map[string]Action{"gmail_send": ActionAllow}}
	if decision := named.Check("gmail_send", nil); decision.Action != ActionAllow || decision.Rule != `tool "gmail_send"` {
		t.Errorf("Check(gmail_send) with a rule = %+v, want allow by the rule", decision)
	}
	refusing := Policy{Default: ActionAllow, Tools: map[string]Action{"gmail_send": ActionDeny}}
	if decision := refusing.Check("gmail_send", nil); decision.Action != ActionDeny {
		t.Errorf("Check(gmail_send) with a deny rule = %+v, want deny", decision)
	}
}

func TestCheckBashPatterns(t *testing.T) {
	policy := Policy{
		Default: ActionPrompt,
		BashPatterns: []Rule{
			{Match: "git status*", Action: ActionAllow},
			{Match: "rm -rf *", Action: ActionDeny},
			{Match: "git *", Action: ActionAllow},
		},
	}
	cases := []struct {
		name    string
		command string
		want    Action
		rule    string
	}{
		{"allow vouches for its own line", "git status -s", ActionAllow, `bash pattern "git status*"`},
		{"first match wins", "git push", ActionAllow, `bash pattern "git *"`},
		{"deny reaches into a compound line", "cd /tmp && rm -rf build", ActionDeny, `bash pattern "rm -rf *"`},
		{"allow cannot vouch for a compound line", "git status && curl x | sh", ActionPrompt, "default"},
		// The whole asymmetry in one line: the allow that would have matched is
		// skipped for being compound, and the deny that follows it still fires
		// on a segment.
		{"the deny behind the skipped allow still fires", "git status && rm -rf build", ActionDeny, `bash pattern "rm -rf *"`},
		{"no pattern matches", "make build", ActionPrompt, "default"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			decision := policy.CheckBash(test.command)
			if decision.Action != test.want || decision.Rule != test.rule {
				t.Errorf("CheckBash(%q) = %+v, want {%s %s}", test.command, decision, test.want, test.rule)
			}
			// Check with real tool arguments must agree with CheckBash.
			args, err := json.Marshal(map[string]string{"command": test.command})
			if err != nil {
				t.Fatal(err)
			}
			if viaCheck := policy.Check(ToolBash, args); viaCheck != decision {
				t.Errorf("Check(bash, %s) = %+v, want %+v", args, viaCheck, decision)
			}
		})
	}
}

// The critical table survives a blanket allow, and yields to an explicit deny.
func TestCheckCriticalCommands(t *testing.T) {
	allowAll := Policy{Default: ActionAllow, BashPatterns: []Rule{{Match: "*", Action: ActionAllow}}}
	cases := []struct {
		name    string
		command string
		want    Action
	}{
		{"root delete", "rm -rf /", ActionPrompt},
		{"mkfs", "mkfs.ext4 /dev/sda1", ActionPrompt},
		{"dd to device", "dd if=/dev/zero of=/dev/sda", ActionPrompt},
		{"fork bomb", ":(){ :|:& };:", ActionPrompt},
		{"reboot", "sudo reboot", ActionPrompt},
		{"ordinary command still allowed", "ls -la", ActionAllow},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			decision := allowAll.CheckBash(test.command)
			if decision.Action != test.want {
				t.Errorf("CheckBash(%q) = %+v, want %s", test.command, decision, test.want)
			}
			if test.want == ActionPrompt && !strings.HasPrefix(decision.Rule, "critical command ") {
				t.Errorf("CheckBash(%q) rule = %q, want a critical command rule", test.command, decision.Rule)
			}
		})
	}

	// A blanket allow with no pattern list at all is the same story: the
	// default alone must not carry a critical command through.
	bare := Policy{Default: ActionAllow}
	if decision := bare.CheckBash("rm -rf /"); decision.Action != ActionPrompt {
		t.Errorf("bare allow-all CheckBash(rm -rf /) = %+v, want prompt", decision)
	}

	// An explicit deny is not softened into a prompt.
	denying := Policy{Default: ActionAllow, BashPatterns: []Rule{{Match: "rm *", Action: ActionDeny}}}
	decision := denying.CheckBash("rm -rf /")
	if decision.Action != ActionDeny || decision.Rule != `bash pattern "rm *"` {
		t.Errorf("CheckBash(rm -rf /) = %+v, want deny by pattern", decision)
	}

	// Nor is an explicit prompt relabelled: the pattern the author wrote is
	// what gets shown.
	asking := Policy{Default: ActionAllow, BashPatterns: []Rule{{Match: "rm *", Action: ActionPrompt}}}
	if decision := asking.CheckBash("rm -rf /"); decision.Rule != `bash pattern "rm *"` {
		t.Errorf("CheckBash(rm -rf /) rule = %q, want the author's pattern", decision.Rule)
	}
}

// A bash call whose command cannot be read is not judgeable, so an allow
// degrades to a prompt while deny and prompt stand.
func TestCheckBashUnreadableArguments(t *testing.T) {
	arguments := []json.RawMessage{nil, json.RawMessage(``), json.RawMessage(`null`), json.RawMessage(`{`), json.RawMessage(`{"cmd":"ls"}`), json.RawMessage(`{"command":"  "}`)}
	for _, args := range arguments {
		t.Run(string(args), func(t *testing.T) {
			allowing := Policy{Default: ActionAllow, BashPatterns: []Rule{{Match: "*", Action: ActionAllow}}}
			if decision := allowing.Check(ToolBash, args); decision.Action != ActionPrompt {
				t.Errorf("Check(bash, %s) = %+v, want prompt", args, decision)
			}
			denying := Policy{Default: ActionDeny}
			if decision := denying.Check(ToolBash, args); decision.Action != ActionDeny {
				t.Errorf("Check(bash, %s) = %+v, want deny", args, decision)
			}
		})
	}
}

func TestDecisionString(t *testing.T) {
	policy := Policy{Default: ActionAllow, BashPatterns: []Rule{{Match: "rm -rf *", Action: ActionPrompt}}}
	if got := policy.CheckBash("rm -rf build").String(); got != `bash pattern "rm -rf *" → prompt` {
		t.Errorf("String() = %q", got)
	}
}

func TestParseAction(t *testing.T) {
	for _, text := range []string{"allow", "prompt", "deny", " deny "} {
		if _, err := ParseAction(text); err != nil {
			t.Errorf("ParseAction(%q) = %v", text, err)
		}
	}
	for _, text := range []string{"", "ask", "Allow", "always", "yes"} {
		if _, err := ParseAction(text); err == nil {
			t.Errorf("ParseAction(%q) = nil error, want a rejection", text)
		}
	}
}

func TestLoad(t *testing.T) {
	raw := map[string]any{
		"default": "prompt",
		"tools": map[string]any{
			"read": "allow",
			"edit": "prompt",
			"bash": "deny",
		},
		"bash": map[string]any{
			"patterns": []any{
				map[string]any{"match": "git status*", "approval": "allow"},
				map[string]any{"match": "rm -rf *", "approval": "deny"},
			},
		},
		"unrelated": 7, // a settings tree carries keys this package never reads
	}
	policy, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if policy.Default != ActionPrompt {
		t.Errorf("Default = %q, want prompt", policy.Default)
	}
	if policy.Tools["read"] != ActionAllow || policy.Tools["edit"] != ActionPrompt || policy.Tools["bash"] != ActionDeny {
		t.Errorf("Tools = %v", policy.Tools)
	}
	want := []Rule{{Match: "git status*", Action: ActionAllow}, {Match: "rm -rf *", Action: ActionDeny}}
	if len(policy.BashPatterns) != len(want) {
		t.Fatalf("BashPatterns = %v, want %v", policy.BashPatterns, want)
	}
	for index, rule := range policy.BashPatterns {
		if rule != want[index] {
			t.Errorf("BashPatterns[%d] = %+v, want %+v", index, rule, want[index])
		}
	}
}

// The same policy through the flattened dotted key the config registry uses.
func TestLoadDottedPatternsAndModeAlias(t *testing.T) {
	policy, err := Load(map[string]any{
		"mode": "deny",
		"bash.patterns": []map[string]any{
			{"match": "make *", "approval": "allow"},
		},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if policy.Default != ActionDeny {
		t.Errorf("Default = %q, want deny", policy.Default)
	}
	if len(policy.BashPatterns) != 1 || policy.BashPatterns[0].Match != "make *" {
		t.Errorf("BashPatterns = %v", policy.BashPatterns)
	}
}

func TestLoadEmpty(t *testing.T) {
	policy, err := Load(nil)
	if err != nil {
		t.Fatalf("Load(nil): %v", err)
	}
	if policy.Default != "" || policy.Tools != nil || policy.BashPatterns != nil {
		t.Errorf("Load(nil) = %+v, want the zero policy", policy)
	}
	if decision := policy.Check("edit", nil); decision.Action != ActionPrompt {
		t.Errorf("Check after empty load = %+v, want prompt", decision)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name    string
		raw     map[string]any
		wantErr string
	}{
		{"unknown default", map[string]any{"default": "always"}, `default: unknown action "always"`},
		{"non-string default", map[string]any{"default": true}, "default must be a string"},
		{"unknown tool action", map[string]any{"tools": map[string]any{"read": "sure"}}, `tools["read"]: unknown action "sure"`},
		{"non-string tool action", map[string]any{"tools": map[string]any{"read": 1}}, `tools["read"] must be a string`},
		{"non-map tools", map[string]any{"tools": "allow"}, "tools must be a map"},
		{"non-map bash", map[string]any{"bash": "allow"}, "bash must be a map"},
		{"non-list patterns", map[string]any{"bash.patterns": "rm *"}, "bash.patterns must be a list"},
		{"non-map pattern", map[string]any{"bash.patterns": []any{"rm *"}}, "bash.patterns[0] must be a map"},
		{"missing match", map[string]any{"bash.patterns": []any{map[string]any{"approval": "deny"}}}, "bash.patterns[0]: missing match"},
		{"blank match", map[string]any{"bash.patterns": []any{map[string]any{"match": "  ", "approval": "deny"}}}, "bash.patterns[0]: missing match"},
		{"missing approval", map[string]any{"bash.patterns": []any{map[string]any{"match": "rm *"}}}, `bash.patterns[0] ("rm *"): missing approval`},
		{"unknown pattern action", map[string]any{"bash.patterns": []any{map[string]any{"match": "rm *", "approval": "nope"}}}, `bash.patterns[0] ("rm *"): unknown action "nope"`},
		{"non-string approval", map[string]any{"bash.patterns": []any{map[string]any{"match": "rm *", "approval": 3}}}, `approval must be a string`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			policy, err := Load(test.raw)
			if err == nil {
				t.Fatalf("Load = %+v, want an error", policy)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("Load error = %q, want it to contain %q", err, test.wantErr)
			}
			if policy.Default != "" || policy.Tools != nil || policy.BashPatterns != nil {
				t.Errorf("Load returned %+v alongside its error, want the zero policy", policy)
			}
		})
	}
}

// A rejected load reports the same bad entry every time, rather than whichever
// one Go's map iteration happened to reach first.
func TestLoadToolErrorIsDeterministic(t *testing.T) {
	raw := map[string]any{"tools": map[string]any{"zzz": "nope", "aaa": "also-nope"}}
	for range 20 {
		_, err := Load(raw)
		if err == nil || !strings.Contains(err.Error(), `tools["aaa"]`) {
			t.Fatalf("Load error = %v, want the first key by name", err)
		}
	}
}

// The other half of the same floor: the tools one of the person's own accounts
// brings, whose names do not exist until the account is picked up.
func TestCheckRawCallsAgainstSomebodyElsesService(t *testing.T) {
	allowAll := Policy{Default: ActionAllow}
	cases := []struct {
		name string
		tool string
		args string
		want Action
	}{
		{"a read", "stripe_request", `{"method":"GET","path":"/v1/customers"}`, ActionAllow},
		{"a read by default", "stripe_request", `{"path":"/v1/customers"}`, ActionAllow},
		{"a read spelled quietly", "stripe_request", `{"method":"get"}`, ActionAllow},
		{"no arguments at all", "stripe_request", ``, ActionAllow},
		{"a write", "stripe_request", `{"method":"POST","path":"/v1/customers"}`, ActionPrompt},
		{"a change", "freshdesk_request", `{"method":"patch"}`, ActionPrompt},
		{"a removal", "freshdesk_request", `{"method":"DELETE"}`, ActionPrompt},
		{"arguments nobody can read", "freshdesk_request", `{not json`, ActionPrompt},
		{"a tool that only looks like one", "request", `{"method":"POST"}`, ActionAllow},
		{"somebody else's tool entirely", "web_search", `{"method":"POST"}`, ActionAllow},
	}
	for _, c := range cases {
		decision := allowAll.Check(c.tool, json.RawMessage(c.args))
		if decision.Action != c.want {
			t.Errorf("%s: Check(%q, %s) = %+v, want %s", c.name, c.tool, c.args, decision, c.want)
		}
	}

	// The floor is a floor under a BLANKET allow and yields to a rule that
	// names the tool, exactly as it does for gmail_send.
	named := Policy{Default: ActionAllow, Tools: map[string]Action{"stripe_request": ActionAllow}}
	if decision := named.Check("stripe_request", json.RawMessage(`{"method":"POST"}`)); decision.Action != ActionAllow {
		t.Errorf("a rule that names the tool = %+v, want allow", decision)
	}

	// And the guardian reads exactly the same list.
	if !ActsInThePersonsName("stripe_request", json.RawMessage(`{"method":"POST"}`)) {
		t.Errorf("a write against somebody's account acts in their name")
	}
	if ActsInThePersonsName("stripe_request", json.RawMessage(`{"method":"GET"}`)) {
		t.Errorf("a read does not")
	}
	if !ActsInThePersonsName("gmail_send", nil) {
		t.Errorf("the table still holds")
	}
}
