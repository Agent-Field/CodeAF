package teams

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
)

// ── WHAT A TEAM HAS SPENT TODAY ─────────────────────────────────────────────
//
// A team's cap (teamsettings.go) is measured against what its conversations
// spent in one local day, and that figure already exists: every model call
// this machine makes is one line of the usage ledger (internal/session's
// usage_ledger.go), <home>/v3/usage.jsonl, carrying the local day, the cost,
// the 16-hex id of the conversation it was made in (session) and, for the
// work a conversation started, that conversation's id again (root). A member
// is known here by its transcript path, <bucket>/<id>/transcript.jsonl, and
// the id is its folder's name. So a team's spend on a day is the sum of the
// ledger's lines on that day whose session or root is one of its members, or
// a member of any team under it: a cap is a POOL over the subtree.
//
// A CONVERSATION IN SEVERAL COUNTED TEAMS IS COUNTED ONCE, and a line whose
// session and root are both members is one line. A sub-team closed today still
// counts toward its parent's day: the money was spent, and a closed team
// spends nothing more.
//
// IT IS READ ONCE AND THEN ONLY WHAT WAS APPENDED. The ledger is machine-wide
// and grows by a line per call, and the teams page asks about spend on its
// clock. So the ledger is folded, once per process, into totals by day and by
// (session, root) pair, and afterwards a read whose stamp has not moved is
// answered from memory and one that grew reads from the byte the last read
// stopped at. This package does not import internal/session (it imports this
// one), so it reads the five fields it needs itself; internal/session's
// TestTeamSpendReadsTheSessionsLedger pins that both name the same file.
//
// NOTHING HERE ENFORCES A CAP. It is the read model the session's cap check
// and the teams page's `$1.20 of $5 today` are both drawn from.

// UsageLedgerPath is the machine's usage ledger, the same path
// internal/session's UsageLedgerPath names.
func UsageLedgerPath() string { return home.Join("v3", "usage.jsonl") }

// dayLayout is the ledger's day, the LOCAL calendar day.
const dayLayout = "2006-01-02"

// Today is today's local day in the ledger's spelling.
func Today() string { return time.Now().Local().Format(dayLayout) }

// Spend is what a team and every team under it spent on one day.
type Spend struct {
	Team  string  `json:"team"`
	Day   string  `json:"day"`
	USD   float64 `json:"usd"`
	Calls int     `json:"calls"`
	// ByMember is each counted conversation's share, by conversation key.
	ByMember map[string]float64 `json:"by_member,omitempty"`
}

// Total is what a team and every team under it has cost, all time: every
// ledger line whose conversation belongs to the subtree, whole conversation
// chains, money following the conversation (membership intervals), floored at
// the owning team's Made. It is the figure of the team's own the teams page
// draws on its rail rows and in the pane; the day's spend against the cap
// ([Spend]) is the different figure the header keeps. A conversation in two
// teams counts in both, and once in the root team's.
type Total struct {
	Team  string  `json:"team"`
	USD   float64 `json:"usd"`
	Calls int     `json:"calls"`
	// ByMember is each counted conversation's share, by conversation key:
	// the line that shows one runaway member, and why the total is computed
	// per conversation at all.
	ByMember map[string]float64 `json:"by_member,omitempty"`
}

// TeamSpend is team teamID's spend on day ("2006-01-02", local) from this
// machine's usage ledger.
func TeamSpend(profileDir, teamID, day string) (Spend, error) {
	return TeamSpendIn(profileDir, UsageLedgerPath(), teamID, day)
}

// TeamSpendIn is [TeamSpend] against the ledger at ledger.
func TeamSpendIn(profileDir, ledger, teamID, day string) (Spend, error) {
	f, err := Load(profileDir)
	if err != nil {
		return Spend{}, err
	}
	out := Spend{Team: teamID, Day: day}
	team, ok := f.Team(teamID)
	if !ok {
		return out, nil
	}
	ids := map[string]string{} // session id -> member key
	for _, t := range append([]Team{team}, f.Descendants(teamID)...) {
		for _, m := range t.Members {
			for _, id := range sessionIDs(m.Key) {
				if _, ok := ids[id]; !ok {
					ids[id] = m.Key
				}
			}
		}
	}
	totals, err := spendCache.day(ledger, day)
	if err != nil {
		return out, err
	}
	for pair, sum := range totals {
		key, ok := ids[pair.session]
		if !ok {
			key, ok = ids[pair.root]
		}
		if !ok {
			continue
		}
		out.USD += sum.usd
		out.Calls += sum.calls
		if out.ByMember == nil {
			out.ByMember = map[string]float64{}
		}
		out.ByMember[key] += sum.usd
	}
	return out, nil
}

// TeamTotal is team teamID's total live cost, all time, from this machine's
// usage ledger.
func TeamTotal(profileDir, teamID string) (Total, error) {
	return TeamTotalIn(profileDir, UsageLedgerPath(), teamID)
}

// TeamTotalIn is [TeamTotal] against the ledger at ledger.
//
// WHOLE CHAINS, NOT ONE HOP. The day spend counts a line whose session or
// root is a member; the total also follows a root that is itself a delegated
// conversation, so a task's own sub-task lands on the member whose chain it
// sits under, at any depth. The owner of a line (its ByMember key) is the
// terminal member of its session's chain, falling back to its root's.
//
// MONEY FOLLOWS THE CONVERSATION. A line counts when its owner's membership
// of the subtree held it at the moment the line was written: inside one of
// the owner's [Member.JoinedAt, Member.LeftAt) intervals, a removal's LeftAt
// stamped by [File.RemoveMember], a removal written before the field existed
// left none and bounds it from above, and at or after the owning team's Made.
// Runs are kept per ledger line, so the boundary is exact even when a join or
// a removal lands mid-day.
func TeamTotalIn(profileDir, ledger, teamID string) (Total, error) {
	f, err := Load(profileDir)
	if err != nil {
		return Total{}, err
	}
	out := Total{Team: teamID}
	team, ok := f.Team(teamID)
	if !ok {
		return out, nil
	}
	ids := map[string]string{} // session id -> member key
	type interval struct{ from, to, made int64 }
	intervals := map[string][]interval{}
	for _, t := range append([]Team{team}, f.Descendants(teamID)...) {
		made := t.Made.UnixNano()
		for _, m := range t.Members {
			for _, id := range sessionIDs(m.Key) {
				if _, ok := ids[id]; !ok {
					ids[id] = m.Key
				}
			}
			intervals[m.Key] = append(intervals[m.Key], interval{from: m.JoinedAt.UnixNano(), made: made})
		}
		for _, m := range t.FormerMembers {
			for _, id := range sessionIDs(m.Key) {
				if _, ok := ids[id]; !ok {
					ids[id] = m.Key
				}
			}
			iv := interval{from: m.JoinedAt.UnixNano(), made: made}
			if !m.LeftAt.IsZero() {
				iv.to = m.LeftAt.UnixNano()
			}
			intervals[m.Key] = append(intervals[m.Key], iv)
		}
	}
	spendCache.mu.Lock()
	defer spendCache.mu.Unlock()
	fold, err := spendCache.refresh(ledger)
	if err != nil {
		return out, err
	}
	// The chain edges, session -> the conversation it delegated to; a chain
	// is followed up to a member, bounded by the seen set so a cycle cannot
	// hang it.
	parent := map[string]string{}
	for pair := range fold.pairs {
		if pair.session != "" && pair.root != "" {
			if _, ok := parent[pair.session]; !ok {
				parent[pair.session] = pair.root
			}
		}
	}
	terminal := func(start string) (string, bool) {
		seen := map[string]bool{}
		for at := start; at != ""; {
			if key, ok := ids[at]; ok {
				return key, true
			}
			if seen[at] {
				return "", false
			}
			seen[at] = true
			next, ok := parent[at]
			if !ok {
				return "", false
			}
			at = next
		}
		return "", false
	}
	for pair, runs := range fold.pairs {
		owner, ok := terminal(pair.session)
		if !ok && pair.root != "" {
			owner, ok = terminal(pair.root)
		}
		if !ok {
			continue
		}
		ivs := intervals[owner]
		if len(ivs) == 0 {
			continue
		}
		for _, run := range runs {
			inside := false
			for _, iv := range ivs {
				if run.at >= iv.from && (iv.to == 0 || run.at < iv.to) && run.at >= iv.made {
					inside = true
					break
				}
			}
			if !inside {
				continue
			}
			out.USD += run.usd
			out.Calls += run.calls
			if out.ByMember == nil {
				out.ByMember = map[string]float64{}
			}
			out.ByMember[owner] += run.usd
		}
	}
	return out, nil
}

// SpendStamp is one stamp over what a team's spend is read from: the teams
// file (membership) and the ledger. Equal stamps on one day are one answer.
func SpendStamp(profileDir, ledger string) string {
	return Stamp(profileDir) + "|" + stampOf(ledger)
}

// TeamSpendStamp is the stamp of one team's day as [TeamSpend] reads it: the
// day, the team, and [SpendStamp] over the machine's ledger. The engine and a
// local window both answer "same" from it.
func TeamSpendStamp(profileDir, teamID, day string) string {
	return day + "|" + teamID + "|" + SpendStamp(profileDir, UsageLedgerPath())
}

// TeamTotalStamp is the stamp of one team's total as [TeamTotal] reads it:
// the team and [SpendStamp] over the machine's ledger. The engine and a local
// window both answer "same" from it.
func TeamTotalStamp(profileDir, teamID string) string {
	return teamID + "|t|" + SpendStamp(profileDir, UsageLedgerPath())
}

// sessionIDs is the ledger ids a conversation key can carry: its folder's
// name, the ordinary layout, and for an older flat file its own name.
func sessionIDs(key string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	var out []string
	if dir := filepath.Base(filepath.Dir(key)); isSessionID(dir) {
		out = append(out, dir)
	}
	if base := strings.TrimSuffix(filepath.Base(key), filepath.Ext(key)); isSessionID(base) {
		out = append(out, base)
	}
	return out
}

// isSessionID reports whether s looks like a session id: 16 hex digits.
func isSessionID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// spendPair is where a ledger line's money went.
type spendPair struct{ session, root string }

type spendSum struct {
	usd   float64
	calls int
}

// spendRun is one ledger line, its time kept whole for the exact
// [JoinedAt, LeftAt) boundary the team's total counts on.
type spendRun struct {
	at    int64 // unix nanos
	usd   float64
	calls int
}

// ledgerFold is one ledger as read so far.
type ledgerFold struct {
	stamp  string
	offset int64
	days   map[string]map[spendPair]spendSum
	// pairs is every line's (session, root) pair, in order, with each line's
	// time and money: the fold the all-time total reads. Runs are per line
	// and never evicted, because dropping an old line would silently
	// undercount a total; that is roughly 24 bytes per ledger line, the
	// price of answering "what has this team cost" without re-reading the
	// ledger.
	pairs map[spendPair][]spendRun
}

// spendMemory is every ledger folded so far, shared by the process.
type spendMemory struct {
	mu      sync.Mutex
	ledgers map[string]*ledgerFold
	// reads counts the bytes read, for a test to see a quiet ledger costs none.
	reads int64
}

var spendCache spendMemory

// ledgerLine is the five fields of a usage line this package reads.
type ledgerLine struct {
	At      time.Time `json:"at"`
	Day     string    `json:"day"`
	Calls   int       `json:"calls"`
	USD     float64   `json:"usd"`
	Session string    `json:"session"`
	Root    string    `json:"root"`
}

// day is the ledger's totals for one day, a copy, brought up to date first.
func (m *spendMemory) day(path, day string) (map[spendPair]spendSum, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ledgers == nil {
		m.ledgers = map[string]*ledgerFold{}
	}
	f, err := m.refresh(path)
	if err != nil {
		return nil, err
	}
	out := make(map[spendPair]spendSum, len(f.days[day]))
	for k, v := range f.days[day] {
		out[k] = v
	}
	return out, nil
}

// refresh brings the fold of path up to date: nothing when its stamp has not
// moved, the appended bytes when it grew, the whole file when it is new here
// or shrank. The caller holds m.mu.
func (m *spendMemory) refresh(path string) (*ledgerFold, error) {
	if m.ledgers == nil {
		m.ledgers = map[string]*ledgerFold{}
	}
	stamp := stampOf(path)
	f := m.ledgers[path]
	if f != nil && f.stamp == stamp {
		return f, nil
	}
	if stamp == MissingStamp {
		f = &ledgerFold{stamp: stamp, days: map[string]map[spendPair]spendSum{}, pairs: map[spendPair][]spendRun{}}
		m.ledgers[path] = f
		return f, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if f == nil || info.Size() < f.offset {
		f = &ledgerFold{days: map[string]map[spendPair]spendSum{}, pairs: map[spendPair][]spendRun{}}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.Seek(f.offset, io.SeekStart); err != nil {
		return nil, err
	}
	r := bufio.NewReaderSize(file, 32<<10)
	for {
		raw, err := r.ReadBytes('\n')
		if len(raw) > 0 && raw[len(raw)-1] == '\n' {
			m.reads += int64(len(raw))
			f.offset += int64(len(raw))
			var line ledgerLine
			if json.Unmarshal(raw, &line) == nil && (line.Session != "" || line.Root != "") {
				day := line.Day
				if day == "" && !line.At.IsZero() {
					day = line.At.Local().Format(dayLayout)
				}
				calls := line.Calls
				if calls == 0 {
					calls = 1
				}
				totals := f.days[day]
				if totals == nil {
					totals = map[spendPair]spendSum{}
					f.days[day] = totals
				}
				pair := spendPair{session: line.Session, root: line.Root}
				sum := totals[pair]
				sum.usd += line.USD
				sum.calls += calls
				totals[pair] = sum
				f.pairs[pair] = append(f.pairs[pair], spendRun{at: line.At.UnixNano(), usd: line.USD, calls: calls})
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	f.stamp = stamp
	m.ledgers[path] = f
	return f, nil
}
