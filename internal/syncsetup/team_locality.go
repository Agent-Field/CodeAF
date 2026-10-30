package syncsetup

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/teams"
)

const (
	teamComputerTimeout  = 3 * time.Second
	teamComputerCacheFor = 5 * time.Second
)

type teamDirectoryCache struct {
	sync.Mutex
	at      time.Time
	listing directory.Listing
	err     error
}

// Handles are reconstructed for tools. Share only the bounded directory reading
// across them, scoped to the current fleet, device, home and relay.
var teamDirectories sync.Map

func (s *Sync) teamDirectory(ctx context.Context) (directory.Listing, error) {
	key := s.teamDirectoryKey()
	value, _ := teamDirectories.LoadOrStore(key, &teamDirectoryCache{})
	cached := value.(*teamDirectoryCache)
	cached.Lock()
	defer cached.Unlock()
	if !cached.at.IsZero() && time.Since(cached.at) < teamComputerCacheFor {
		return cached.listing, cached.err
	}
	ctx, cancel := context.WithTimeout(ctx, teamComputerTimeout)
	defer cancel()
	cached.listing, cached.err = s.Dir.List(ctx)
	cached.at = time.Now()
	return cached.listing, cached.err
}

// TeamComputer reports only positive remote ownership; missing local paths
// say nothing about where a member runs, including sub-team manager claims.
func (s *Sync) TeamComputer(ctx context.Context, member teams.Member) (string, error) {
	names, err := s.TeamComputers(ctx, []teams.Member{member})
	return names[member.Key], err
}

func (s *Sync) TeamComputers(ctx context.Context, members []teams.Member) (map[string]string, error) {
	out := map[string]string{}
	if Quiet(s.Home) {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, teamComputerTimeout)
	defer cancel()
	cells := map[string]string{}
	devices := map[string]string{}
	for _, member := range members {
		if teams.OnOtherComputer(member.Key) {
			out[member.Key] = "the other computer"
			id := strings.TrimPrefix(member.Key, "other-computer:device:")
			devices[member.Key], _, _ = strings.Cut(id, ":")
			continue
		}
		id, remote := teams.RemoteCell(member.Key)
		if remote {
			out[member.Key] = "the other computer"
		}
		if !remote {
			var ok bool
			id, ok = strings.CutPrefix(member.SyncID, "cell:")
			if !ok {
				id = ""
			}
		}
		if id == "" && member.File != "" && member.SyncID == "" {
			root := filepath.Dir(member.File)
			if filepath.Base(root) == cell.StateDir {
				root = filepath.Dir(root)
			}
			if c, err := cell.OpenAt(root, filepath.Base(root)); err == nil {
				id = c.ID
			}
		}
		if id != "" {
			cells[member.Key] = id
		}
	}
	if len(cells) == 0 && len(devices) == 0 {
		return out, nil
	}
	branches, err := cellsync.OpenBranchMap(s.Home)
	if err != nil {
		teams.SharingError(s.profile(), err)
		return out, nil
	}
	// Local members use ownership already learned by Home or its follower.
	// Only a remote sentinel can justify adding a directory request here.
	listing, local, err := s.displayListing(ctx)
	if !local && err == nil {
		if value, ok := teamDirectories.Load(s.teamDirectoryKey()); ok {
			cached := value.(*teamDirectoryCache)
			cached.Lock()
			if !cached.at.IsZero() && time.Since(cached.at) < teamComputerCacheFor {
				listing, err, local = cached.listing, cached.err, true
			}
			cached.Unlock()
		}
	}
	if !local && err == nil {
		if len(out) == 0 {
			return out, nil
		}
		listing, err = s.teamDirectory(ctx)
	}
	if err != nil {
		teams.SharingError(s.profile(), err)
		return out, nil
	}
	key := directory.MetadataKey(s.Identity.CellKey())
	open := func(sealed string) (string, error) { return directory.OpenName(key, sealed) }
	name := func(id string) string {
		if d, ok := listing.Devices[id]; ok && d.Name != "" {
			if text, err := open(d.Name); err == nil && strings.TrimSpace(text) != "" {
				return text
			}
		}
		return "the other computer"
	}
	for member, id := range devices {
		out[member] = name(id)
	}
	rows := chatlist.Rows(listing, s.Device.ID(), open)
	for member, id := range cells {
		id = branches.Resolve(id)
		for _, row := range rows {
			if row.Cell == id && row.DeviceID != s.Device.ID() && (row.Status == chatlist.Running || (row.Status == chatlist.Off && !row.Mine)) {
				out[member] = name(row.DeviceID)
				break
			}
		}
	}
	return out, nil
}

func (s *Sync) teamDirectoryKey() string {
	return s.Home + "\x00" + s.Identity.ID() + "\x00" + s.Device.ID() + "\x00" + s.Relay
}

// Existing directory reads supply local ownership evidence without adding a
// request to local sends. A failed read is cached for the same short interval.
func (s *Sync) rememberTeamDirectory(l directory.Listing, err error) {
	value, _ := teamDirectories.LoadOrStore(s.teamDirectoryKey(), &teamDirectoryCache{})
	cached := value.(*teamDirectoryCache)
	cached.Lock()
	defer cached.Unlock()
	cached.listing, cached.err, cached.at = l, err, time.Now()
}
