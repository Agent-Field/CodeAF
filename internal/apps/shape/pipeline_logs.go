package shape

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func planLog(c bridge.Change, files spec.Files) string {
	if c.From == "plan" {
		return "Plan · " + strings.Replace(c.Request, ": ", " · ", 1)
	}
	if strings.HasPrefix(strings.ToLower(c.Request), "rename ") {
		def, err := spec.Decode(files)
		if err == nil && def.App.Records["deal"] != nil {
			return "Plan · Data › deal · rename a field"
		}
	}
	return "Plan · " + c.Request
}

func writeLog(files spec.Files) string {
	lines := []string{}
	for _, name := range files.Paths() {
		line := "Wrote " + name
		if name == spec.AppFile {
			var app struct {
				Records map[string]struct {
					Fields map[string]struct {
						Alias string `json:"alias"`
					} `json:"fields"`
				} `json:"records"`
			}
			if json.Unmarshal(files[name], &app) == nil {
				aliases := spec.Files{}
				for _, rt := range app.Records {
					for field, f := range rt.Fields {
						if f.Alias != "" {
							aliases[f.Alias+" → "+field] = nil
						}
					}
				}
				if len(aliases) > 0 {
					line += " · alias " + strings.Join(aliases.Paths(), ", ")
				}
			}
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "No files changed"
	}
	return strings.Join(lines, "\n")
}
