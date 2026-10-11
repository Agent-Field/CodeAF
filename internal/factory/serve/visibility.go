package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// repoVisibilityTTL is how long one GitHub answer is reused. A queue paint
// must not ask once per repository, and a revoked token is noticed on the
// next miss rather than held for the life of the process.
const repoVisibilityTTL = 10 * time.Minute

// viewerState remembers who has signed in and which repositories their
// GitHub token can read. The token itself is process memory only.
// LOCK ORDER: accessSessions.mu before viewerState.mu, never the reverse.
type viewerState struct {
	mu     sync.Mutex
	people map[string]memberMemory
	grants map[string]repoGrant
	github map[string]string
}

type memberMemory struct {
	profile   api.Profile
	joined    time.Time
	lastSeen  time.Time
	invitedBy string
}

type repoGrant struct {
	allow bool
	until time.Time
}

func newViewerState() *viewerState {
	return &viewerState{
		people: map[string]memberMemory{},
		grants: map[string]repoGrant{},
		github: map[string]string{},
	}
}

// see records a signed-in person. An empty token does not forget one already
// held, and a later sign-in does not move joined earlier than the first.
func (v *viewerState) see(profile api.Profile, invitedBy string, joined, seen time.Time, token string) {
	if v == nil || profile.Handle == "" {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	known := v.people[profile.Handle]
	if known.profile.Handle == "" {
		known.profile = profile
		known.joined = joined
		known.invitedBy = invitedBy
	} else {
		if profile.Name != "" {
			known.profile.Name = profile.Name
		}
		if profile.AvatarURL != "" {
			known.profile.AvatarURL = profile.AvatarURL
		}
		if len(profile.Scopes) > 0 {
			known.profile.Scopes = profile.Scopes
		}
		if invitedBy != "" && known.invitedBy == "" {
			known.invitedBy = invitedBy
		}
		if !joined.IsZero() && (known.joined.IsZero() || joined.Before(known.joined)) {
			known.joined = joined
		}
	}
	if seen.After(known.lastSeen) {
		known.lastSeen = seen
	}
	v.people[profile.Handle] = known
	if token != "" {
		v.github[profile.Handle] = token
	}
}

func (v *viewerState) token(handle string) string {
	if v == nil {
		return ""
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.github[handle]
}

// releaseToken drops the GitHub token once no live session of this handle
// remains. The caller holds accessSessions.mu.
func (h *Handler) releaseToken(handle string) {
	if handle == "" || h.viewer == nil {
		return
	}
	for _, session := range h.sessions.ids {
		if session.profile != nil && session.profile.Handle == handle && h.sessionUsable(session) {
			return
		}
	}
	h.viewer.mu.Lock()
	delete(h.viewer.github, handle)
	h.viewer.mu.Unlock()
}

// itemRepo is the GitHub owner/name a job belongs to. A short name joins the
// product; anything that is not owner/name is not a repository we can check.
func itemRepo(it factory.Item) string {
	repo := strings.ToLower(strings.TrimSpace(it.Repo))
	if repo == "" {
		return ""
	}
	if strings.Count(repo, "/") == 1 {
		return repo
	}
	if strings.Contains(repo, "/") {
		return ""
	}
	product := strings.ToLower(strings.TrimSpace(it.Product))
	if product == "" || strings.Contains(product, "/") {
		return ""
	}
	return product + "/" + repo
}

func repoKeys(s factory.Snapshot) []string {
	keys := make([]string, 0, len(s.Items)+len(s.Repos))
	for _, it := range s.Items {
		keys = append(keys, itemRepo(it))
	}
	for _, repo := range s.Repos {
		keys = append(keys, repo.Name)
	}
	for _, src := range s.Sources {
		keys = append(keys, src.Repos...)
	}
	for name := range s.Checkouts {
		keys = append(keys, name)
	}
	return keys
}

// repoAccess is the set of owner/name keys this caller may read. filter is
// false for the owner: loopback without a GitHub profile, and the host token.
// A signed-in profile with no token still filters, and sees nothing.
func (h *Handler) repoAccess(r *http.Request, keys []string) (map[string]bool, bool) {
	session, ok := h.credential(r)
	if !ok || session.profile == nil || session.profile.Handle == "" {
		return nil, false
	}
	token := session.githubToken
	if token == "" && h.viewer != nil {
		token = h.viewer.token(session.profile.Handle)
	}
	allow := map[string]bool{}
	if token == "" {
		return allow, true
	}
	seen := map[string]bool{}
	for _, key := range keys {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || strings.Count(key, "/") != 1 || seen[key] {
			continue
		}
		seen[key] = true
		allow[key] = h.repoAllowed(r, token, key)
	}
	return allow, true
}

func (h *Handler) repoVisible(r *http.Request, name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	allow, filter := h.repoAccess(r, []string{name})
	if !filter {
		return true
	}
	return name != "" && allow[name]
}

func (h *Handler) repoAllowed(r *http.Request, token, key string) bool {
	sum := sha256.Sum256([]byte(token))
	cacheKey := hex.EncodeToString(sum[:]) + "\n" + key
	now := h.now()
	if h.viewer != nil {
		h.viewer.mu.Lock()
		grant, ok := h.viewer.grants[cacheKey]
		h.viewer.mu.Unlock()
		if ok && now.Before(grant.until) {
			return grant.allow
		}
	}
	allow, cache := h.askRepo(r, token, key)
	if cache && h.viewer != nil {
		h.viewer.mu.Lock()
		h.viewer.grants[cacheKey] = repoGrant{allow: allow, until: h.now().Add(repoVisibilityTTL)}
		h.viewer.mu.Unlock()
	}
	return allow
}

// askRepo is GitHub's own answer for this token. A 5xx or a transport failure
// is not cached: the next read tries again. 200 is readable; anything else
// definitive is not.
func (h *Handler) askRepo(r *http.Request, token, key string) (allow, cache bool) {
	owner, name, ok := strings.Cut(key, "/")
	if !ok || owner == "" || name == "" {
		return false, true
	}
	endpoint := "https://api.github.com/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
	status, err := h.githubStatus(r, http.MethodGet, endpoint, token)
	if err != nil || status >= 500 {
		return false, false
	}
	return status == http.StatusOK, true
}

// forViewer narrows a floor to the repositories this caller can read.
// The owner gets the snapshot unchanged. A viewer who can see every
// repository also does, so factory-wide totals stay intact. A partial
// viewer gets recomputed job totals, and since-counters cleared, because
// those counters are not stored per repository. The bool is true when
// the returned snapshot is still the whole floor.
func (h *Handler) forViewer(r *http.Request, s factory.Snapshot) (factory.Snapshot, string, bool) {
	allow, filter := h.repoAccess(r, repoKeys(s))
	if !filter {
		return s, "", true
	}
	if !anyAllowed(allow) {
		return emptyView(s), api.ReasonNoVisibleRepos, false
	}
	if coversFloor(s, allow) {
		return s, "", true
	}
	return narrowView(s, allow), "", false
}

func anyAllowed(allow map[string]bool) bool {
	for _, ok := range allow {
		if ok {
			return true
		}
	}
	return false
}

func coversFloor(s factory.Snapshot, allow map[string]bool) bool {
	for _, it := range s.Items {
		key := itemRepo(it)
		if key == "" || !allow[key] {
			return false
		}
	}
	for _, repo := range s.Repos {
		if githubNameHidden(repo.Name, allow) {
			return false
		}
	}
	for _, src := range s.Sources {
		for _, name := range src.Repos {
			if githubNameHidden(name, allow) {
				return false
			}
		}
	}
	for name := range s.Checkouts {
		if githubNameHidden(name, allow) {
			return false
		}
	}
	return true
}

func githubNameHidden(name string, allow map[string]bool) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	return strings.Count(key, "/") == 1 && !allow[key]
}

func emptyView(s factory.Snapshot) factory.Snapshot {
	out := s
	out.Items = []factory.Item{}
	out.Repos = []factory.Repo{}
	out.Benches = 0
	out.Daily = 0
	out.Rail = 0
	out.Shift = factory.Shift{Since: s.Shift.Since}
	out.Busy = nil
	out.BusyAll = ""
	out.LastReadCost = 0
	out.Marked = nil
	if s.Checkouts != nil {
		out.Checkouts = map[string]string{}
	}
	out.Sources = blankSources(s.Sources)
	return out
}

func narrowView(s factory.Snapshot, allow map[string]bool) factory.Snapshot {
	out := s
	items := make([]factory.Item, 0, len(s.Items))
	ids := map[int]bool{}
	var daily float64
	for _, it := range s.Items {
		key := itemRepo(it)
		if key == "" || !allow[key] {
			continue
		}
		items = append(items, it)
		ids[it.ID] = true
		if it.Stream != nil {
			daily += it.Stream.Spent
		}
	}
	repos := make([]factory.Repo, 0, len(s.Repos))
	for _, repo := range s.Repos {
		if nameAllowed(repo.Name, allow) {
			repos = append(repos, repo)
		}
	}
	out.Items = items
	out.Repos = repos
	out.Daily = daily
	out.Shift = factory.Shift{Since: s.Shift.Since}
	out.BusyAll = ""
	out.LastReadCost = 0
	if s.Busy != nil {
		next := map[int]string{}
		for id, word := range s.Busy {
			if ids[id] {
				next[id] = word
			}
		}
		out.Busy = next
	}
	marked := make([]int, 0, len(s.Marked))
	for _, id := range s.Marked {
		if ids[id] {
			marked = append(marked, id)
		}
	}
	out.Marked = marked
	if s.Checkouts != nil {
		next := map[string]string{}
		for name, dir := range s.Checkouts {
			if nameAllowed(name, allow) {
				next[name] = dir
			}
		}
		out.Checkouts = next
	}
	sources := blankSources(s.Sources)
	for i := range sources {
		sources[i].Repos = keepNames(s.Sources[i].Repos, allow)
	}
	out.Sources = sources
	return out
}

func blankSources(in []factory.SourceInfo) []factory.SourceInfo {
	out := make([]factory.SourceInfo, len(in))
	copy(out, in)
	for i := range out {
		out[i].Repos = nil
		out[i].Items = 0
		out[i].Read = 0
		out[i].Of = 0
	}
	return out
}

func nameAllowed(name string, allow map[string]bool) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if allow[name] {
		return true
	}
	if strings.Contains(name, "/") {
		return false
	}
	for key, ok := range allow {
		if !ok {
			continue
		}
		_, tail, found := strings.Cut(key, "/")
		if found && tail == name {
			return true
		}
	}
	return false
}

func keepNames(names []string, allow map[string]bool) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if allow[strings.ToLower(strings.TrimSpace(name))] {
			out = append(out, name)
		}
	}
	return out
}

// filterAudit drops events whose job is not in a repository this viewer can
// read. An event with no job stays. An event whose job is gone from the
// floor is dropped, so a hidden job cannot be inferred from the record.
func (h *Handler) filterAudit(r *http.Request, events []record.Event) []record.Event {
	if h.seam.Load == nil {
		return []record.Event{}
	}
	snap, err := h.seam.Load()
	if err != nil {
		return []record.Event{}
	}
	byID := map[int]string{}
	keys := make([]string, 0, len(snap.Items))
	for _, it := range snap.Items {
		key := itemRepo(it)
		byID[it.ID] = key
		keys = append(keys, key)
	}
	allow, _ := h.repoAccess(r, keys)
	out := make([]record.Event, 0, len(events))
	for _, event := range events {
		if event.Job == 0 || allow[byID[event.Job]] {
			out = append(out, event)
		}
	}
	return out
}

// viewerWantsTick drops a changed tick that names a job this caller cannot
// see. A "*" tick still wakes a re-read: the read itself is filtered, and
// the tick does not name a repository.
func (h *Handler) viewerWantsTick(r *http.Request, data string) bool {
	if _, filter := h.repoAccess(r, nil); !filter {
		return true
	}
	data = strings.TrimSpace(data)
	if data == "*" || data == "" {
		return true
	}
	id, err := strconv.Atoi(data)
	if err != nil || h.seam.Load == nil {
		return false
	}
	snap, err := h.seam.Load()
	if err != nil {
		return false
	}
	for _, it := range snap.Items {
		if it.ID == id {
			return h.repoVisible(r, itemRepo(it))
		}
	}
	return false
}
