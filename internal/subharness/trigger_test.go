package subharness

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// fakeSource remembers what it was asked to mount. It keys by command line
// because that is what a real source would key by: two harnesses claiming one
// line is the collision that matters, and a map is the assertion.
type fakeSource struct {
	mounted map[string]Hosted
	fail    error
}

func newSource() *fakeSource { return &fakeSource{mounted: map[string]Hosted{}} }

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

// hosted is the shape the trigger slice is about: a hosted trigger whose
// successor is an agent.loop, with an allowed-args whitelist.
func hosted(name string) Harness {
	return Harness{
		Id: Id{Name: name, Desc: "sort the inbox", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "start", Kind: KindTrigger, Fields: Fields{
					"source": TriggerHosted, "args": "since, label",
				}},
				{Id: "read-it", Kind: KindAgentLoop, Fields: Fields{
					"brief": "read the inbox and say what is in it", "tools": "read",
				}},
			},
			Edges: []Edge{{"start", "read-it"}},
		},
		Whitelist: []string{"read"},
	}
}

func TestHostMountsTheCommandAndSaysWhereItEnters(t *testing.T) {
	src := newSource()
	mounted, err := Host(src, hosted("triage"))
	if err != nil {
		t.Fatal(err)
	}
	if len(mounted) != 1 {
		t.Fatalf("mounted %d triggers, want 1", len(mounted))
	}
	if CommandFor("triage") != "/harness triage" {
		t.Fatalf("the hosted command is %q", CommandFor("triage"))
	}
	one, ok := src.mounted["/harness triage"]
	if !ok {
		t.Fatalf("the source was never handed the command: %v", src.mounted)
	}
	if one.Harness != "triage" || one.Version != 1 {
		t.Errorf("hosted %q at v%d, want triage at v1", one.Harness, one.Version)
	}
	// THE ENTRY IS THE TRIGGER'S SUCCESSOR, read off the edges rather than out
	// of a field that could disagree with them.
	if one.Entry != "read-it" || one.Node != "start" {
		t.Errorf("hosted entry %q from node %q, want read-it from start", one.Entry, one.Node)
	}
	if one.Desc != "sort the inbox" {
		t.Errorf("hosted desc %q, want the harness's own", one.Desc)
	}
	if strings.Join(one.AllowedArgs, ",") != "since,label" {
		t.Errorf("allowed args %v, want the trigger's whitelist", one.AllowedArgs)
	}
}

// A harness started by a watch mounts nothing, and that is not a failure.
func TestAHarnessWithNoHostedTriggerMountsNothing(t *testing.T) {
	h := hosted("watched")
	h.Program.Nodes[0].Fields = Fields{"source": TriggerWatch, "spec": "bench/nightly.log"}
	src := newSource()
	mounted, err := Host(src, h)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounted) != 0 || len(src.mounted) != 0 {
		t.Fatalf("a watch trigger mounted %v", src.mounted)
	}
}

func TestHostRefusesToMountAHarnessThatWouldNotRun(t *testing.T) {
	h := hosted("broken")
	h.Whitelist = nil // the agent.loop now hands out a tool nobody allowed
	if _, err := Host(newSource(), h); err == nil {
		t.Fatal("a harness that does not validate was offered to a source anyway")
	}
	if _, err := Host(nil, hosted("triage")); err == nil {
		t.Fatal("hosting on no source at all was accepted")
	}
}

// WHAT A PERSON TYPES IS A SENTENCE, AND A SENTENCE NEEDS A READER.
func TestAHostedTriggerMustEnterAtAnAgentLoop(t *testing.T) {
	h := hosted("triage")
	h.Program.Nodes[1] = Node{Id: "read-it", Kind: KindToolCall, Fields: Fields{"tool": "read"}}
	err := Validate(h)
	if err == nil || !strings.Contains(err.Error(), "hosted as a command") {
		t.Fatalf("a hosted trigger into a tool.call gave %v", err)
	}
}

func TestATriggerMayNotAllowOneArgumentTwice(t *testing.T) {
	h := hosted("triage")
	h.Program.Nodes[0].Fields["args"] = "since, --since=today"
	if err := Validate(h); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("a repeated argument gave %v", err)
	}
}

// The whitelist is about which arguments EXIST; a value is the caller's
// business, so `since` and `--since=today` are one argument.
func TestAnArgumentIsJudgedByItsName(t *testing.T) {
	one := Hosted{Command: "/harness triage", AllowedArgs: []string{"since", "label"}}
	for _, arg := range []string{"since", "--since=yesterday", "label=bug"} {
		if !one.Allows(arg) {
			t.Errorf("%q is not allowed and should be", arg)
		}
	}
	if one.Allows("--rm") {
		t.Error("an argument nobody whitelisted is allowed")
	}
	if err := one.Accepts([]string{"since=today", "label=bug"}); err != nil {
		t.Errorf("a whitelisted list was refused: %v", err)
	}
	err := one.Accepts([]string{"--rm"})
	if err == nil || !strings.Contains(err.Error(), "does not take") {
		t.Fatalf("an unknown argument gave %v", err)
	}
	none := Hosted{Command: "/harness quiet"}
	if err := none.Accepts([]string{"anything"}); err == nil ||
		!strings.Contains(err.Error(), "takes no arguments") {
		t.Fatalf("an empty whitelist granted %v", err)
	}
}

// HostAll is the boot path: the registry as it stands on disk.
func TestHostAllMountsEveryRegisteredHarnessHead(t *testing.T) {
	store := At(t.TempDir())
	for _, name := range []string{"triage", "sweep"} {
		if _, err := store.Save(hosted(name)); err != nil {
			t.Fatal(err)
		}
	}
	// A second version of one of them is what a command must resolve to.
	second := hosted("triage")
	second.Id.Version, second.Id.Desc = 2, "sort the inbox, better"
	if _, err := store.Save(second); err != nil {
		t.Fatal(err)
	}

	src := newSource()
	mounted, err := store.HostAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounted) != 2 {
		t.Fatalf("mounted %d commands, want 2", len(mounted))
	}
	one := src.mounted["/harness triage"]
	if one.Version != 2 || one.Desc != "sort the inbox, better" {
		t.Fatalf("the mounted command is v%d (%q), want the head", one.Version, one.Desc)
	}
}

// ONE BROKEN PAGE MUST NOT TAKE EVERY OTHER HARNESS'S COMMAND WITH IT.
func TestHostAllSkipsAPageItCannotUse(t *testing.T) {
	store := At(t.TempDir())
	if _, err := store.Save(hosted("triage")); err != nil {
		t.Fatal(err)
	}
	broken := hosted("broken")
	if _, err := store.Save(broken); err != nil {
		t.Fatal(err)
	}
	// Rewrite the saved page into one that will not validate on the way back.
	page := store.page("broken", 1)
	data, err := Encode(Harness{Id: Id{Name: "broken", Version: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, data, 0o644); err != nil {
		t.Fatal(err)
	}

	src := newSource()
	mounted, err := store.HostAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounted) != 1 || src.mounted["/harness triage"].Harness != "triage" {
		t.Fatalf("a broken page took the good one down with it: %v", src.mounted)
	}
}
