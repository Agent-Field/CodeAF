package session

import (
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
)

var cursorDeviceCache struct {
	sync.Mutex
	root   string
	stamps [3]fileStamp
	device string
	known  bool
	reads  int
}

// Identity reads never create files. Save checks only stamps on quiet paths;
// pairing, replacement and device renewal invalidate the process's cached id.
func teamCursorDevice() string {
	root := home.Dir()
	stamps := [3]fileStamp{stampOf(filepath.Join(root, "identity-solo")), stampOf(filepath.Join(root, identity.File)), stampOf(filepath.Join(root, identity.DeviceFile))}
	cursorDeviceCache.Lock()
	defer cursorDeviceCache.Unlock()
	if cursorDeviceCache.known && cursorDeviceCache.root == root && cursorDeviceCache.stamps == stamps {
		return cursorDeviceCache.device
	}
	cursorDeviceCache.root, cursorDeviceCache.stamps, cursorDeviceCache.known = root, stamps, true
	cursorDeviceCache.device = ""
	if identity.Solo(root) {
		return ""
	}
	cursorDeviceCache.reads++
	if _, err := identity.Load(root); err != nil {
		return ""
	}
	if dev, err := identity.LoadDevice(root); err == nil {
		cursorDeviceCache.device = dev.ID()
	}
	return cursorDeviceCache.device
}
