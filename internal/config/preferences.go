package config

import (
	"encoding/json"
	"os"
	"time"
)

// PreferenceKey is the explicit boundary between a person's choices and this
// computer's facts. New settings stay local until deliberately classified.
func PreferenceKey(key string) bool {
	switch key {
	case KeyDailyBudget, KeyPlanConsent, KeySpendRail, KeyToolApprovalMode, KeyToolApprovals, KeyEffort, KeyTaskModel, KeyTaskAutoApprove, KeyTaskRepairRounds, KeyTaskSettle, KeyCrewAllowed, KeyCrewCap, KeyCrewTaskCap, KeyCrewFreeRoutes,
		KeyCrewRouteWorker, KeyCrewRoutePlanner, KeyCrewRouteChecker, KeyCrewProvidersOff,
		KeyGuardian, KeyConsentTimeout, KeyMouse, KeyTimestamps, KeyTaskColumn,
		KeyQuickSwitch, KeyHints, KeyWork, KeyIcons, KeyHistoryEnabled, KeyDraftPersist,
		KeyTaskAudit, KeyReplyGuard, KeyTaskStart, KeyMemoryEnabled, KeyPromptProfile,
		KeyTierLowModel, KeyTierHighModel, KeyTierWorkerModel, KeyTierReflexModel,
		KeyTierMastermindModel, KeyKnowledgeModel, KeyModelRoles, KeyModelFallbacks, KeyRouting,
		KeyVisionModel, KeyDocumentEngine, KeyAttributionModel, KeyContextFill,
		KeyCompletionReserve, KeyWorkingSet, KeyContextReuse:
		return true
	}
	for _, slot := range ModelSlots() {
		if key == ModelSettingKey(slot.Slot) {
			return true
		}
	}
	return false
}

// ReadPreferences reads stored values alone: operator environment pins and
// project settings never travel to another computer (C30.3).
func ReadPreferences(dir string) (map[string]string, error) {
	held, err := readProfileConfig(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range held {
		if PreferenceKey(k) {
			out[k] = string(v)
		}
	}
	return out, nil
}

// ApplyPreferences changes only the named settings inside the existing profile
// transaction, preserving paths, credentials and simultaneous local writes.
func ApplyPreferences(dir string, changes map[string]string) error {
	return ApplyPreferencesIf(dir, changes, nil)
}

// ApplyPreferencesIf preserves a setting changed after the sync reader took
// its snapshot. The next round captures that newer local edit (C30.3).
func ApplyPreferencesIf(dir string, changes, expected map[string]string) error {
	return editProfile(dir, "preferences", func(held map[string]json.RawMessage) (profileChange, error) {
		change := profileChange{set: map[string]json.RawMessage{}}
		for k, v := range changes {
			if expected != nil && string(held[k]) != expected[k] {
				continue
			}
			if !PreferenceKey(k) {
				continue
			}
			if v == "" {
				if _, ok := held[k]; ok {
					change.removed = append(change.removed, k)
				}
				continue
			}
			if !json.Valid([]byte(v)) {
				return profileChange{}, &json.SyntaxError{}
			}
			if string(held[k]) != v {
				change.set[k] = json.RawMessage(v)
			}
		}
		return change, nil
	})
}

// PreferenceTime reads the setting's edit clock, including a removal. Older
// profiles have no clocks; their file time is the only available evidence.
func PreferenceTime(dir, key string) time.Time {
	held, err := readProfileConfig(dir)
	if err != nil {
		return time.Time{}
	}
	var stamps map[string]int64
	_ = json.Unmarshal(held[preferenceTimesKey], &stamps)
	if at := stamps[key]; at != 0 {
		return time.Unix(0, at)
	}
	if info, err := os.Stat(BudgetConfigPath(dir)); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

const preferenceTimesKey = "sync.preference_times"

// stampPreferences places clocks inside the same atomic profile replacement,
// so a crash cannot separate a changed setting from its edit time (C30.3).
func stampPreferences(dir string, held map[string]json.RawMessage, change *profileChange) {
	var stamps map[string]int64
	_ = json.Unmarshal(held[preferenceTimesKey], &stamps)
	if stamps == nil {
		stamps = map[string]int64{}
	}
	// Preserve older profiles' only known edit clock before replacing their
	// file. Editing one setting must not make every unstamped setting newer.
	if info, err := os.Stat(BudgetConfigPath(dir)); err == nil {
		for key := range held {
			if PreferenceKey(key) && stamps[key] == 0 {
				stamps[key] = info.ModTime().UnixNano()
			}
		}
	}
	changed := false
	now := time.Now().UnixNano()
	for k, v := range change.set {
		if PreferenceKey(k) && string(v) != string(held[k]) {
			stamps[k] = now
			changed = true
		}
	}
	for _, k := range change.removed {
		if PreferenceKey(k) {
			stamps[k] = now
			changed = true
		}
	}
	if changed {
		if change.set == nil {
			change.set = map[string]json.RawMessage{}
		}
		change.set[preferenceTimesKey], _ = json.Marshal(stamps)
	}
}
