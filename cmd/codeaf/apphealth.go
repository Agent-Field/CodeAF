package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/diskspace"
	"github.com/Agent-Field/codeaf/internal/home"
)

// HealthCheck is health.check: what this engine knows about itself, read from
// memory, the state file it already holds, and one filesystem call per disk.
// It opens no conversation and starts nothing.
func (b *appBackend) HealthCheck(ctx context.Context) (appwire.Health, error) {
	var health appwire.Health
	b.locked(func() {
		health.StartedAt = b.startedAt
		if b.lastResume != nil {
			resume := *b.lastResume
			health.Resume = &resume
		}
	})
	health.Version, health.Build = resolvedVersion(), buildinfo.Identity()
	health.UptimeS = int64(b.now().Sub(health.StartedAt).Seconds())
	work := ""
	_ = b.state.read(func(f *appStateFile) { work = f.workFolder() })
	health.Disks = []appwire.HealthDisk{
		appDisk(ctx, appwire.DiskWorkFolder, work),
		appDisk(ctx, appwire.DiskCodeafHome, home.Dir()),
	}
	factory := b.localStatus()
	health.Factory = &factory
	return health, nil
}

// appDisk is one disk reading, its failure said in the reading rather than
// failing the whole check.
func appDisk(ctx context.Context, role, path string) appwire.HealthDisk {
	disk := appwire.HealthDisk{Role: role, Path: path}
	if err := ctx.Err(); err != nil {
		disk.Error = err.Error()
		return disk
	}
	usage, err := diskspace.Of(path)
	if err != nil {
		disk.Error = err.Error()
		return disk
	}
	disk.FreeBytes, disk.TotalBytes = usage.FreeBytes, usage.TotalBytes
	return disk
}
