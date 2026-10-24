package main

import (
	"os"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// crewConfigStamp is the same freshness contract as internal/filememo: a file
// creation, removal, size change or modification time invalidates the snapshot.
// The in-process generation still covers writes on coarse timestamp clocks.
type crewConfigStamp struct {
	Size     int64
	Modified time.Time
	Exists   bool
}

// configFilesNow notices writes from a settings window or editor in another
// process. Only metadata is read on the resolution path, never file contents.
func (c *v3Crew) configFilesNow() [2]crewConfigStamp {
	var stamps [2]crewConfigStamp
	for i, path := range []string{config.BudgetConfigPath(c.profileDir), config.ProjectConfigPath(c.workspace)} {
		if info, err := os.Stat(path); err == nil {
			stamps[i] = crewConfigStamp{Size: info.Size(), Modified: info.ModTime(), Exists: true}
		}
	}
	return stamps
}
