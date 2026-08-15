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
	KeyNerdFont:       "NerdFontChosenAt",
	KeyHistoryEnabled: "HistoryEnabledAt",
	KeyDraftPersist:   "DraftPersistAt",
	// The v3 session's rows name what READS the value on the far side, which
	// for these six is not a function in this package: the two approval rows
	// become the policy hung off session.Config.ApprovalPolicy, the ceiling is
	// session.Config.SpendRailUSD, and the tier and role rows are looked up by
	// internal/roles through the key spellings it owns.
	KeyToolApprovalMode: "ApprovalPolicy",
	KeyToolApprovals:    "ApprovalPolicy",
	KeySpendRail:        "SpendRailUSD",
	KeyTierLowModel:     "TierKey",
	KeyTierHighModel:    "TierKey",
	KeyModelRoles:       "PinKey",
	// The context law's knobs are read live by ctxbudget on every call — the
	// environment name is the reader, as with tenure; Load seeds the
	// persisted half through ctxbudget.Configure.
	KeyContextFill:       "AFORGE_CONTEXT_FILL_PCT",
	KeyCompletionReserve: "AFORGE_COMPLETION_RESERVE",
	KeyWorkingSet:        "AFORGE_WORKING_SET",
	KeyContextReuse:      "AFORGE_CONTEXT_REUSE_PCT",
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
