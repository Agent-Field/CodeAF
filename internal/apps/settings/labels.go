package settings

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func triggerInfo(def *spec.Bundle, trigger spec.Trigger, state triggerState, capPaused bool, at time.Time) bridge.TriggerInfo {
	ai := runAI(def.Runs[trigger.Run])
	info := bridge.TriggerInfo{
		ID: trigger.ID, On: string(trigger.On), Type: trigger.Type, Run: trigger.Run,
		Source: triggerSource(trigger), Label: triggerStart(def.App, trigger) + " → " + runLabel(def, trigger.Run),
		AI: ai, Paused: state.Paused || (ai && capPaused),
		PausedBy: state.PausedBy, PausedAt: state.PausedAt, LastFired: state.LastFired,
	}
	if trigger.On == spec.TriggerSchedule && !info.Paused {
		if next, ok := nextCron(trigger.Cron, at); ok {
			info.NextFire = next.Format(time.RFC3339)
		}
	}
	return info
}

func triggerSource(trigger spec.Trigger) bridge.TriggerSource {
	switch trigger.On {
	case spec.TriggerSchedule:
		return bridge.TriggerSourceSchedule
	case spec.TriggerConnector:
		return bridge.TriggerSourceConnector
	default:
		return bridge.TriggerSourceRecord
	}
}

func triggerStart(app *spec.App, trigger spec.Trigger) string {
	switch trigger.On {
	case spec.TriggerSchedule:
		return scheduleLabel(trigger.Cron)
	case spec.TriggerConnector:
		if trigger.Event != "" {
			return words(trigger.Event) + " · " + words(trigger.Connector)
		}
		return words(trigger.ID)
	default:
		if trigger.When != "" {
			return words(trigger.ID)
		}
		label := words(trigger.Type)
		if recordType := app.Records[trigger.Type]; recordType != nil && recordType.Label != "" {
			label = recordType.Label
		}
		return label + " " + strings.TrimPrefix(string(trigger.On), "record.")
	}
}

func runLabel(def *spec.Bundle, id string) string {
	if run := def.Runs[id]; run != nil && run.Title != "" {
		return run.Title
	}
	return strings.ToLower(words(id))
}

func words(id string) string {
	text := strings.NewReplacer("-", " ", "_", " ", ".", " ").Replace(id)
	r, n := utf8.DecodeRuneInString(text)
	if n == 0 {
		return ""
	}
	return string(unicode.ToUpper(r)) + text[n:]
}

func scheduleLabel(cron string) string {
	parts := strings.Fields(cron)
	if len(parts) != 5 {
		return "Schedule " + cron
	}
	minute, mErr := strconv.Atoi(parts[0])
	hour, hErr := strconv.Atoi(parts[1])
	if mErr != nil || hErr != nil || parts[2] != "*" || parts[3] != "*" {
		return "Schedule " + cron
	}
	if parts[4] == "*" {
		return fmt.Sprintf("Every day %02d:%02d", hour, minute)
	}
	day, err := strconv.Atoi(parts[4])
	if err == nil && day >= 0 && day <= 7 {
		return fmt.Sprintf("Every %s %02d:%02d", time.Weekday(day%7), hour, minute)
	}
	return "Schedule " + cron
}

func runAI(run *spec.Run) bool { return run != nil && stepsAI(run.Steps) }

func stepsAI(steps []*spec.Step) bool {
	for _, step := range steps {
		if step != nil && (step.Agent != "" || stepsAI(step.Steps)) {
			return true
		}
	}
	return false
}
