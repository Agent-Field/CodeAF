package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The derivation law, as tests.
//
// A settings row exists because something in the build reads it. That sentence
// used to be a convention and the convention drifted: two rows shipped for
// learning loops that were "landing soon" and were still the only readers of
// their own values a wave later, and the models group carried a hand-copied
// slot list that had gone out of step with the roles table in both directions
// at once — a row for a thing that is not a slot, no row for two things that
// are. Both are the same failure and this file is the gate against it.

// Every role the router has gets a row, and every row of the models group that
// binds a role binds one the router actually has. A sixth role appears on the
// sheet without anyone editing internal/config; a role deleted from the table
// takes its row with it.
func TestModelRowsCoverExactlyTheRoutersRoles(t *testing.T) {
	rows := registry(t, t.TempDir())

	bound := map[store.ModelRole]string{}
	for _, row := range rows.Rows() {
		if row.Kind != SettingModel {
			continue
		}
		slot, ok := ModelSlotFor(row.Slot)
		if !ok {
			t.Fatalf("model row %q fronts slot %q, which ModelSlots does not know", row.Key, row.Slot)
		}
		if slot.Role == "" {
			continue
		}
		if !slot.Role.Valid() {
			t.Fatalf("model row %q binds %q, which is not one of the router's roles", row.Key, slot.Role)
		}
		if previous, seen := bound[slot.Role]; seen {
			t.Fatalf("role %q has two rows: %q and %q — one binding, one door (8.2.16)",
				slot.Role, previous, row.Key)
		}
		bound[slot.Role] = row.Key
	}

	for _, role := range store.ModelRoles() {
		if bound[role] == "" {
			t.Fatalf("role %q routes calls and has no row: the sheet cannot say what it runs on", role)
		}
	}
	if len(bound) != len(store.ModelRoles()) {
		t.Fatalf("bound %d roles, the router has %d", len(bound), len(store.ModelRoles()))
	}
}

// Machinery names never reach a reader (14). Every role and every modality the
// models group is built from has a plain word written for it here, and the word
// is not the machinery's.
func TestEveryRoleAndModalityHasAPlainWord(t *testing.T) {
	for _, role := range store.ModelRoles() {
		word, written := roleWords[role]
		if !written || strings.TrimSpace(word) == "" {
			t.Fatalf("role %q has no plain word — it would reach the sheet as %q", role, role)
		}
		if word == string(role) {
			t.Fatalf("role %q is spelled the way the journal spells it", role)
		}
	}
	for _, modality := range mediaModalities {
		if strings.TrimSpace(mediaSlotWords[modality]) == "" {
			t.Fatalf("modality %q has no plain word", modality)
		}
	}
}

// Every row must belong to a group the sheet renders, carry the plain language
// a reader needs, and be readable. A row nobody can read is a row that shows a
// blank and calls it a value.
func TestEveryRowIsGroupedNamedAndReadable(t *testing.T) {
	rows := registry(t, t.TempDir())
	groups := map[string]bool{}
	for _, title := range SettingCategories {
		groups[title] = true
	}
	for _, row := range rows.Rows() {
		if !groups[row.Category] {
			t.Fatalf("row %q sits in %q, which the sheet does not render", row.Key, row.Category)
		}
		if strings.TrimSpace(row.Label) == "" || strings.TrimSpace(row.Hint) == "" {
			t.Fatalf("row %q is missing plain language", row.Key)
		}
		if row.read == nil {
			t.Fatalf("row %q has no reader", row.Key)
		}
		if row.Value() == "" {
			t.Fatalf("row %q reads as nothing at all", row.Key)
		}
	}
}

// settingReaders names, for every persisted row, the identifier that proves
// somebody outside internal/config actually uses the value.
//
// It is a table and it is deliberately a table: a row is a promise that turning
// this changes something, and the cheapest way to keep that promise honest is
// to make the person adding a row write down who reads it. A key resolved into
// a [Config] field and then never read again passes no grep for its own key —
// which is exactly how practice_demand_pct and propose_new_skills survived a
// wave apiece — so what is named here is the thing the reader touches, not the
// spelling of the key.
var settingReaders = map[string]string{
	KeyWork:           "workMode",
	KeyDailyBudget:    "DailyBudgetUSD",
	KeyPlanConsent:    "PlanConsentUSD",
	KeyPracticeBudget: "PracticeBudgetUSD",
	KeyPracticeIdle:   "PracticeIdle",
	KeyBriefAfter:     "BriefAfter",
	KeyTenureAfter:    "AFORGE_TENURE_AFTER",
	KeyDocumentEngine: "DocumentEngine",
	KeyVisionModel:    "VisionModel",
	KeyAttribution:    "Attribution",
	KeyLinearMode:     "LinearModeAt",
	KeyRailState:      "RailStateAt",
	// The v3 task column names its own accessor, which the surface reads at boot
	// and writes back through SaveTaskColumn every time ctrl+g moves the column
	// (internal/tui3's task.go).
	KeyTaskColumn:  "TaskColumnAt",
	KeyQuickSwitch: "QuickSwitchAt",
	// The hints row names its accessor too: the surface reads it at boot and at
	// every turn end, beside the mouse row (internal/tui3's notice.go).
	KeyHints:    "HintsAt",
	KeyNerdFont: "NerdFontChosenAt",
	// The two surface rows name their own KEY, because that is now what the
	// far side touches: they resolve through the project layer
	// (ProjectBoolAt), which takes the row by name and calls
	// HistoryEnabledAt/DraftPersistAt itself as the profile rung. Naming the
	// resolver instead would let both rows be proven by one call site, which
	// is the one thing this table exists to prevent.
	KeyHistoryEnabled: "KeyHistoryEnabled",
	KeyDraftPersist:   "KeyDraftPersist",
	// The v3 session's rows name what READS the value on the far side, which
	// for these six is not a function in this package: the two approval rows
	// become the policy hung off session.Config.ApprovalPolicy, the ceiling is
	// session.Config.SpendRailUSD, and the tier and role rows are looked up by
	// internal/roles through the key spellings it owns.
	KeyToolApprovalMode: "ApprovalPolicy",
	KeyToolApprovals:    "ApprovalPolicy",
	// The bash rules row names the PARSER the door touches rather than the policy
	// the other two name: the list only reaches the gate if somebody read the text
	// into rules, and naming ApprovalPolicy a third time would let the two rows
	// above prove this one.
	KeyBashApprovals: "ParseBashApprovals",
	KeySpendRail:     "SpendRailUSD",
	// The guardian row is read by the v3 door and becomes session.Config.Guardian.
	// It names the accessor rather than the field, because the door is the only
	// caller and the accessor is what it touches.
	KeyGuardian: "GuardianEnabledAt",
	// The approval countdown is read by the chat surface itself, not by the
	// door: the clock runs on the frame clock beside the question it is counting
	// down (internal/tui3's consent.go), so what touches the value is this
	// accessor and nothing downstream of it.
	KeyConsentTimeout: "ConsentTimeoutAt",
	KeyMouse:          "MouseEnabledAt",
	// The timestamps row is read by the v3 surface itself — at boot and at every
	// turn end, beside the mouse and the gate posture — and becomes what the
	// transcript draws of the clock (internal/tui3's timestamps.go).
	KeyTimestamps: "TimestampsAt",
	// The routing row is read by the v3 door and becomes session.Config.Routing,
	// which the adapter turns into the preference object on every request
	// (internal/provider's velocity.go). It names the accessor the door touches,
	// which is the CHOICE reader rather than the resolved one: the door has to be
	// able to hand down "nobody wrote a word", because that is what lets the
	// adapter route a person's own turn by speed and an errand by price while an
	// actual word still wins. RoutingAt is the settings sheet's read of the same
	// row and stays what it always was.
	KeyRouting: "RoutingChoiceAt",
	// The fallback chain is read by the v3 door and becomes
	// session.Config.ModelFallbacks, which the adapter walks when no endpoint
	// serving the session's model will accept the request at all
	// (internal/provider's endpoints.go). It names the parser the door touches,
	// because that is the only call site and the accessor alone would not prove
	// the list ever reaches a request.
	KeyModelFallbacks: "ParseModelFallbacks",
	KeyTaskAudit:      "TaskAuditEnabledAt",
	// The reply guard is read by the v3 door and becomes the OFF half of
	// session.Config's ReplyGuardOff, which the session's completer stamps onto
	// every request as internal/provider's WithoutBabbleGuard. It names the
	// accessor the door touches.
	KeyReplyGuard: "ReplyGuardEnabledAt",
	// And the row beside it, read at the same moment and into the same struct:
	// the launch resolves it onto session.Config.TaskSettle, which is the one
	// thing that decides whether a landing nobody could check asks the person or
	// the model (internal/session's task_run.go).
	KeyTaskSettle: "TaskSettleAt",
	// The starting-a-task row names its own accessor, which the v3 surface calls
	// at the moment `/task` is typed rather than at boot (internal/tui3's
	// taskcommand.go): the row decides what one command does, so it is read when
	// the command is run and never resolved into a session field.
	KeyTaskStart:     "TaskStartAt",
	KeyMemoryEnabled: "MemoryEnabledAt",
	// The task countdown names the field it becomes on the far side —
	// session.Config's TaskAutoApproveSeconds, which task.go reads when it puts
	// a deadline on a proposal — for the reason the spend rail names its own:
	// the door resolves the row and the session is what touches the value.
	KeyTaskAutoApprove: "TaskAutoApproveSeconds",
	// The repair-round count names its own accessor, which the v3 door calls to
	// fill session.Config's TaskRepairRounds — the number task_audit.go's loop
	// counts its rounds against.
	KeyTaskRepairRounds: "TaskRepairRoundsAt",
	// The three throttle rows are read by the v3 door and become
	// session.Config's TaskParallel, TaskMaxLoad and TaskMinFreeMB — the cap the
	// frontier gates on and the two machine readings its admission governor asks
	// about (internal/session's task_run.go and task_pressure.go). Each names its
	// own accessor rather than the door that calls all three, so a row that stops
	// being read cannot be proven by its neighbours' call site.
	KeyTaskParallel:  "TaskParallelAt",
	KeyTaskMaxLoad:   "TaskMaxLoadAt",
	KeyTaskMinFreeMB: "TaskMinFreeMBAt",
	// The task model is read by the v3 door and becomes session.Config.TaskModel,
	// which a proposal that names no model of its own resolves through
	// (internal/session's taskmodel.go). It names the accessor the door touches,
	// as the guardian and the countdown rows do.
	KeyTaskModel: "TaskModelAt",
	// The worker roster is read once, at the top of cmd/aforge's run(), by the
	// function that registers this build's workers — before any command has read
	// a flag, because registration is what puts a worker in front of everything
	// that could choose one. It names its own accessor, as the throttle rows do.
	KeyWorkers:       "WorkersAt",
	KeyTierLowModel:  "TierKey",
	KeyTierHighModel: "TierKey",
	// The mastermind row names the TIER rather than the shared spelling, for the
	// reason the reflex row below does: naming TierKey a third time would let the
	// low row prove this one. cmd/aforge's crew source and internal/tui3's
	// settings skin both reach it by this name.
	KeyTierMastermindModel: "TierMastermind",
	// The crew row is the four tier rows answered as one word, and what reads it
	// is /crew (internal/tui3's crew.go) — the row's own reader, named here
	// rather than the writer, because a row that could be set and never read
	// would be exactly the dial-wired-to-nothing this table exists to catch.
	KeyCrew: "CrewAt",
	// The reflex row names the ROLE that reaches it rather than TierKey, which
	// its two neighbours share: internal/reflex resolves [roles.RoleReflex] and
	// nothing else lands on this key, so naming the shared spelling a third
	// time would let the low row prove this one.
	KeyTierReflexModel: "RoleReflex",
	KeyModelRoles:      "PinKey",
	// The three web-search rows are read by the v3 door, which turns them into
	// the [search.Options] it resolves the session's pair from
	// (cmd/aforge/chatv3.go's v3SearchOptions). Each names its own reader
	// rather than the mapping they share, so a row that stops being read
	// cannot be proven by its neighbours' call site.
	KeySearchProvider: "SearchProviderAt",
	KeyAPIKey:         "APIKeyAt",
	KeyExaKey:         "ExaKeyAt",
	KeyJinaKey:        "JinaKeyAt",
	// The Google pair is read by the v3 door, which turns it into the manager
	// hung off session.Config.Connect (cmd/aforge/chatv3.go's v3Connect). Both
	// rows name the same reader because the reader answers both halves at once:
	// an id without its secret connects nothing, so nothing in this tree ever
	// reads one of them alone.
	KeyGoogleOAuthClient: "GoogleOAuthClientAt",
	KeyGoogleOAuthSecret: "GoogleOAuthClientAt",
	KeySlackOAuthClient:  "SlackOAuthClientAt",
	// The context law's knobs are read live by ctxbudget on every call — the
	// environment name is the reader, as with tenure; Load seeds the
	// persisted half through ctxbudget.Configure.
	KeyContextFill:       "AFORGE_CONTEXT_FILL_PCT",
	KeyCompletionReserve: "AFORGE_COMPLETION_RESERVE",
	KeyWorkingSet:        "AFORGE_WORKING_SET",
	KeyContextReuse:      "AFORGE_CONTEXT_REUSE_PCT",
	// The install's rung on the effort ladder. The v3 door reads it into the
	// session's posture, from where the resolver hands it to every model call
	// that has nothing more specific to go on (internal/effort).
	KeyEffort: "DefaultEffortAt",
	// The four ssh rows resolve as one transport policy at the local --host
	// door, because passing four separately-read values to one process would
	// let a settings edit split one launch across two snapshots.
	KeySSHControlPersist: "SSHTransportAt",
	KeySSHServerAlive:    "SSHTransportAt",
	KeySSHServerMisses:   "SSHTransportAt",
	KeySSHIPQoS:          "SSHTransportAt",
	// The lane rows are read by the v3 surface: the picker opens on the pin so
	// that the machine you are held to is the marked row, and the `auto` row
	// says whether a slow answer will be rescued (internal/tui3's palette.go
	// and lanes.go). The chooser reads the pin too, through the same reader.
	LaneSettingKey(LaneSlotTalk): "LaneAt",
	KeyLaneGuard:                 "LaneGuardAt",
}

// Every persisted row names a reader, and every named reader is really there.
func TestEveryPersistedRowHasANamedReaderThatExists(t *testing.T) {
	root := repositoryRoot(t)
	rows := registry(t, t.TempDir())

	named := map[string]bool{}
	for _, row := range rows.Rows() {
		if row.Kind == SettingModel || row.PrefsField != "" {
			// The model slots and the divider live beside the graph, in the
			// chat prefs file, and their readers are the engine's own.
			continue
		}
		reader, written := settingReaders[row.Key]
		if !written {
			t.Fatalf("row %q names no reader: add it to settingReaders, or delete the row "+
				"if nothing in this build reads the value (5.20)", row.Key)
		}
		named[row.Key] = true
		if !readOutsideConfig(t, root, reader) {
			t.Fatalf("row %q claims %q reads it, and nothing outside internal/config does — "+
				"the row is a dial wired to nothing", row.Key, reader)
		}
	}
	for key := range settingReaders {
		if !named[key] {
			t.Fatalf("settingReaders names %q, which is not a row any more", key)
		}
	}
}

// readOutsideConfig reports whether an identifier appears in a non-test Go file
// anywhere but internal/config itself.
func readOutsideConfig(t *testing.T, root, identifier string) bool {
	t.Helper()
	needle := regexp.MustCompile(regexp.QuoteMeta(identifier))
	found := false
	walkGoFiles(t, root, func(path string, data []byte) {
		if found || strings.Contains(path, string(os.PathSeparator)+"config"+string(os.PathSeparator)) {
			return
		}
		if needle.Match(data) {
			found = true
		}
	})
	return found
}

func walkGoFiles(t *testing.T, root string, visit func(path string, data []byte)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "swepro":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		visit(path, data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
