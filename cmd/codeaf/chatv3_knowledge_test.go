package main

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

func TestLocalKnowledgeUsesEngineProfile(t *testing.T) {
	engineProfile := t.TempDir()
	surfaceProfile := t.TempDir()
	t.Setenv("CODEAF_HOME", surfaceProfile)
	t.Cleanup(func() { stopPoolErrands(engineProfile); stopPoolErrands(surfaceProfile) })
	options := tui3.Options{}
	localDoors(&options, remote.Welcome{ProfileDir: engineProfile, PlacesRoot: t.TempDir()}, config.Config{ProfileDir: surfaceProfile})
	if options.Knowledge == nil || options.Knowledge.Directory() != filepath.Join(engineProfile, "knowledge") {
		t.Fatal("local Knowledge did not use the engine's resolved profile")
	}
}
