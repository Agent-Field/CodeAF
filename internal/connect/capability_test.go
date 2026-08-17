package connect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// withCapabilities replaces the capability registry for one test, so that a
// declaration a test invents cannot outlive it. It is [withPlugs] for the other
// registry.
func withCapabilities(t *testing.T, declare func()) {
	t.Helper()
	capabilityMu.Lock()
	saved := capabilityRegistry
	capabilityRegistry = map[string]capabilitySet{}
	capabilityMu.Unlock()
	t.Cleanup(func() {
		capabilityMu.Lock()
		capabilityRegistry = saved
		capabilityMu.Unlock()
	})
	declare()
}

// settingsPath names the answers file beside a manager's connections.
func settingsPath(directory string) string {
	return filepath.Join(directory, CapabilityFileName)
}

func TestCapabilitiesAreTheDeclarationsOrder(t *testing.T) {
	manager, _ := testManager(t)

	var ids []string
	for _, capability := range manager.Capabilities("google") {
		ids = append(ids, capability.ID)
		if strings.TrimSpace(capability.Phrase) == "" {
			t.Errorf("%s has no sentence a person can read", capability.ID)
		}
	}
	// The declaration's order, which is not the alphabet's — an alphabetical
	// list would open with the calendar.
	want := "mail-read,mail-send,calendar-read,calendar-write"
	if got := strings.Join(ids, ","); got != want {
		t.Errorf("Capabilities(google): got %q, want %q", got, want)
	}

	// Asking twice answers the same, and a caller who reorders what they were
	// handed cannot reorder it for anybody else.
	handed := manager.Capabilities("google")
	handed[0] = Capability{ID: "meddled"}
	if manager.Capabilities("google")[0].ID != "mail-read" {
		t.Errorf("the declaration must survive a caller writing into what it handed out")
	}

	// An id nobody declared answers nothing rather than inventing a row.
	if got := manager.Capabilities("nothing-like-this"); len(got) != 0 {
		t.Errorf("an unknown service must list nothing, got %+v", got)
	}
}

// THE DEFAULTS ARE TODAY'S BEHAVIOUR: what only looks already runs, what acts is
// already asked about, and nothing is missing from the belt.
func TestDefaultsAreTodaysBehaviour(t *testing.T) {
	manager, directory := testManager(t)

	for _, capability := range manager.Capabilities("google") {
		want := StateYes
		if capability.Acts {
			want = StateAsk
		}
		if got := manager.CapabilityState("google", capability.ID); got != want {
			t.Errorf("%s: got %q, want %q", capability.ID, got, want)
		}
	}
	// The two that act are the two internal/approval already holds a floor
	// under, and they are the only two.
	for _, id := range []string{"mail-send", "calendar-write"} {
		if manager.CapabilityState("google", id) != StateAsk {
			t.Errorf("%s acts in the person's name and must default to being asked about", id)
		}
	}
	if _, err := os.Stat(settingsPath(directory)); !os.IsNotExist(err) {
		t.Errorf("reading defaults must write nothing, err = %v", err)
	}
}

// A capability nobody declared is off, and that reading is deliberately not the
// one an unmapped tool gets.
func TestAnUndeclaredCapabilityIsOff(t *testing.T) {
	manager, _ := testManager(t)

	if got := manager.CapabilityState("google", "read-their-diary"); got != StateOff {
		t.Errorf("an undeclared capability: got %q, want %q", got, StateOff)
	}
	if got := manager.CapabilityState("nothing-like-this", "mail-read"); got != StateOff {
		t.Errorf("an undeclared service: got %q, want %q", got, StateOff)
	}
	if got := manager.CapabilityState("google", ""); got != StateOff {
		t.Errorf("the empty capability: got %q, want %q", got, StateOff)
	}
}

func TestSetAndGetRoundTrip(t *testing.T) {
	manager, directory := testManager(t)

	answers := map[string]CapabilityState{
		"mail-read":     StateOff,
		"mail-send":     StateYes,
		"calendar-read": StateAsk,
	}
	for id, state := range answers {
		if err := manager.SetCapabilityState("google", id, state); err != nil {
			t.Fatalf("SetCapabilityState(%s, %s): %v", id, state, err)
		}
	}
	for id, state := range answers {
		if got := manager.CapabilityState("google", id); got != state {
			t.Errorf("%s: got %q, want %q", id, got, state)
		}
	}
	// The one nobody touched still reads as the default.
	if got := manager.CapabilityState("google", "calendar-write"); got != StateAsk {
		t.Errorf("calendar-write: got %q, want %q", got, StateAsk)
	}

	// A person's answers outlive the process that took them.
	next, err := NewManager(directory, map[string]ClientCredential{
		"google": {ID: "client-id", Secret: "client-secret"},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	for id, state := range answers {
		if got := next.CapabilityState("google", id); got != state {
			t.Errorf("after reopening, %s: got %q, want %q", id, got, state)
		}
	}

	// The ids are read the way every other id in this package is read.
	if got := next.CapabilityState("Google", " MAIL-READ "); got != StateOff {
		t.Errorf("a shouted id must find the same answer, got %q", got)
	}
}

// THE EMPTINESS LAW ON DISK: only what somebody actually said is written down,
// and undoing every answer leaves the machine as it started.
func TestOnlyWhatSomebodySaidReachesTheDisk(t *testing.T) {
	manager, directory := testManager(t)
	path := settingsPath(directory)

	// Setting a capability to what it already said is not a decision.
	if err := manager.SetCapabilityState("google", "mail-read", StateYes); err != nil {
		t.Fatalf("SetCapabilityState: %v", err)
	}
	if err := manager.SetCapabilityState("google", "mail-send", StateAsk); err != nil {
		t.Fatalf("SetCapabilityState: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("writing the defaults must leave no file, err = %v", err)
	}

	// One real decision writes one real line.
	if err := manager.SetCapabilityState("google", "mail-send", StateYes); err != nil {
		t.Fatalf("SetCapabilityState: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var written map[string]map[string]string
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatalf("the settings file must be readable JSON: %v", err)
	}
	if len(written) != 1 || len(written["google"]) != 1 || written["google"]["mail-send"] != "yes" {
		t.Errorf("only the decision belongs on disk, got %s", raw)
	}

	// The policy is not a secret, so it is readable.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o644 {
		t.Errorf("settings file mode: got %04o, want 0644", mode)
	}

	// Taking the decision back takes the file with it.
	if err := manager.SetCapabilityState("google", "mail-send", StateAsk); err != nil {
		t.Fatalf("SetCapabilityState: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("forgetting the last answer must leave no file, err = %v", err)
	}
	if got := manager.CapabilityState("google", "mail-send"); got != StateAsk {
		t.Errorf("mail-send: got %q, want %q", got, StateAsk)
	}
}

// The answers file is not the credentials file, and writing one must never
// touch the other.
func TestTheSettingsFileIsNotTheKeysFile(t *testing.T) {
	if CapabilityFileName == StoreFileName {
		t.Fatalf("the policy and the keys must not share a file")
	}
	manager, directory := testManager(t)
	if err := manager.SetCapabilityState("google", "mail-read", StateOff); err != nil {
		t.Fatalf("SetCapabilityState: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, StoreFileName)); !os.IsNotExist(err) {
		t.Errorf("saying something about a capability must not write a keys file, err = %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".connections-") {
			t.Errorf("a temporary file was left behind: %s", entry.Name())
		}
	}
}

// A DAMAGED FILE IS ALL DEFAULTS, NEVER AN ERROR — whole, and cell by cell.
func TestADamagedFileReadsAsDefaults(t *testing.T) {
	cases := []struct {
		name    string
		content string
		// survives is the one answer that must still be honoured, if any.
		survives CapabilityState
	}{
		{"not JSON at all", "{ this is not", ""},
		{"the wrong shape", `{"google":"yes"}`, ""},
		{"a word nobody recognises", `{"google":{"mail-read":"always"}}`, ""},
		{"one bad word beside a good one", `{"google":{"mail-read":"always","mail-send":"off"}}`, StateOff},
	}
	for _, c := range cases {
		directory := t.TempDir()
		if err := os.WriteFile(settingsPath(directory), []byte(c.content), 0o644); err != nil {
			t.Fatalf("%s: write: %v", c.name, err)
		}
		manager, err := NewManager(directory, map[string]ClientCredential{
			"google": {ID: "client-id", Secret: "client-secret"},
		})
		if err != nil {
			t.Fatalf("%s: a damaged settings file must never block a manager: %v", c.name, err)
		}
		if len(manager.Capabilities("google")) != 4 {
			t.Errorf("%s: the panel must still have its rows", c.name)
		}
		if got := manager.CapabilityState("google", "mail-read"); got != StateYes {
			t.Errorf("%s: mail-read got %q, want the default %q", c.name, got, StateYes)
		}
		want := StateAsk
		if c.survives != "" {
			want = c.survives
		}
		if got := manager.CapabilityState("google", "mail-send"); got != want {
			t.Errorf("%s: mail-send got %q, want %q", c.name, got, want)
		}
	}
}

// EVERY TOOL THE GOOGLE FAMILY ARMS IS OWNED BY A CAPABILITY. A tool missing
// from the map is a tool no control on the panel can reach.
func TestToolCapabilityCoversTheWholeFamily(t *testing.T) {
	manager, _ := testManager(t)

	family := map[string]string{
		"gmail_search":    "mail-read",
		"gmail_read":      "mail-read",
		"gmail_send":      "mail-send",
		"calendar_list":   "calendar-read",
		"calendar_create": "calendar-write",
	}
	for tool, want := range family {
		if got := manager.ToolCapability("google", tool); got != want {
			t.Errorf("ToolCapability(google, %s): got %q, want %q", tool, got, want)
		}
	}
	if len(googleTools) != len(family) {
		t.Errorf("the map declares %d tools, the family arms %d", len(googleTools), len(family))
	}

	// NO CAPABILITY IS NOT AN OFF ONE: these answer empty, and the wiring
	// wave must read that as "judged as it is today".
	for _, absent := range []string{"bash", "services", "use_service", ""} {
		if got := manager.ToolCapability("google", absent); got != "" {
			t.Errorf("ToolCapability(google, %q): got %q, want empty", absent, got)
		}
	}
	if got := manager.ToolCapability("nothing-like-this", "gmail_send"); got != "" {
		t.Errorf("an unknown service owns no tool, got %q", got)
	}
}

func TestSetRefusesWhatNobodyDeclared(t *testing.T) {
	manager, directory := testManager(t)

	cases := []struct {
		name       string
		service    string
		capability string
		state      CapabilityState
	}{
		{"a word that is not one of the three", "google", "mail-read", CapabilityState("always")},
		{"the empty answer", "google", "mail-read", CapabilityState("")},
		{"a capability nobody declared", "google", "read-their-diary", StateOff},
		{"a service nobody declared", "nothing-like-this", "mail-read", StateOff},
	}
	for _, c := range cases {
		err := manager.SetCapabilityState(c.service, c.capability, c.state)
		if err == nil {
			t.Errorf("%s: must be refused", c.name)
			continue
		}
		lowered := strings.ToLower(err.Error())
		for _, word := range []string{"oauth", "token", "bearer", "grant"} {
			if strings.Contains(lowered, word) {
				t.Errorf("%s: %q says %q, which is machinery vocabulary", c.name, err, word)
			}
		}
	}
	if _, err := os.Stat(settingsPath(directory)); !os.IsNotExist(err) {
		t.Errorf("a refused answer must write nothing, err = %v", err)
	}
}

// The read/act pair a key-based service gets, and the door it comes through.
func TestGenericCapabilitiesAreTheReadActPair(t *testing.T) {
	pair := genericCapabilities()
	if len(pair) != 2 || pair[0].ID != "read" || pair[1].ID != "act" {
		t.Fatalf("the generic pair: got %+v", pair)
	}
	if pair[0].Acts || !pair[1].Acts {
		t.Errorf("only the second half of the pair acts in the person's name: %+v", pair)
	}

	withCapabilities(t, func() {
		RegisterGenericCapabilities("ledger", map[string]string{
			"ledger_search": "read",
			"ledger_post":   "act",
		})
	})
	manager, _ := testManager(t)

	if got := len(manager.Capabilities("ledger")); got != 2 {
		t.Fatalf("a generic service must have the two rows, got %d", got)
	}
	if got := manager.CapabilityState("ledger", "read"); got != StateYes {
		t.Errorf("read: got %q, want %q", got, StateYes)
	}
	if got := manager.CapabilityState("ledger", "act"); got != StateAsk {
		t.Errorf("act: got %q, want %q", got, StateAsk)
	}
	if got := manager.ToolCapability("ledger", "ledger_post"); got != "act" {
		t.Errorf("ToolCapability(ledger, ledger_post): got %q", got)
	}
	if err := manager.SetCapabilityState("ledger", "act", StateYes); err != nil {
		t.Errorf("SetCapabilityState on a generic service: %v", err)
	}
	if got := manager.CapabilityState("ledger", "act"); got != StateYes {
		t.Errorf("act after being set: got %q, want %q", got, StateYes)
	}
}

// A declaration that cannot be honoured fails at registration, where somebody is
// looking, rather than silently later as a missing row.
func TestRegisterRefusesADeclarationThatCannotBeHonoured(t *testing.T) {
	cases := []struct {
		name    string
		declare func()
	}{
		{"no service", func() { RegisterCapabilities("  ", googleCapabilities, nil) }},
		{"a capability with no id", func() {
			RegisterCapabilities("ledger", []Capability{{Phrase: "do things"}}, nil)
		}},
		{"the same capability twice", func() {
			RegisterCapabilities("ledger", []Capability{{ID: "read"}, {ID: "read"}}, nil)
		}},
		{"a tool pointing at nothing", func() {
			RegisterCapabilities("ledger", []Capability{{ID: "read"}}, map[string]string{"ledger_post": "act"})
		}},
		{"the same service twice", func() {
			RegisterCapabilities("ledger", []Capability{{ID: "read"}}, nil)
			RegisterCapabilities("ledger", []Capability{{ID: "read"}}, nil)
		}},
	}
	for _, c := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: must panic at registration", c.name)
				}
			}()
			withCapabilities(t, c.declare)
		}()
	}
}

// Two managers over one profile, answering and being asked at once. The lock is
// on the FILE, not on the manager, so neither can lose what the other said.
func TestAnswersSurviveBeingGivenAtOnce(t *testing.T) {
	manager, directory := testManager(t)
	other, err := NewManager(directory, map[string]ClientCredential{
		"google": {ID: "client-id", Secret: "client-secret"},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	var wait sync.WaitGroup
	for _, pair := range []struct {
		manager    *Manager
		capability string
		state      CapabilityState
	}{
		{manager, "mail-read", StateOff},
		{other, "mail-send", StateYes},
		{manager, "calendar-read", StateAsk},
		{other, "calendar-write", StateOff},
	} {
		wait.Add(2)
		go func() {
			defer wait.Done()
			for i := 0; i < 40; i++ {
				if err := pair.manager.SetCapabilityState("google", pair.capability, pair.state); err != nil {
					t.Errorf("SetCapabilityState(%s): %v", pair.capability, err)
					return
				}
			}
		}()
		go func() {
			defer wait.Done()
			for i := 0; i < 40; i++ {
				_ = pair.manager.CapabilityState("google", pair.capability)
				_ = pair.manager.Capabilities("google")
				_ = pair.manager.ToolCapability("google", "gmail_send")
			}
		}()
	}
	wait.Wait()

	for _, want := range []struct {
		capability string
		state      CapabilityState
	}{
		{"mail-read", StateOff},
		{"mail-send", StateYes},
		{"calendar-read", StateAsk},
		{"calendar-write", StateOff},
	} {
		if got := manager.CapabilityState("google", want.capability); got != want.state {
			t.Errorf("%s: got %q, want %q", want.capability, got, want.state)
		}
	}
}
