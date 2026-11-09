package main

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/diskspace"
	"github.com/Agent-Field/codeaf/internal/home"
)

// HealthCheck is health.check: what this engine knows about itself, read from
// memory, disk readings, and bounded checks of git and gh in the user's
// normal environment. It opens no conversation.
func (b *appBackend) HealthCheck() (appwire.Health, error) {
	b.refreshKeyHealth()
	var health appwire.Health
	b.locked(func() {
		health.StartedAt = b.startedAt
		if b.lastResume != nil {
			resume := *b.lastResume
			health.Resume = &resume
		}
	})
	health.ProviderKey = b.keyHealth.read()
	health.Checks = machineReadiness()
	health.Version, health.Build = resolvedVersion(), buildinfo.Identity()
	health.UptimeS = int64(b.now().Sub(health.StartedAt).Seconds())
	work := ""
	_ = b.state.read(func(f *appStateFile) { work = f.workFolder() })
	health.Disks = []appwire.HealthDisk{
		appDisk(appwire.DiskWorkFolder, work),
		appDisk(appwire.DiskCodeafHome, home.Dir()),
	}
	factory := b.localStatus()
	health.Factory = &factory
	return health, nil
}

// appDisk is one disk reading, its failure said in the reading rather than
// failing the whole check.
func appDisk(role, path string) appwire.HealthDisk {
	disk := appwire.HealthDisk{Role: role, Path: path}
	usage, err := diskspace.Of(path)
	if err != nil {
		disk.Error = err.Error()
		return disk
	}
	disk.FreeBytes, disk.TotalBytes = usage.FreeBytes, usage.TotalBytes
	return disk
}
