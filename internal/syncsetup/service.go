package syncsetup

import (
	"context"
	"errors"
	"os"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// ProbeService asks the sync service at url what this build needs it to
// promise ([directory.Capabilities]), and answers the promises it did not
// make. It acts as a new identity of its own, in a home it removes, so it
// reads nothing of any person and leaves only one empty archived chat record.
// The release gate runs it against the live service before a build ships.
func ProbeService(ctx context.Context, url string) ([]directory.Capability, error) {
	base, err := relayBase(url)
	if err != nil {
		return nil, err
	}
	home, err := os.MkdirTemp("", "codeaf-probe-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(home)
	id, err := identity.Ensure(home)
	if err != nil {
		return nil, err
	}
	dev, err := identity.Device(home)
	if err != nil {
		return nil, err
	}
	s := build(home, base, id, dev, 0)
	if err := s.putDevice(ctx, "release check"); err != nil {
		return nil, err
	}
	probe, err := cell.NewID(s.now())
	if err != nil {
		return nil, err
	}
	if _, err := s.Dir.Create(ctx, probe, directory.CellInit{Class: string(cell.HostBound)}); err != nil {
		return nil, err
	}
	reader, ok := s.Dir.(interface {
		Missing(context.Context, string) ([]directory.Capability, error)
	})
	if !ok {
		return nil, errors.New("this sync client cannot read a service's promises")
	}
	missing, err := reader.Missing(ctx, probe)
	return missing, errors.Join(err, s.Dir.Archive(ctx, probe))
}
