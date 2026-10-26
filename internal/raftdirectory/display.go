package raftdirectory

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

// DisplayListing projects one applied view. Public reads call it only inside
// their fresh quorum barrier; a personal learner uses it solely for Home's
// display hints. Taking, publishing and authorization never use this shortcut.
func DisplayListing(ctx context.Context, s *raftstore.SQLite, app raftstore.Application) (directory.Listing, error) {
	l := directory.Listing{Devices: map[string]directory.Device{}, Cells: map[string]directory.Cell{}}
	l.Now, l.Version = app.LogicalTime, app.VisibleVersion
	after := ""
	for {
		rows, _, err := s.ScanPage(ctx, app.Applied, raftstore.State, after)
		if err != nil {
			return l, err
		}
		if len(rows) == 0 {
			return l, nil
		}
		for _, r := range rows {
			after = r.Key.Key
			switch {
			case after == "identity":
				if err := clusterstate.Decode(r.Value, &l.Identity); err != nil {
					return l, err
				}
			case strings.HasPrefix(after, "device/"):
				id, e := hex.DecodeString(strings.TrimPrefix(after, "device/"))
				if e != nil {
					return l, clusterstate.ErrDependency
				}
				var v directory.Device
				if e = clusterstate.Decode(r.Value, &v); e != nil {
					return l, e
				}
				l.Devices[string(id)] = v
			case strings.HasPrefix(after, "cell/"):
				id, e := hex.DecodeString(strings.TrimPrefix(after, "cell/"))
				if e != nil {
					return l, clusterstate.ErrDependency
				}
				var v directory.Cell
				if e = clusterstate.Decode(r.Value, &v); e != nil {
					return l, e
				}
				l.Cells[string(id)] = v
			}
		}
	}
}
