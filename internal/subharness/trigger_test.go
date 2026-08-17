package subharness

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeSource is a source that remembers what it was asked to mount. It keys by
// command line because that is what a real source would key by: two harnesses
// claiming one line is the collision that matters, and a map is the assertion.
type fakeSource struct {
	mounted map[string]Hosted
	fail    error
}

func newFakeSource() *fakeSource { return &fakeSource{mounted: map[string]Hosted{}} }

func (s *fakeSource) AddTrigger(h Hosted) error {
	if s.fail != nil {
		return s.fail
	}
	if _, taken := s.mounted[h.Command]; taken {
		return errors.New("command already mounted")
	}
	s.mounted[h.Command] = h
	return nil
}

// hostedEntry is the shape the trigger slice is about: a hosted trigger whose
// entry is an agent.loop, with an allowed-args whitelist.
func hostedEntry() Entry {
	return Entry{
		Name:     "triage",
		Desc:     "sort the inbox",
		Revision: 1,
		Tools:    []string{"read", "write"},
		Nodes: []Node{
			{
				ID:   "start",
				Kind: KindTrigger,
				Trigger: &Trigger{
					Mode:  TriggerHosted,
					Entry: "read-it",
					Args:  []string{"since", "label"},
				},
			},
			{
				ID:    "read-it",
				Kind:  KindAgentLoop,
				Brief: "read the inbox and say what is in it",
				Loop:  &AgentLoop{Model: "work", Tools: []string{"read"}},
			},
		},
	}
}

func TestHostAddsTheTriggerEntry(t *testing.T) {
	src := newFakeSource()
	e := hostedEntry()

	mounted, err := Host(src, e)
	if err != nil {
		t.Fatalf("Host: %v", err)
	}
	if len(mounted) != 1 {
		t.Fatalf("mounted %d triggers, want 1", len(mounted))
	}

	want := CommandFor("triage")
	if want != "/harness triage" {
		t.Fatalf("hosted command is %q, want /harness triage", want)
	}
	h, ok := src.mounted[want]
	if !ok {
		t.Fatalf("source has %v, want an entry for %q", keysOf(src.mounted), want)
	}
	if h.Harness != "triage" || h.Revision != 1 {
		t.Errorf("hosted %q at revision %d, want triage at 1", h.Harness, h.Revision)
	}
	if h.Entry != "read-it" || h.Node != "start" {
		t.Errorf("hosted entry %q from node %q, want read-it from start", h.Entry, h.Node)
	}
	if h.Desc != "sort the inbox" {
		t.Errorf("hosted desc %q, want the entry's own", h.Desc)
	}
	if strings.Join(h.AllowedArgs, ",") != "since,label" {
		t.Errorf("allowed args %v, want the trigger's whitelist", h.AllowedArgs)
	}
	if !reflect.DeepEqual(mounted[0], src.mounted[want]) {
		// The returned value and the mounted value are one fact; a caller that
		// journals what it mounted must be journaling what the source got.
		t.Errorf("returned %+v, mounted %+v", mounted[0], src.mounted[want])
	}
}

func TestHostCopiesTheAllowedArgs(t *testing.T) {
	src := newFakeSource()
	e := hostedEntry()

	if _, err := Host(src, e); err != nil {
		t.Fatalf("Host: %v", err)
	}
	// Mutating the entry after hosting must not reach into the source: the
	// whitelist the source refuses arguments against is its own copy.
	e.Nodes[0].Trigger.Args[0] = "everything"

	h := src.mounted[CommandFor("triage")]
	if h.AllowedArgs[0] != "since" {
		t.Fatalf("allowed args followed the entry to %v", h.AllowedArgs)
	}
}

func TestHostedAcceptsOnlyWhitelistedArgs(t *testing.T) {
	h := Hosted{Command: "/harness triage", AllowedArgs: []string{"since", "label"}}

	if err := h.Accepts([]string{"since=yesterday", "--label=urgent"}); err != nil {
		t.Fatalf("Accepts whitelisted args: %v", err)
	}
	err := h.Accepts([]string{"since=yesterday", "rm"})
	if err == nil {
		t.Fatal("Accepts took an argument that is not on the whitelist")
	}
	if !strings.Contains(err.Error(), "rm") || !strings.Contains(err.Error(), "since, label") {
		t.Errorf("refusal %q names neither the bad argument nor the good ones", err)
	}
}

func TestHostedWithNoArgsTakesNone(t *testing.T) {
	h := Hosted{Command: "/harness triage"}

	if err := h.Accepts(nil); err != nil {
		t.Fatalf("Accepts nothing: %v", err)
	}
	err := h.Accepts([]string{"since=yesterday"})
	if err == nil {
		t.Fatal("an empty whitelist granted an argument")
	}
	if !strings.Contains(err.Error(), "takes no arguments") {
		t.Errorf("refusal %q does not say the command takes no arguments", err)
	}
}

func TestHostRequiresAnAgentLoopEntry(t *testing.T) {
	e := hostedEntry()
	e.Nodes[1] = Node{ID: "read-it", Kind: KindToolCall, Call: &ToolCall{Tool: "read"}}

	src := newFakeSource()
	_, err := Host(src, e)
	if err == nil {
		t.Fatal("hosted a trigger that enters at a tool.call")
	}
	if !strings.Contains(err.Error(), "agent.loop") {
		t.Errorf("error %q does not say what a hosted entry must be", err)
	}
	if len(src.mounted) != 0 {
		t.Errorf("source mounted %v from an invalid entry", keysOf(src.mounted))
	}
}

func TestHostRejectsATriggerThatNeedsSomething(t *testing.T) {
	e := hostedEntry()
	e.Nodes[0].Needs = []string{"read-it"}

	if _, err := Host(newFakeSource(), e); err == nil {
		t.Fatal("hosted a trigger that waits for a node to finish first")
	}
}

func TestHostMountsNothingWhenNoTriggerIsHosted(t *testing.T) {
	e := hostedEntry()
	e.Nodes[0].Trigger = &Trigger{Mode: TriggerWatch, Entry: "read-it", Spec: "inbox"}

	src := newFakeSource()
	mounted, err := Host(src, e)
	if err != nil {
		t.Fatalf("Host: %v", err)
	}
	if len(mounted) != 0 || len(src.mounted) != 0 {
		t.Fatalf("mounted %v for a watch trigger", keysOf(src.mounted))
	}
}

func TestHostReportsWhatTheSourceRefused(t *testing.T) {
	src := newFakeSource()
	src.fail = errors.New("line is taken")

	if _, err := Host(src, hostedEntry()); err == nil || !strings.Contains(err.Error(), "line is taken") {
		t.Fatalf("Host swallowed the source's refusal: %v", err)
	}
}

func TestTriggerModesAreValidated(t *testing.T) {
	cases := []struct {
		name    string
		trigger Trigger
		ok      bool
	}{
		{"hosted", Trigger{Mode: TriggerHosted, Entry: "read-it"}, true},
		{"hosted names its own command", Trigger{Mode: TriggerHosted, Entry: "read-it", Command: "/inbox"}, false},
		{"idle with a spec", Trigger{Mode: TriggerIdle, Entry: "read-it", Spec: "15m"}, true},
		{"idle without one", Trigger{Mode: TriggerIdle, Entry: "read-it"}, false},
		{"watch with a spec", Trigger{Mode: TriggerWatch, Entry: "read-it", Spec: "inbox"}, true},
		{"source command", Trigger{Mode: TriggerSource, Entry: "read-it", Source: "mail", Command: "sweep"}, true},
		{"source without a command", Trigger{Mode: TriggerSource, Entry: "read-it", Source: "mail"}, false},
		{"unknown mode", Trigger{Mode: "whenever", Entry: "read-it"}, false},
		{"unknown entry", Trigger{Mode: TriggerHosted, Entry: "nowhere"}, false},
		{"duplicate arg", Trigger{Mode: TriggerHosted, Entry: "read-it", Args: []string{"since", "--since"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := hostedEntry()
			e.Nodes[0].Trigger = &tc.trigger
			err := e.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("Validate accepted it")
			}
		})
	}
}

func keysOf(m map[string]Hosted) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
