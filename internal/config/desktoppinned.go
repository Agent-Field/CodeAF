package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The pinned models.
//
// The composer's model control shows three models as a segmented control and
// answers ⌘1-3 for them. Which three is the person's choice, kept in the
// profile next to the role choices; until they choose, these are the three.

// DesktopPinnedCount is how many models are pinned.
const DesktopPinnedCount = 3

const keyDesktopPinned = "desktop.pinned"

// DesktopPinnedDefault is the pinned list until the person changes it, in the
// order of the segmented control and of ⌘1, ⌘2, ⌘3.
var DesktopPinnedDefault = []string{"z-ai/glm-5.3-flash", DesktopDefaultModel, "z-ai/glm-5.3"}

// desktopPinnedLabels are the short words the segmented control shows. They
// live here, once; a model without one reads as the last part of its name.
var desktopPinnedLabels = map[string]string{
	"z-ai/glm-5.3-flash":           "GLM Flash",
	"deepseek/deepseek-v4.1-flash": "DS Flash",
	"z-ai/glm-5.3":                 "GLM 5.3",
}

// DesktopPinnedLabel is the segmented control's word for a model.
func DesktopPinnedLabel(model string) string {
	if label, ok := desktopPinnedLabels[model]; ok {
		return label
	}
	return model[strings.LastIndex(model, "/")+1:]
}

// DesktopPinned lists the pinned models and reports whether the person chose
// them. A stored list that is not exactly three distinct models is ignored.
func DesktopPinned(profileDir string) (models []string, chosen bool) {
	if encoded, ok := persistedValue(profileDir, keyDesktopPinned); ok {
		var stored []string
		if json.Unmarshal(encoded, &stored) == nil && validPinned(stored) == nil {
			return stored, true
		}
	}
	return append([]string(nil), DesktopPinnedDefault...), false
}

func validPinned(models []string) error {
	if len(models) != DesktopPinnedCount {
		return fmt.Errorf("pin exactly %d models", DesktopPinnedCount)
	}
	seen := map[string]bool{}
	for _, model := range models {
		if model == "" || strings.ContainsAny(model, " \t\r\n:") || seen[model] {
			return errors.New("pin three different models")
		}
		seen[model] = true
	}
	return nil
}

// WriteDesktopPinned records the pinned list. An empty list puts it back on
// the default.
func WriteDesktopPinned(profileDir string, models []string) error {
	if len(models) == 0 {
		return writeProfileValues(profileDir, map[string]any{keyDesktopPinned: removeProfileKey})
	}
	if err := validPinned(models); err != nil {
		return err
	}
	return writeProfileValues(profileDir, map[string]any{keyDesktopPinned: models})
}
