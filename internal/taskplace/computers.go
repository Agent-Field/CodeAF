// Package taskplace adapts paired task doors to the conversation's run engine.
package taskplace

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

// Computers uses the same identity roster as pairing. The separate machine
// access book is deliberately uninvolved: pairing is the task permission.
type Computers struct {
	Directory directory.Client
	Copies    taskdoor.CopyDeps
	Home      string
	Clock     session.TaskWatchClock
}

func (c *Computers) ThisComputer() string { return devname.Name(c.Home) }
func (c *Computers) List(ctx context.Context) ([]session.TaskComputer, error) {
	listing, err := c.Directory.List(ctx)
	if err != nil {
		return nil, err
	}
	key := directory.MetadataKey(c.Copies.Identity.CellKey())
	var out []session.TaskComputer
	for id, device := range listing.Devices {
		if device.Revoked || id == c.Copies.Device.ID() {
			continue
		}
		name, _ := directory.OpenName(key, device.Name)
		if strings.TrimSpace(name) == "" {
			name = "a device"
		}
		row := session.TaskComputer{Device: id, Name: devname.Shown(name), CanTakeTasks: device.Platform != "windows"}
		if !row.CanTakeTasks {
			row.Reason = UnsupportedTarget(row.Name)
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Device < out[j].Device
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
func UnsupportedTarget(name string) string {
	return name + " cannot take tasks from other computers yet"
}
func (c *Computers) Engine(ctx context.Context, device string) (session.TaskRunEngine, error) {
	computers, err := c.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, computer := range computers {
		if computer.Device == device {
			if !computer.CanTakeTasks {
				return nil, errors.New(computer.Reason)
			}
			return c.RecoverEngine(session.TaskExecutor{Device: computer.Device, Name: computer.Name}), nil
		}
	}
	return nil, errors.New(taskdoor.RemovedComputerLine)
}

// RecoverEngine needs only the saved executor to install an offline watcher or
// compare the local copy. Each dial checks current roster permission separately.
func (c *Computers) RecoverEngine(executor session.TaskExecutor) session.TaskRunEngine {
	return &Engine{Copies: c.Copies, Computer: session.TaskComputer{Device: executor.Device, Name: executor.Name, CanTakeTasks: true}, OriginName: c.ThisComputer(), Clock: c.Clock, Allowed: func(ctx context.Context) error {
		computers, err := c.List(ctx)
		if err != nil {
			return err
		}
		for _, computer := range computers {
			if computer.Device == executor.Device && computer.CanTakeTasks {
				return nil
			}
		}
		return errors.New(taskdoor.RemovedComputerLine)
	}}
}
