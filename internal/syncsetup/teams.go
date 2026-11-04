package syncsetup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

// teamKeys keeps field clocks separate from membership clocks. Only the wire
// values use portable identities; the local store keeps its transcript paths.
type teamKeys struct {
	profile, home, device string
	cache                 *teamIdentityCache
}

// The cache lives on Sync under stateMu. Capture probes only members' metadata
// stamps; the expensive discovery index is built solely for incoming identities.
type teamIdentityCache struct {
	ids          map[string]cachedTeamIdentity
	discovery    string
	index        map[string]teams.Member
	scans        int
	queried      map[string]bool
	opens        int
	scanned      bool
	identities   *teamIdentities
	input        string
	traffic      map[string]teamTrafficWindow
	trafficReads int
}

// A quiet round need not decode the same tail again. Keep authored windows
// separate from identity translation, which can change after membership edits.
type teamTrafficWindow struct {
	stamp string
	lines []teams.Entry
}
type cachedTeamIdentity struct{ stamp, id string }
type teamIdentities struct {
	device   string
	branches *cellsync.BranchMap
	local    map[string]teams.Member
	reverse  map[string]string
	cache    *teamIdentityCache
}

func teamFileStamp(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return info.ModTime().String() + "/" + teamValue(info.Size())
}

func (p teamKeys) identities(f *teams.File) (*teamIdentities, error) {
	cache := p.cache
	if cache == nil {
		cache = &teamIdentityCache{}
	}
	input := teamValue(f.Teams) + teamFileStamp(filepath.Join(p.home, "v3", "sync", "branches.json"))
	for _, t := range f.Teams {
		for _, member := range append(append([]teams.Member(nil), t.Members...), t.FormerMembers...) {
			if !strings.HasPrefix(member.SyncID, "cell:") && member.File != "" {
				root := filepath.Dir(member.File)
				if filepath.Base(root) == cell.StateDir {
					root = filepath.Dir(root)
				}
				input += teamFileStamp(filepath.Join(root, cell.MetaPath))
			}
		}
	}
	if cache.identities != nil && cache.input == input {
		return cache.identities, nil
	}
	branches, err := cellsync.OpenBranchMap(p.home)
	if err != nil {
		return nil, err
	}
	if cache.ids == nil {
		cache.ids = map[string]cachedTeamIdentity{}
	}
	m := &teamIdentities{device: p.device, branches: branches, local: map[string]teams.Member{}, reverse: map[string]string{}, cache: cache}
	for _, t := range f.Teams {
		for _, member := range append(append([]teams.Member(nil), t.Members...), t.FormerMembers...) {
			if _, remote := teams.RemoteCell(member.Key); remote || teams.OnOtherComputer(member.Key) {
				continue
			}
			m.add(m.id(member), member)
		}
	}
	cache.input, cache.identities = input, m
	return m, nil
}

// Duplicate records choose one path lexically. A Go map's traversal order must
// never choose a portable identity or the local transcript that it opens.
func (m *teamIdentities) add(id string, member teams.Member) {
	if old, ok := m.local[id]; !ok || member.Key < old.Key {
		m.local[id] = member
	}
	if m.reverse == nil {
		m.reverse = map[string]string{}
	}
	if old := m.reverse[member.Key]; old == "" || id < old {
		m.reverse[member.Key] = id
	}
}
func (m *teamIdentities) id(member teams.Member) string {
	if id, ok := teams.RemoteCell(member.Key); ok {
		return "cell:" + m.branches.Resolve(id)
	}
	if id, ok := strings.CutPrefix(member.Key, "other-computer:"); ok {
		return id
	}
	if id, ok := strings.CutPrefix(member.SyncID, "cell:"); ok {
		return "cell:" + m.branches.Resolve(id)
	}
	file := member.File
	if file == "" {
		file = member.Key
	}
	root := filepath.Dir(file)
	if filepath.Base(root) == cell.StateDir {
		root = filepath.Dir(root)
	}
	metaStamp := teamFileStamp(filepath.Join(root, cell.MetaPath))
	stamp := metaStamp + "/" + member.SyncID
	// Memberships can preserve different old ids for one file until apply
	// upgrades them. Cache each record's identity without evicting the other.
	cacheKey := file + "\x00" + member.SyncID
	if m.cache != nil {
		if cached, ok := m.cache.ids[cacheKey]; ok && cached.stamp == stamp {
			return cached.id
		}
	}
	sum := sha256.Sum256([]byte(member.Key))
	id := member.SyncID
	if id == "" {
		id = "device:" + m.device + ":" + hex.EncodeToString(sum[:])
	}
	if metaStamp != "" {
		if m.cache != nil {
			m.cache.opens++
		}
		if c, err := cell.OpenAt(root, filepath.Base(root)); err == nil {
			id = "cell:" + m.branches.Resolve(c.ID)
		}
	}
	if m.cache != nil {
		m.cache.ids[cacheKey] = cachedTeamIdentity{stamp, id}
	}
	return id
}

// resolve is apply-only. A known member never asks the session world anything;
// multiple unresolved members share one discovery pass in the network round.
func (p teamKeys) resolve(m *teamIdentities, ids []string) {
	need := false
	for _, id := range ids {
		if strings.HasPrefix(id, "cell:") {
			if _, ok := m.local[id]; !ok {
				need = true
			}
		}
	}
	if !need {
		return
	}
	cache := m.cache
	// A continuation can land inside an existing project bucket, without
	// moving the projects directory's stamp. Its local lease receipt is
	// always filed after landing, so that directory also invalidates discovery.
	stamp := teamFileStamp(cell.Dir(p.home)) + teamFileStamp(filepath.Join(p.home, "v3", "projects")) + teamFileStamp(filepath.Join(p.home, "v3", "sync", "holds"))
	fresh := false
	for _, id := range ids {
		if !cache.queried[id] {
			fresh = true
		}
	}
	if !cache.scanned && (cache.index == nil || cache.discovery != stamp || fresh) {
		if cache.queried == nil {
			cache.queried = map[string]bool{}
		}
		for _, id := range ids {
			cache.queried[id] = true
		}
		cache.scanned = true
		cache.scans++
		cache.discovery = stamp
		cache.index = map[string]teams.Member{}
		add := func(c cell.Cell, file, where string) {
			id := "cell:" + m.branches.Resolve(c.ID)
			member := teams.Member{Key: teams.ConversationIdentity(file), File: file, Where: where}
			if old, ok := cache.index[id]; !ok || member.Key < old.Key {
				cache.index[id] = member
			}
		}
		for _, row := range session.ReadWorld(filepath.Join(p.home, "v3", "projects")).Sessions() {
			root := filepath.Dir(row.Transcript)
			if filepath.Base(root) == cell.StateDir {
				root = filepath.Dir(root)
			}
			if c, err := cell.OpenAt(root, row.ID); err == nil {
				add(c, row.Transcript, row.Workspace)
			}
		}
		dirs, _ := os.ReadDir(cell.Dir(p.home))
		for _, dir := range dirs {
			if dir.IsDir() {
				if c, err := cell.Open(p.home, dir.Name()); err == nil {
					add(c, filepath.Join(c.Root, cell.TranscriptPath), workspaceOf(c.Root))
				}
			}
		}
	}
	names := make([]string, 0, len(cache.index))
	for id := range cache.index {
		names = append(names, id)
	}
	sort.Strings(names)
	for _, id := range names {
		if _, ok := m.local[id]; !ok {
			m.add(id, cache.index[id])
		}
	}
}

// Portable records omit every machine path. Start boundaries retain their
// writer so a foreign sequence can never become a local delivery position.
func (m *teamIdentities) portable(member teams.Member) teams.Member {
	id := m.id(member)
	member.Key, member.File, member.Where, member.SyncID = id, "", "", ""
	if member.StartID != "" {
		member.StartID = m.device + "/" + member.StartID
	} else {
		member.StartID = member.StartOrigin
	}
	member.StartOrigin = ""
	return member
}

// Realization borrows only the receiver's own paths. A missing transcript
// takes the existing machine-row identity rather than guessing a local path.
func (m *teamIdentities) realized(member teams.Member) teams.Member {
	id := member.Key
	member.SyncID = id
	member.StartOrigin = member.StartID
	if localID, own := strings.CutPrefix(member.StartID, m.device+"/"); own {
		member.StartID = localID
	} else {
		member.StartID = ""
	}
	if local, ok := m.local[id]; ok {
		member.Key, member.File, member.Where = local.Key, local.File, local.Where
	} else if cellID, ok := strings.CutPrefix(id, "cell:"); ok {
		member.Key, member.File, member.Where = teams.RemoteKey(cellID), "", ""
	} else {
		member.Key, member.File, member.Where = teams.OtherComputerKey(id), "", ""
	}
	return member
}

func teamValue(v any) string { raw, _ := json.Marshal(v); return string(raw) }

// Fields have independent clocks, including unknown fields from a later
// build. Members carry their full record so unknown member fields survive too.
func (p teamKeys) values(f *teams.File, m *teamIdentities) (map[string]string, error) {
	values := map[string]string{}
	for _, t := range f.Teams {
		t = t.Clone()
		base := t.ID + "/"
		values[base+"exists"] = "true"
		byKey := map[string]string{}
		var order []string
		for _, member := range t.Members {
			id := m.id(member)
			byKey[member.Key] = id
			values[base+"member/"+id] = teamValue(m.portable(member))
			order = append(order, id)
		}
		values[base+"order"] = teamValue(order)
		if t.Manager != "" {
			t.Manager = byKey[t.Manager]
		}
		for i, member := range t.FormerMembers {
			byKey[member.Key] = m.id(member)
			t.FormerMembers[i] = m.portable(member)
		}
		if t.FormerManager != "" {
			former := byKey[t.FormerManager]
			if former == "" {
				former = m.id(teams.Member{Key: t.FormerManager})
			}
			t.FormerManager = former
		}
		raw, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		for key, value := range fields {
			if key != "id" && key != "members" {
				values[base+"field/"+key] = string(value)
			}
		}
	}
	return values, nil
}

// Capture can create the ledger before restore, so backup belongs before
// either step and before Refresh can repair a newly continued local path.
func (p teamKeys) beforeSharing() error {
	if _, err := os.Stat(filepath.Join(p.home, "teams-sync.ledger")); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := teams.BackupBeforeSharing(p.profile); err != nil {
		return fmt.Errorf("could not save teams.before-sharing.json; incoming teams are waiting: %w", err)
	}
	return nil
}

func (p teamKeys) Values() (map[string]string, error) {
	if err := p.beforeSharing(); err != nil {
		return nil, err
	}
	f, err := teams.Snapshot(p.profile)
	if err != nil {
		teams.SharingError(p.profile, err)
		return nil, vaultsync.ErrDamaged
	}
	if len(f.Teams) == 0 {
		return map[string]string{}, nil
	}
	m, err := p.identities(f)
	if err != nil {
		return nil, err
	}
	return p.values(f, m)
}
func (p teamKeys) Holds(name string) bool {
	id, rest, ok := strings.Cut(name, "/")
	return ok && id != "" && (rest == "exists" || rest == "order" || strings.HasPrefix(rest, "field/") || strings.HasPrefix(rest, "member/"))
}
func (p teamKeys) Apply(changes map[string]string) error { return p.ApplyIf(changes, nil) }

// ApplyIf compares each field inside Update's lock with the pre-network
// snapshot. A simultaneous local edit remains for the next capture to send.
func (p teamKeys) ApplyIf(changes, expected map[string]string) error {
	if err := p.beforeSharing(); err != nil {
		return err
	}
	return teams.Update(p.profile, func(f *teams.File) error {
		m, err := p.identities(f)
		if err != nil {
			return err
		}
		current, err := p.values(f, m)
		if err != nil {
			return err
		}
		for key, value := range changes {
			if !p.Holds(key) || (expected != nil && current[key] != expected[key]) {
				continue
			}
			// A deleted team cannot be re-created by an edit to one of its fields.
			if value == "" {
				delete(current, key)
			} else {
				current[key] = value
			}
		}
		var incoming []string
		for key := range current {
			if _, rest, ok := strings.Cut(key, "/member/"); ok {
				incoming = append(incoming, rest)
			}
		}
		p.resolve(m, incoming)
		next, err := p.decode(current, m)
		if err != nil {
			return err
		}
		for _, old := range f.Teams {
			found := false
			for _, t := range next {
				found = found || old.ID == t.ID
			}
			if !found {
				if err := os.Remove(teams.TrafficMirrorPath(p.profile, old.ID)); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
		}
		f.Teams = next
		f.RepairMerged()
		return nil
	})
}

// Decode orders every ambiguous choice before ordinary store repair runs.
// Existence is separate from fields, so an offline rename cannot revive a team.
func (p teamKeys) decode(values map[string]string, m *teamIdentities) ([]teams.Team, error) {
	var result []teams.Team
	for key, value := range values {
		id, rest, _ := strings.Cut(key, "/")
		if rest != "exists" || value != "true" {
			continue
		}
		fields := map[string]json.RawMessage{"id": json.RawMessage(teamValue(id))}
		members := map[string]teams.Member{}
		base := id + "/"
		for name, value := range values {
			rest, ok := strings.CutPrefix(name, base)
			if !ok {
				continue
			}
			if field, ok := strings.CutPrefix(rest, "field/"); ok {
				fields[field] = json.RawMessage(value)
			}
			if who, ok := strings.CutPrefix(rest, "member/"); ok {
				var member teams.Member
				if err := json.Unmarshal([]byte(value), &member); err != nil {
					return nil, err
				}
				member.Key = who
				members[who] = m.realized(member)
			}
		}
		raw, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		var t teams.Team
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		var order []string
		if raw := values[base+"order"]; raw != "" {
			if err := json.Unmarshal([]byte(raw), &order); err != nil {
				return nil, err
			}
		}
		for _, who := range order {
			if member, ok := members[who]; ok {
				t.Members = append(t.Members, member)
				delete(members, who)
			}
		}
		var restMembers []teams.Member
		for _, member := range members {
			restMembers = append(restMembers, member)
		}
		sort.Slice(restMembers, func(i, j int) bool {
			a, b := restMembers[i], restMembers[j]
			if !a.JoinedAt.Equal(b.JoinedAt) {
				return a.JoinedAt.Before(b.JoinedAt)
			}
			return a.SyncID < b.SyncID
		})
		t.Members = append(t.Members, restMembers...)
		manager := t.Manager
		t.Manager = ""
		for _, member := range t.Members {
			if member.SyncID == manager {
				t.Manager = member.Key
			}
		}
		if manager != "" && t.Manager == "" && (t.FormerManager == "" || t.FormerManager == manager) {
			// A concurrent removal can arrive before its manager field. Keep
			// the appointment and its portable identity in the existing history.
			t.FormerManager = manager
			known := false
			for _, former := range t.FormerMembers {
				known = known || former.Key == manager
			}
			if !known {
				former := teams.Member{Key: manager}
				if local, ok := m.local[manager]; ok {
					former = m.portable(local)
				}
				t.FormerMembers = append(t.FormerMembers, former)
			}
		}
		formerManager := t.FormerManager
		if formerManager != "" {
			t.FormerManager = m.realized(teams.Member{Key: formerManager}).Key
		}
		for i, member := range t.FormerMembers {
			t.FormerMembers[i] = m.realized(member)
			if member.Key == formerManager {
				t.FormerManager = t.FormerMembers[i].Key
			}
		}
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		if !result[i].Made.Equal(result[j].Made) {
			return result[i].Made.Before(result[j].Made)
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

// Refresh heals paths after a successful continuation whose immediate team
// bookkeeping failed. No discovery work occurs unless an unresolved cell is
// present and its discovery stamp has moved since the last apply.
func (p teamKeys) Refresh() error {
	if err := p.beforeSharing(); err != nil {
		return err
	}
	f, err := teams.Snapshot(p.profile)
	if err != nil {
		teams.SharingError(p.profile, err)
		return vaultsync.ErrDamaged
	}
	var ids []string
	for _, t := range f.Teams {
		for _, member := range t.Members {
			if id, ok := teams.RemoteCell(member.Key); ok {
				ids = append(ids, "cell:"+id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	m, err := p.identities(f)
	if err != nil {
		return err
	}
	p.resolve(m, ids)
	for _, id := range ids {
		if member, ok := m.local[id]; ok {
			root := filepath.Dir(member.File)
			if filepath.Base(root) == cell.StateDir {
				root = filepath.Dir(root)
			}
			if c, err := cell.OpenAt(root, strings.TrimPrefix(id, "cell:")); err == nil {
				if err := p.rebindChat(strings.TrimPrefix(id, "cell:"), c); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func syncedTeams(profile, home, device string, cache *teamIdentityCache) vaultsync.Keyed {
	return vaultsync.GuardedKeys("teams", teamKeys{profile, home, device, cache}, filepath.Join(home, "teams-sync.ledger"))
}

// A writer publishes only its own windows. The complete received set rebuilds
// the display mirror, so forgetting a transport window never edits a local log.
type teamTraffic struct {
	teamKeys
	peers    func() (map[string]bool, error)
	snapshot *teams.File
	stamp    string
}

func (p *teamTraffic) load() (*teams.File, error) {
	if p.snapshot != nil && p.stamp == teams.Stamp(p.profile) {
		return p.snapshot, nil
	}
	f, err := teams.Snapshot(p.profile)
	if err != nil {
		teams.SharingError(p.profile, err)
		return nil, vaultsync.ErrDamaged
	}
	p.snapshot = f
	p.stamp = teams.Stamp(p.profile)
	return f, nil
}
func (p *teamTraffic) Values() (map[string]string, error) {
	f, err := p.load()
	if err != nil {
		return nil, err
	}
	m, err := p.identities(f)
	if err != nil {
		return nil, err
	}
	portable := func(key string) string {
		if key == "" {
			return ""
		}
		if id := m.reverse[key]; id != "" {
			return id
		}
		return m.id(teams.Member{Key: key})
	}
	live := map[string]bool{}
	for _, t := range f.Teams {
		live[p.profile+"\x00"+t.ID] = true
	}
	for key := range m.cache.traffic {
		if strings.HasPrefix(key, p.profile+"\x00") && !live[key] {
			delete(m.cache.traffic, key)
		}
	}
	type window struct {
		id, raw string
		at      time.Time
	}
	var windows []window
	for _, t := range f.Teams {
		key := p.profile + "\x00" + t.ID
		stamp := teams.TrafficStamp(p.profile, t.ID)
		cached, known := m.cache.traffic[key]
		if !known || cached.stamp != stamp {
			lines, err := teams.TrafficForSync(p.profile, t.ID, p.device)
			if err != nil {
				return nil, err
			}
			m.cache.trafficReads++
			cached = teamTrafficWindow{stamp: stamp, lines: lines}
			if m.cache.traffic == nil {
				m.cache.traffic = map[string]teamTrafficWindow{}
			}
			m.cache.traffic[key] = cached
		}
		lines := append([]teams.Entry(nil), cached.lines...)
		for i := range lines {
			e := &lines[i]
			e.Member, e.FromKey, e.ToKey = portable(e.Member), portable(e.FromKey), portable(e.ToKey)
		}
		// Translation can lengthen keys, so bound again on their encoded form.
		for len(lines) > 0 {
			raw, _ := json.Marshal(lines)
			if len(raw) <= teams.TrafficSyncBytes {
				break
			}
			lines = lines[1:]
		}
		if len(lines) > 0 {
			windows = append(windows, window{t.ID, teamValue(lines), lines[len(lines)-1].At})
		}
	}
	sort.Slice(windows, func(i, j int) bool {
		if !windows[i].at.Equal(windows[j].at) {
			return windows[i].at.After(windows[j].at)
		}
		return windows[i].id < windows[j].id
	})
	out := map[string]string{}
	size := 0
	for _, w := range windows {
		// Account for JSON escaping and the entry's name, scope, stamp and id as
		// well as its words. This keeps the encoded share below the device cap.
		raw, _ := json.Marshal(w.raw)
		cost := len(raw) + 2*(len(w.id)+len(p.device)) + 256
		if size+cost > teams.TrafficSyncDeviceBytes {
			continue
		}
		out[w.id] = w.raw
		size += cost
	}
	return out, nil
}
func (p *teamTraffic) Holds(name string) bool {
	f, err := p.load()
	if err != nil {
		return false
	}
	_, ok := f.Team(name)
	return ok
}
func (p *teamTraffic) Apply(values map[string]string) (err error) {
	// Display refresh is optional even when called from capture before a push.
	// Keep its failures out of DeviceWindows and every unrelated vault value.
	defer func() { teams.SharingError(p.profile, err); err = nil }()
	// Team records restore before Traffic, and may have added or deleted teams.
	p.snapshot = nil
	f, err := p.load()
	if err != nil {
		return err
	}
	// An empty profile has no mirror to filter. In particular, leave the
	// existing hosted polling cost unchanged when no team uses this road.
	if len(f.Teams) == 0 {
		return nil
	}
	m, err := p.identities(f)
	if err != nil {
		return err
	}
	peers := map[string]bool{}
	if p.peers != nil {
		peers, err = p.peers()
		if err != nil {
			return err
		}
	}
	byTeam := map[string][]teams.Entry{}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		id, device, ok := strings.Cut(name, "/")
		if !ok || device == p.device || (peers != nil && !peers[device]) {
			continue
		}
		if _, ok := f.Team(id); !ok {
			continue
		}
		var lines []teams.Entry
		if err := json.Unmarshal([]byte(values[name]), &lines); err != nil {
			return err
		}
		if len(lines) > teams.TrafficSyncLines || len(values[name]) > teams.TrafficSyncBytes {
			continue
		}
		for _, e := range lines {
			if !strings.HasPrefix(e.ID, device+"/") {
				continue
			}
			realized := func(key string) string {
				if key == "" {
					return ""
				}
				return m.realized(teams.Member{Key: key}).Key
			}
			e.Member, e.FromKey, e.ToKey = realized(e.Member), realized(e.FromKey), realized(e.ToKey)
			byTeam[id] = append(byTeam[id], e)
		}
	}
	for _, t := range f.Teams {
		if err := teams.WriteTrafficMirror(p.profile, t.ID, byTeam[t.ID]); err != nil {
			teams.SharingError(p.profile, err)
		}
	}
	return nil
}

// pairedTrafficDevices reads only the roster already held by the local learner.
// Without that roster, display every other writer until ordinary fleet refresh
// supplies it; a vault round must never add a directory request.
func (s *Sync) pairedTrafficDevices() (map[string]bool, error) {
	l, local, err := s.displayListing(context.Background())
	if err != nil {
		teams.SharingError(s.profile(), err)
	}
	if !local || err != nil {
		return nil, nil
	}
	peers := map[string]bool{}
	for id, d := range l.Devices {
		if !d.Revoked {
			peers[id] = true
		}
	}
	return peers, nil
}

// rebindChat runs after continuation has chosen the final local folder. Keeping
// the portable identity beside the new path prevents a take from becoming a
// membership removal and addition on the next profile round.
func (p teamKeys) rebindChat(id string, at cell.Cell) error {
	snapshot, err := teams.Snapshot(p.profile)
	if err != nil {
		return err
	}
	matches := func(member teams.Member) bool {
		return member.SyncID == "cell:"+id || member.Key == teams.RemoteKey(id)
	}
	found := false
	for _, t := range snapshot.Teams {
		for _, member := range append(append([]teams.Member(nil), t.Members...), t.FormerMembers...) {
			found = found || matches(member)
		}
	}
	if !found {
		return nil
	}
	file := filepath.Join(at.Root, cell.TranscriptPath)
	key := teams.ConversationIdentity(file)
	where := workspaceOf(at.Root)
	return teams.Update(p.profile, func(f *teams.File) error {
		for i := range f.Teams {
			t := &f.Teams[i]
			for _, members := range [][]teams.Member{t.Members, t.FormerMembers} {
				for j := range members {
					member := &members[j]
					if !matches(*member) {
						continue
					}
					old := member.Key
					member.Key, member.File, member.Where, member.SyncID = key, file, where, "cell:"+id
					if t.Manager == old {
						t.Manager = key
					}
					if t.FormerManager == old {
						t.FormerManager = key
					}
				}
			}
		}
		return nil
	})
}
