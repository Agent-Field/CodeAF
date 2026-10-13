package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// AppCap is an execution limit, with an optional local-midnight override.
// Keeping both figures lets a restart preserve a raise without making it permanent.
type AppCap struct {
	Permanent bool      `json:"permanent,omitempty"`
	USD       float64   `json:"usd"`
	TodayUSD  float64   `json:"today_usd,omitempty"`
	Until     time.Time `json:"until,omitempty"`
}

const appCapsKey = "app.spending.caps"

var ErrAppCapConflict = errors.New("the new cap must be higher than the current cap")

func appCapsFrom(values map[string]json.RawMessage) (map[string]AppCap, error) {
	caps := map[string]AppCap{}
	if raw := values[appCapsKey]; raw != nil {
		if err := json.Unmarshal(raw, &caps); err != nil {
			return nil, err
		}
	}
	return caps, nil
}

func appCapFrom(values map[string]json.RawMessage, scope, id string, base float64, now time.Time) (float64, error) {
	caps, err := appCapsFrom(values)
	if err != nil {
		return 0, err
	}
	if scope == "day" || scope == "task" {
		key, fallback := KeyDailyBudget, AppDefaultDayCap
		if scope == "task" {
			key, fallback = KeyCrewTaskCap, AppDefaultTaskCap
		}
		base = fallback
		if raw := values[key]; raw != nil {
			if err := json.Unmarshal(raw, &base); err != nil {
				return 0, err
			}
		}
	}
	if cap, ok := caps[scope+":"+id]; ok {
		// Day and task defaults remain live settings unless this particular task
		// has its own permanent override.
		if scope != "day" && (scope != "task" || cap.Permanent) {
			base = cap.USD
		}
		if now.Before(cap.Until) {
			base = max(base, cap.TodayUSD)
		}
	}
	return validateDailyBudget(base, "app cap")
}

// AppCapAt is used by the HTTP resource and the execution owner alike.
// Expiry is evaluated on every admission, without a timer that could die on restart.
func AppCapAt(profile, scope, id string, base float64, now time.Time) (float64, error) {
	values, err := readProfileConfig(profile)
	if err != nil {
		return 0, err
	}
	return appCapFrom(values, scope, id, base, now)
}

// BindAppAgentBudget installs the saved agent's base while preserving today's raise.
func BindAppAgentBudget(profile, id string, usd float64) error {
	if _, err := validateDailyBudget(usd, "agent cap"); err != nil {
		return err
	}
	return transformProfileValues(profile, func(values map[string]json.RawMessage) (map[string]any, error) {
		caps, err := appCapsFrom(values)
		if err != nil {
			return nil, err
		}
		cap := caps["agent:"+id]
		cap.USD, cap.Permanent = usd, true
		caps["agent:"+id] = cap
		return map[string]any{appCapsKey: caps}, nil
	})
}

// RaiseAppCap compares and saves under the profile's cross-process lock.
// A temporary day raise does not rewrite the terminal's permanent daily setting.
func RaiseAppCap(profile, scope, id string, base, usd float64, today bool, now time.Time) error {
	if scope != "day" && scope != "agent" && scope != "task" {
		return fmt.Errorf("this cap needs its resource's execution owner")
	}
	if _, err := validateDailyBudget(usd, "app cap"); err != nil {
		return err
	}
	return transformProfileValues(profile, func(values map[string]json.RawMessage) (map[string]any, error) {
		old, err := appCapFrom(values, scope, id, base, now)
		if err != nil {
			return nil, err
		}
		if old == 0 || usd <= old {
			return nil, ErrAppCapConflict
		}
		caps, err := appCapsFrom(values)
		if err != nil {
			return nil, err
		}
		cap, exists := caps[scope+":"+id]
		if !exists {
			cap.USD = base
			if scope == "task" {
				cap.USD, _ = appCapFrom(values, scope, id, base, now)
			}
		}
		updates := map[string]any{}
		if today {
			cap.TodayUSD = usd
			cap.Until = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		} else {
			cap = AppCap{USD: usd, Permanent: true}
			if scope == "day" {
				updates[KeyDailyBudget] = usd
			}
		}
		caps[scope+":"+id] = cap
		updates[appCapsKey] = caps
		return updates, nil
	})
}

// AgentsMayAddMemoryAt leaves explicit person writes available when automation is off.
func AgentsMayAddMemoryAt(profile string) bool {
	values, err := readProfileConfig(profile)
	if err != nil {
		return false
	}
	allowed := true
	if raw := values["app.memory.agents_may_add"]; raw != nil {
		if json.Unmarshal(raw, &allowed) != nil {
			return false
		}
	}
	return allowed
}

// AppDefaultAgentBudgetAt resolves the same number the settings route displays.
func AppDefaultAgentBudgetAt(profile string) float64 {
	values, err := readProfileConfig(profile)
	if err != nil {
		return AppDefaultAgentBudget
	}
	usd := AppDefaultAgentBudget
	if raw := values["app.spending.per_agent"]; raw != nil {
		if json.Unmarshal(raw, &usd) != nil {
			return AppDefaultAgentBudget
		}
	}
	if _, err := validateDailyBudget(usd, "agent cap"); err != nil {
		return AppDefaultAgentBudget
	}
	return usd
}

// AppTaskCapID keeps a task's local number inside its root agent's namespace.
func AppTaskCapID(agent, task string) string { return agent + ":" + task }
