package config

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/roles"
)

const (
	AppDefaultEffort               = "medium"
	AppDefaultDayCap               = 20.0
	AppDefaultAgentBudget          = 5.0
	AppDefaultTaskCap              = CrewTaskCapDefault
	AppDefaultQuestionClockMinutes = 15
	AppDefaultIdleDays             = 7
	AppDefaultFactoryDoneDays      = 1
	KeyAppSetupStep                = "app.setup.step"
)

var ErrAppStale = errors.New("settings changed; read them again")

// AppRoleChoice is the app's small model vocabulary. Empty model follows the
// agent, rather than pinning every agent to the installation's default.
type AppRoleChoice struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

// appRoleEngines ports the desktop role groups, with only the two rows the
// app design exposes. Engine role names never become app labels.
var appRoleEngines = map[string][]roles.Role{
	"checks":  {roles.RoleAuditor, roles.RoleRepair},
	"planner": {roles.RolePlanner, roles.RoleDesigner, roles.RoleShaper},
}

type appSetting struct {
	native   string
	fallback any
}

// The codec uses existing engine keys wherever one exists. New defaults are
// persisted here for their owning engine lanes to consume through AppSettingsAt.
var appSettings = map[string]appSetting{
	"models.default":                    {KeyChatModel, ""},
	"models.effort":                     {KeyEffort, AppDefaultEffort},
	"models.pinned":                     {"app.models.pinned", []string{"", "", ""}},
	"models.roles.checks":               {"app.models.roles.checks", AppRoleChoice{"", AppDefaultEffort}},
	"models.roles.planner":              {"app.models.roles.planner", AppRoleChoice{"", AppDefaultEffort}},
	"spending.day_cap":                  {KeyDailyBudget, AppDefaultDayCap},
	"spending.per_agent":                {"app.spending.per_agent", AppDefaultAgentBudget},
	"spending.per_task":                 {KeyCrewTaskCap, AppDefaultTaskCap},
	"spending.per_conversation_ceiling": {KeySpendRail, 0.0},
	"memory.agents_may_add":             {"app.memory.agents_may_add", true},
	"permissions.mode":                  {"app.permissions.mode", "ask_risky"},
	"permissions.always_ask":            {"app.permissions.always_ask", []string{"acting in your name", "deleting outside a task", "payments"}},
	"permissions.question_clock_min":    {"app.permissions.question_clock_min", AppDefaultQuestionClockMinutes},
	"accounts.github":                   {"app.accounts.github", "ask_first"},
	"accounts.google":                   {"app.accounts.google", "ask_first"},
	"accounts.slack":                    {"app.accounts.slack", "ask_first"},
	"factory.runner_timeout_s":          {"app.factory.runner_timeout_s", factory.DefaultRunnerTimeoutSeconds},
	"factory.keep_output_days":          {"app.factory.keep_output_days", factory.DefaultKeepOutputDays},
	"factory.keep_output_mb":            {"app.factory.keep_output_mb", factory.DefaultKeepOutputMB},
	"factory.done_days":                 {"app.factory.done_days", AppDefaultFactoryDoneDays},
	"factory.polling_s":                 {"app.factory.polling_s", 60},
	"advanced.context_tokens":           {"app.advanced.context_tokens", 160000},
	"advanced.work_folder":              {"app.advanced.work_folder", "~/codeaf/work"},
	"advanced.proposal_s":               {KeyTaskAutoApprove, DefaultTaskAutoApprove},
	"advanced.idle_days":                {"app.advanced.idle_days", AppDefaultIdleDays},
	"crews.depth":                       {KeyTeamsDepthLimit, DefaultTeamsDepthLimit},
	"crews.share_pct":                   {KeyTeamsSubSharePct, DefaultTeamsSubSharePct},
	"crews.wake":                        {KeyTeamsWake, DefaultTeamsWake},
}

func appSettingFor(key string) (appSetting, bool) {
	if row, ok := appSettings[key]; ok {
		return row, true
	}
	if appAccountActionKey(key) {
		return appSetting{"app." + key, "ask_first"}, true
	}
	if strings.HasPrefix(key, "accounts.mcp:") && len(key) > len("accounts.mcp:") && len(key) <= 160 && !strings.ContainsAny(key, " \t\r\n\x00") {
		return appSetting{"app." + key, "ask_first"}, true
	}
	return appSetting{}, false
}

// AppSettingsAt returns only non-secret, binary-owned rows. The opaque version
// covers the whole profile so a conditional write also notices terminal edits.
func AppSettingsAt(profileDir string) (map[string]any, string, error) {
	held, err := readProfileConfig(profileDir)
	if err != nil {
		return nil, "", err
	}
	return appSettingsFrom(held), appProfileVersion(held), nil
}

func appSettingsFrom(held map[string]json.RawMessage) map[string]any {
	result := make(map[string]any, len(appSettings))
	keys := make([]string, 0, len(appSettings))
	for key := range appSettings {
		keys = append(keys, key)
	}
	for key := range held {
		if strings.HasPrefix(key, "app.accounts.mcp:") || appAccountActionKey(strings.TrimPrefix(key, "app.")) {
			keys = append(keys, strings.TrimPrefix(key, "app."))
		}
	}
	for _, key := range keys {
		row, _ := appSettingFor(key)
		value := row.fallback
		if raw, ok := held[row.native]; ok {
			var saved any
			if json.Unmarshal(raw, &saved) == nil {
				value = saved
			}
		}
		if key == "models.effort" {
			word, _ := value.(string)
			value = AppEffortWord(word)
		}

		if strings.HasPrefix(key, "models.roles.") {
			role := strings.TrimPrefix(key, "models.roles.")
			text := ""
			if raw, ok := held[KeyModelRoles]; ok {
				_ = json.Unmarshal(raw, &text)
			}
			pins, _ := ParseModelRoles(text)
			choice := AppRoleChoice{Effort: AppDefaultEffort}
			if pin := pins[string(appRoleEngines[role][0])]; pin != "" {
				choice.Model, choice.Effort = roles.SplitEffort(pin)
				if !roles.ValidEffort(choice.Effort) {
					choice.Effort = AppDefaultEffort
				}
			}
			value = choice
		}
		// A malformed saved row falls back before it reaches an HTTP codec.
		check := value
		if strings.HasPrefix(key, "spending.") {
			if amount, ok := value.(float64); ok {
				check = map[string]string{"usd": fmt.Sprintf("%.2f", amount)}
			}
		}
		encoded, _ := json.Marshal(check)
		if _, err := validateAppSetting(key, encoded); err != nil {
			value = row.fallback
		}
		if strings.HasPrefix(key, "spending.") {
			if amount, ok := value.(float64); ok {
				value = map[string]string{"usd": fmt.Sprintf("%.2f", amount)}
			}
		}
		result[key] = value
	}
	return result
}

func appProfileVersion(held map[string]json.RawMessage) string {
	raw, _ := json.Marshal(held)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// ApplyAppSettings validates the entire patch before the single shared writer
// changes anything. Role pins and their app rows land in the same transaction.
func ApplyAppSettings(profileDir string, patch map[string]json.RawMessage, expect string) error {
	return applyAppSettings(profileDir, patch, expect, nil)
}

func applyAppSettings(profileDir string, patch map[string]json.RawMessage, expect string, extra map[string]any) error {
	return transformProfileValues(profileDir, func(held map[string]json.RawMessage) (map[string]any, error) {
		if expect != "" && expect != appProfileVersion(held) {
			return nil, ErrAppStale
		}
		updates := make(map[string]any)
		keys := make([]string, 0, len(patch))
		for key := range patch {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			row, ok := appSettingFor(key)
			if !ok {
				return nil, fmt.Errorf("unknown binary setting %q", key)
			}
			value, err := validateAppSetting(key, patch[key])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			updates[row.native] = value
		}
		if err := appRolePins(held, updates); err != nil {
			return nil, err
		}
		for key, value := range extra {
			updates[key] = value
		}
		return updates, nil
	})
}

func validateAppSetting(key string, raw json.RawMessage) (any, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, errors.New("needs a value")
	}
	if strings.HasPrefix(key, "spending.") {
		money, ok := value.(map[string]any)
		if !ok || len(money) != 1 {
			return nil, errors.New("needs money as {usd: \"20.00\"}")
		}
		text, ok := money["usd"].(string)
		if !ok {
			return nil, errors.New("usd must be a decimal string")
		}
		pieces := strings.Split(text, ".")
		if len(pieces) != 2 || len(pieces[1]) != 2 || pieces[0] == "" {
			return nil, errors.New("usd needs two decimal places")
		}
		for _, r := range pieces[0] + pieces[1] {
			if r < '0' || r > '9' {
				return nil, errors.New("usd must be non-negative")
			}
		}
		amount, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsInf(amount, 0) {
			return nil, errors.New("invalid money")
		}
		return amount, nil
	}
	if strings.HasPrefix(key, "models.roles.") {
		var choice AppRoleChoice
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || fields["model"] == nil || fields["effort"] == nil || json.Unmarshal(raw, &choice) != nil || !roles.ValidEffort(choice.Effort) || !appModelWord(choice.Model, true) {
			return nil, errors.New("needs {model, effort: low|medium|high}")
		}
		return choice, nil
	}
	switch key {
	case "models.default":
		text, ok := value.(string)
		if !ok || !appModelWord(text, false) {
			return nil, errors.New("needs a model id")
		}
		return text, nil
	case "models.effort":
		text, ok := value.(string)
		if !ok || !roles.ValidEffort(text) {
			return nil, errors.New("effort must be low, medium or high")
		}
		return text, nil
	case "models.pinned":
		var ids []string
		if json.Unmarshal(raw, &ids) != nil || len(ids) != 3 {
			return nil, errors.New("needs exactly three model slots")
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if !appModelWord(id, true) || id != "" && seen[id] {
				return nil, errors.New("pinned models must be distinct model ids or empty slots")
			}
			seen[id] = true
		}
		return ids, nil
	case "permissions.mode":
		switch value {
		case "ask_risky":
			return "ask_risky", nil
		case "ask_everything":
			return "ask_everything", nil
		case "dont_ask":
			return "dont_ask", nil
		}
		return nil, errors.New("mode must be ask_risky, ask_everything or dont_ask")
	case "permissions.always_ask":
		var rows []string
		if json.Unmarshal(raw, &rows) != nil || len(rows) > 64 {
			return nil, errors.New("needs a list of at most 64 actions")
		}
		for _, floor := range appSettings[key].fallback.([]string) {
			found := false
			for _, row := range rows {
				if row == floor {
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("always asks for %s", floor)
			}
		}
		for _, row := range rows {
			if strings.TrimSpace(row) == "" || len(row) > 240 || strings.ContainsAny(row, "\r\n\x00") {
				return nil, errors.New("invalid action")
			}
		}
		return rows, nil
	case "memory.agents_may_add", "crews.wake":
		flag, ok := value.(bool)
		if !ok {
			return nil, errors.New("needs true or false")
		}
		return flag, nil
	case "advanced.work_folder":
		path, ok := value.(string)
		if !ok || path == "" || len(path) > 4096 || strings.ContainsAny(path, "\r\n\x00") || !filepath.IsAbs(path) && !strings.HasPrefix(path, "~/") {
			return nil, errors.New("needs an absolute path or ~/path")
		}
		return path, nil
	}
	if strings.HasPrefix(key, "accounts.") {
		switch value {
		case "yes", "ask_first", "off":
			if (strings.HasSuffix(key, ".mail-send") || strings.HasSuffix(key, ".calendar-write") || strings.HasSuffix(key, ".messages-send")) && value != "ask_first" {
				return nil, errors.New("acting in your name always asks")
			}
			return value, nil
		}
		return nil, errors.New("account must be yes, ask_first or off")
	}
	bands := map[string][2]int{"permissions.question_clock_min": {1, 1440}, "factory.done_days": {0, 3650}, "factory.polling_s": {1, 86400}, "factory.runner_timeout_s": {factory.MinRunnerTimeoutSeconds, factory.MaxRunnerTimeoutSeconds}, "factory.keep_output_days": {factory.MinKeepOutputDays, factory.MaxKeepOutputDays}, "factory.keep_output_mb": {factory.MinKeepOutputMB, factory.MaxKeepOutputMB}, "advanced.context_tokens": {1, 2000000}, "advanced.proposal_s": {0, 60}, "advanced.idle_days": {1, 3650}, "crews.depth": {teamsDepthMin, teamsDepthMax}, "crews.share_pct": {teamsShareMin, teamsShareMax}}
	band, known := bands[key]
	n, ok := value.(float64)
	if !known || !ok || n != math.Trunc(n) || n < float64(band[0]) || n > float64(band[1]) {
		return nil, fmt.Errorf("needs an integer from %d to %d", band[0], band[1])
	}
	return int(n), nil
}

func appModelWord(text string, empty bool) bool {
	return (empty || text != "") && len(text) <= 240 && strings.TrimSpace(text) == text && !strings.ContainsAny(text, " \t\r\n\x00,")
}

func appRolePins(held map[string]json.RawMessage, updates map[string]any) error {
	changed := false
	for role := range appRoleEngines {
		if _, ok := updates["app.models.roles."+role]; ok {
			changed = true
		}
	}
	if !changed {
		return nil
	}
	raw := ""
	if encoded, ok := held[KeyModelRoles]; ok {
		if json.Unmarshal(encoded, &raw) != nil {
			return errors.New("existing model roles are unreadable")
		}
	}
	pins, err := ParseModelRoles(raw)
	if err != nil {
		return err
	}
	for role, engines := range appRoleEngines {
		value, ok := updates["app.models.roles."+role]
		if !ok {
			continue
		}
		choice := value.(AppRoleChoice)
		updates["app.models.roles."+role] = removeProfileKey
		for _, engine := range engines {
			if choice.Model == "" {
				delete(pins, string(engine))
			} else {
				pins[string(engine)] = (roles.Call{Model: choice.Model, Effort: choice.Effort}).String()
			}
		}
	}
	parts := make([]string, 0, len(pins))
	for role, model := range pins {
		parts = append(parts, role+":"+model)
	}
	sort.Strings(parts)
	updates[KeyModelRoles] = strings.Join(parts, ", ")
	return nil
}

// AppSetupStepAt is separate from the terminal's three-step welcome marker.
// Five means the app's four-step welcome has been completed.
func AppSetupStepAt(profileDir string) int {
	if n, ok := persistedInt(profileDir, KeyAppSetupStep); ok && n >= 1 && n <= 5 {
		return n
	}
	return 1
}

func WriteAppSetup(profileDir string, step int, patch map[string]json.RawMessage, expect string) error {
	if step < 1 || step > 5 {
		return errors.New("setup step must be from 1 to 5")
	}
	return applyAppSettings(profileDir, patch, expect, map[string]any{KeyAppSetupStep: step})
}

// AppRoleSource makes the two app role groups follow this agent when unpinned.
// App agent launchers wrap their ordinary routing source here; terminal callers
// keep their existing tier defaults. The profile is read at each role call so a
// Settings edit reaches the next call without restarting an agent.
func AppRoleSource(profileDir, agentModel, agentEffort string, fallback roles.Source) roles.Source {
	if !roles.ValidEffort(agentEffort) {
		agentEffort = AppDefaultEffort
	}
	return func(key string) (string, bool) {
		for _, engines := range appRoleEngines {
			for _, engine := range engines {
				if key != roles.PinKey(engine) {
					continue
				}
				pins, err := ParseModelRoles(ModelRolesAt(profileDir))
				if err == nil {
					if pin := pins[string(engine)]; pin != "" {
						return pin, true
					}
				}
				if agentModel == "" {
					return "", false
				}
				return (roles.Call{Model: agentModel, Effort: agentEffort}).String(), true
			}
		}
		if fallback != nil {
			return fallback(key)
		}
		return "", false
	}
}

// Dynamic MCP account rows use the same validated namespace as the app writer.
// A typo elsewhere under app.* must still be reported as an unread key.
func appMCPProfileKey(key string) bool {
	if appAccountActionKey(strings.TrimPrefix(key, "app.")) {
		return true
	}
	if !strings.HasPrefix(key, "app.accounts.mcp:") {
		return false
	}
	_, ok := appSettingFor(strings.TrimPrefix(key, "app."))
	return ok
}

// AppEffortWord is the one codec for saved terminal rungs on app resources.
// Reading never rewrites the terminal's choice, and app mutations stay strict.
func AppEffortWord(word string) string {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "low":
		return "low"
	case "medium":
		return AppDefaultEffort
	case "high", "xhigh", "max":
		return "high"
	default:
		return AppDefaultEffort
	}
}

// AppFactoryDoneDaysAt shares the Settings retention choice with served job projections.
func AppFactoryDoneDaysAt(profileDir string) int {
	if n, ok := persistedInt(profileDir, "app.factory.done_days"); ok && n >= 0 && n <= 3650 {
		return n
	}
	return AppDefaultFactoryDoneDays
}
