package direction

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// ── THE LOAD MODEL (L10) ────────────────────────────────────────────────────
//
// 50,000 lifetime records of which 10,000 are live, a 2,000-folder DAG where
// every folder below the roots has two parents, 100,000 placements and 20,000
// chats. Every live governing rule was imported and re-imported twenty times,
// so provenance is read against a real import history, and every finding came
// from shared context. It is written straight into a real store — the fixture
// is scale, not behaviour, and the behaviour tests use the write API — and the
// live index is then derived by the real Rebuild. The store is closed once, so
// PRAGMA optimize leaves the statistics a long-used store would have.
//
// ITS TIME IS FIXED. Every date in it is drawn back from loadNow and every
// resolve over it runs at loadNow, so the pending window selects the same
// proposals on any day the suite runs.

var updateGoldens = flag.Bool("update-explain", false, "rewrite testdata/explain from the load model's plans")

const (
	loadFolders    = 2000
	loadChats      = 20000
	loadPlacements = 100000
	loadRecords    = 50000
	loadLive       = 10000
	loadRepos      = 50
	loadSubjects   = 1000
	loadReimports  = 20
)

var loadNow = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

// openLoad opens the load model at its own fixed time.
func openLoad(t *testing.T, path string) *Store {
	t.Helper()
	s, err := OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return loadNow }
	return s
}

var loadLayers = []int{8, 24, 64, 160, 400, 800, 544}

type loadModelFixture struct {
	path     string
	subjects []Subject
	counts   map[string]int
}

var (
	loadOnce sync.Once
	loaded   *loadModelFixture
	loadErr  error
	loadDir  string
)

func TestMain(m *testing.M) {
	code := m.Run()
	if loadDir != "" {
		_ = os.RemoveAll(loadDir)
	}
	os.Exit(code)
}

func loadModel(t *testing.T) *loadModelFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("the load model is not built under -short")
	}
	loadOnce.Do(func() {
		loadDir, loadErr = os.MkdirTemp("", "direction-load-")
		if loadErr == nil {
			start := time.Now()
			loaded, loadErr = buildLoadModel(filepath.Join(loadDir, "collections.db"))
			if loaded != nil {
				loaded.counts["build_ms"] = int(time.Since(start).Milliseconds())
			}
		}
	})
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	return loaded
}

func hexID(n int) string { return fmt.Sprintf("%032x", n) }

func buildLoadModel(path string) (*loadModelFixture, error) {
	ctx := context.Background()
	s, err := Open(path)
	if err != nil {
		return nil, err
	}
	rng := rand.New(rand.NewSource(20260910))
	now := loadNow
	fx := &loadModelFixture{path: path, counts: map[string]int{}}

	// Folders in layers; every folder below the roots has two parents in the
	// layer above, next to each other, so the DAG has diamonds everywhere and
	// a subject's ancestry stays the size real nesting has.
	var layers [][]string
	next := 1
	for _, size := range loadLayers {
		var layer []string
		for i := 0; i < size; i++ {
			layer = append(layer, hexID(next))
			next++
		}
		layers = append(layers, layer)
	}
	var folders, deep []string
	for i, layer := range layers {
		folders = append(folders, layer...)
		if i >= 3 {
			deep = append(deep, layer...)
		}
	}
	type task struct{ session, n string }
	var chats []string
	var tasks []task
	for i := 0; i < loadChats; i++ {
		chats = append(chats, fmt.Sprintf("chat-%05d", i))
	}
	err = s.ws.WriteImmediate(ctx, func(tx *sql.Tx) error {
		exec := func(query string) (*sql.Stmt, error) { return tx.PrepareContext(ctx, query) }
		folderStmt, err := exec("INSERT INTO collections(id,name) VALUES (?,?)")
		if err != nil {
			return err
		}
		for i, id := range folders {
			if _, err := folderStmt.ExecContext(ctx, id, fmt.Sprint("folder ", i)); err != nil {
				return err
			}
		}
		place, err := exec(`INSERT OR IGNORE INTO placements(collection_id,kind,ref_id,session_id,target_collection) VALUES (?,?,?,?,?)`)
		if err != nil {
			return err
		}
		placements := 0
		add := func(folder string, kind workspace.Kind, ref, session string) error {
			var target any
			if kind == workspace.CollectionKind {
				target = ref
			}
			res, err := place.ExecContext(ctx, folder, string(kind), ref, session, target)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			placements += int(n)
			return nil
		}
		for k := 1; k < len(layers); k++ {
			prev, cur := layers[k-1], layers[k]
			for j, child := range cur {
				p := j * len(prev) / len(cur)
				for _, parent := range []string{prev[p], prev[(p+1)%len(prev)]} {
					if err := add(parent, workspace.CollectionKind, child, ""); err != nil {
						return err
					}
				}
			}
		}
		for _, c := range chats {
			for n := 1 + rng.Intn(3); n > 0; n-- {
				if err := add(deep[rng.Intn(len(deep))], workspace.ConversationKind, c, ""); err != nil {
					return err
				}
			}
		}
		for i := 0; placements < loadPlacements-7000; i++ {
			tk := task{session: chats[rng.Intn(len(chats))], n: fmt.Sprint(1 + i)}
			tasks = append(tasks, tk)
			for n := 1 + rng.Intn(2); n > 0; n-- {
				if err := add(deep[rng.Intn(len(deep))], workspace.TaskKind, tk.n, tk.session); err != nil {
					return err
				}
			}
		}
		for i := 0; placements < loadPlacements; i++ {
			if i%2 == 0 {
				err = add(deep[rng.Intn(len(deep))], workspace.StandingKind, fmt.Sprintf("standing-%04d", i), "")
			} else {
				err = add(deep[rng.Intn(len(deep))], workspace.ArtifactKind, fmt.Sprintf("/work/repo-%02d/doc-%05d.md", i%loadRepos, i), "")
			}
			if err != nil {
				return err
			}
		}
		fx.counts["placements"] = placements
		member, err := exec(`INSERT OR IGNORE INTO memberships(collection_id,kind,ref_id,session_id,target_collection) VALUES (?,?,?,?,NULL)`)
		if err != nil {
			return err
		}
		for i := 0; i < loadChats; i++ {
			if _, err := member.ExecContext(ctx, folders[rng.Intn(len(folders))], "conversation", chats[rng.Intn(len(chats))], ""); err != nil {
				return err
			}
		}
		return writeLoadRecords(ctx, tx, rng, now, folders, chats, len(tasks), func(i int) (string, string) {
			return tasks[i].n, tasks[i].session
		}, fx.counts)
	})
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	if err := s.Rebuild(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	// Subjects: what a turn asks about — a chat, a task with its chat, or a
	// standing item with the chat that owns it — half with a physical root.
	for i := 0; i < loadSubjects; i++ {
		c := chats[rng.Intn(len(chats))]
		refs := []workspace.Ref{{Kind: workspace.ConversationKind, ID: c}}
		switch r := rng.Intn(10); {
		case r < 2:
			tk := tasks[rng.Intn(len(tasks))]
			refs = []workspace.Ref{{Kind: workspace.TaskKind, ID: tk.n, SessionID: tk.session}, {Kind: workspace.ConversationKind, ID: tk.session}}
		case r < 3:
			refs = append([]workspace.Ref{{Kind: workspace.StandingKind, ID: fmt.Sprintf("standing-%04d", 2*rng.Intn(3000))}}, refs...)
		}
		subj := Subject{Refs: refs, Phase: PhaseTask}
		if rng.Intn(2) == 0 {
			subj.Workspace = fmt.Sprintf("/work/repo-%02d", rng.Intn(loadRepos))
		}
		fx.subjects = append(fx.subjects, subj)
	}
	return fx, s.Close()
}

// writeLoadRecords writes 50,000 records with their whole histories:
// 3,000 governing, 2,000 pending and 5,000 informational live, and 40,000 that
// were rejected, withdrawn or superseded. The governing rules were imported
// from holds and re-imported loadReimports times — one revision and one
// mapping each time — and the findings from shared context.
func writeLoadRecords(ctx context.Context, tx *sql.Tx, rng *rand.Rand, now time.Time, folders, chats []string,
	tasks int, task func(int) (string, string), counts map[string]int) error {
	records, err := tx.PrepareContext(ctx, "INSERT INTO direction_records(id,revision,created_at) VALUES (?,?,?)")
	if err != nil {
		return err
	}
	revisions, err := tx.PrepareContext(ctx, `INSERT INTO direction_revisions(record_id,revision,kind,state,title,text,text_sha256,
 quote_origin,source_class,source_id,author_class,author_ref,receipt_actor,receipt_door,receipt_ref,receipt_at,written_at)
 VALUES (?,?,?,?,?,?,?,'adopted_wording','conversation',?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	targets, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO direction_targets(record_id,revision,position,target_kind,ref_id,session_id,reach) VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	exclusions, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO direction_exclusions(record_id,revision,target_kind,ref_id,session_id,at) VALUES (?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	links, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO direction_links(record_id,revision,link_kind,to_ref,to_revision) VALUES (?,?,?,?,0)`)
	if err != nil {
		return err
	}
	mappings, err := tx.PrepareContext(ctx, `INSERT INTO direction_legacy(source_store,source_id,source_version,source_sha256,record_id,revision,import_run)
 VALUES (?,?,?,?,?,?,'load-import')`)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO direction_import_runs(id,mode,binary,report_sha256,started_at) VALUES ('load-import','apply','fixture','',?)`,
		stamp(now)); err != nil {
		return err
	}
	type plan struct {
		kind       Kind
		states     []State
		n          int
		from       LegacyStore // imported from this store, or ""
		everywhere bool        // its first ten reach everywhere
	}
	reimported := make([]State, loadReimports)
	for i := range reimported {
		reimported[i] = Accepted
	}
	plans := []plan{
		{Rule, reimported, 3000, LegacyStanding, true},
		{Decision, []State{Proposed}, 2000, "", true},
		{Finding, []State{Informational}, 5000, LegacyContexts, true},
		{Rule, []State{Proposed, Rejected}, 10000, "", false},
		{Decision, []State{Proposed, Accepted, Withdrawn}, 15000, "", false},
		{Rule, []State{Proposed, Accepted, Superseded}, 15000, "", false},
	}
	var governing []string
	id := 1 << 40
	revs, targetRows, mapped := 0, 0, 0
	for _, p := range plans {
		for i := 0; i < p.n; i++ {
			id++
			rid := hexID(id)
			born := now.Add(-time.Duration(rng.Int63n(int64(90 * 24 * time.Hour))))
			if _, err := records.ExecContext(ctx, rid, len(p.states), stamp(born)); err != nil {
				return err
			}
			places := loadTargets(rng, folders, chats, tasks, task)
			// A handful of live records reach everywhere; they reach every subject.
			if i < 10 && p.everywhere {
				places = []Target{{Kind: TargetEverywhere, Ref: Everywhere, Reach: Direct}}
			}
			for rev, state := range p.states {
				at := born.Add(time.Duration(rev) * time.Hour)
				author, actor, door, ref, receiptAt := string(AuthorModel), "", "", "", ""
				switch {
				case p.from != "" && receiptRequired(state, AuthorMigration):
					author, actor, door, ref, receiptAt = string(AuthorMigration), string(ActorLegacyDelegated), string(DoorCard), "proposal", stamp(at)
				case p.from != "":
					author = string(AuthorMigration)
				case receiptRequired(state, AuthorPerson) || state == Superseded:
					author, actor, door, ref, receiptAt = string(AuthorPerson), string(ActorPerson), string(DoorCard), "card", stamp(at)
				}
				text := fmt.Sprintf("load record %d says something bounded about the work (%s, revision %d)", id, state, rev+1)
				if _, err := revisions.ExecContext(ctx, rid, rev+1, p.kind, state, fmt.Sprint("record ", id), text,
					fmt.Sprintf("%064x", id*64+rev), "chat", author, "ref", actor, door, ref, receiptAt, stamp(at)); err != nil {
					return err
				}
				revs++
				if p.from != "" {
					source := rid
					if p.from == LegacyStanding {
						source = fmt.Sprint("hold-", id)
					}
					if _, err := mappings.ExecContext(ctx, p.from, source, fmt.Sprint(rev+1), fmt.Sprintf("%064x", id*64+rev), rid, rev+1); err != nil {
						return err
					}
					mapped++
				}
				for pos, t := range places {
					if _, err := targets.ExecContext(ctx, rid, rev+1, pos, t.Kind, t.Ref, t.Session, t.Reach); err != nil {
						return err
					}
					targetRows++
				}
				live := rev == len(p.states)-1 && lane(p.kind, state) != laneNone
				if live && rng.Intn(20) == 0 {
					// Half name a chat, half a folder: the folder ones exercise the
					// path-by-path exclusion walk at scale.
					kind, ref := TargetConversation, chats[rng.Intn(len(chats))]
					if rng.Intn(2) == 0 {
						kind, ref = TargetCollection, folders[rng.Intn(len(folders))]
					}
					if _, err := exclusions.ExecContext(ctx, rid, rev+1, kind, ref, "", stamp(at)); err != nil {
						return err
					}
				}
				if live && state == Accepted && len(governing) > 0 && rng.Intn(33) == 0 {
					kind := []LinkKind{Overrides, ConflictsWith}[rng.Intn(2)]
					if _, err := links.ExecContext(ctx, rid, rev+1, kind, governing[rng.Intn(len(governing))]); err != nil {
						return err
					}
				}
			}
			if p.states[len(p.states)-1] == Accepted {
				governing = append(governing, rid)
			}
		}
	}
	counts["records"], counts["revisions"], counts["target_rows"], counts["legacy_mappings"] = id-(1<<40), revs, targetRows, mapped
	return nil
}

// loadTargets draws one to three places for a record: mostly folders (a third
// of them reaching their subtree), then chats, tasks and legacy workspaces.
func loadTargets(rng *rand.Rand, folders, chats []string, tasks int, task func(int) (string, string)) []Target {
	var out []Target
	for n := 1 + rng.Intn(3); n > 0; n-- {
		switch r := rng.Intn(100); {
		case r < 55:
			reach := Direct
			if rng.Intn(3) == 0 {
				reach = Subtree
			}
			out = append(out, Target{Kind: TargetCollection, Ref: folders[rng.Intn(len(folders))], Reach: reach})
		case r < 80:
			out = append(out, Target{Kind: TargetConversation, Ref: chats[rng.Intn(len(chats))], Reach: Direct})
		case r < 93:
			n, session := task(rng.Intn(tasks))
			out = append(out, Target{Kind: TargetTask, Ref: n, Session: session, Reach: Direct})
		default:
			out = append(out, Target{Kind: TargetLegacyWorkspace, Ref: fmt.Sprintf("/work/repo-%02d", rng.Intn(loadRepos)), Reach: Direct})
		}
	}
	return out
}

// countingQuerier counts the statements one resolve sends.
type countingQuerier struct {
	q     querier
	count int
}

func (c *countingQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	c.count++
	return c.q.QueryContext(ctx, query, args...)
}

func (c *countingQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	c.count++
	return c.q.QueryRowContext(ctx, query, args...)
}

// maxResolveStatements is the whole of what one resolve may send: the
// closure, the snapshot stamp, the candidates, their exclusions and links, the
// bodies and legacy names for the governing and pending lanes, and the
// informational page with its bodies and legacy names. It is a count of work,
// not of time (PERF.md): it cannot grow with the store, and a change that
// makes a resolve issue a statement per record fails here. Moving it moves
// PERF.md's "The direction resolver's cost" in the same commit.
const maxResolveStatements = 10

// THE RESOLVE COST DOES NOT GROW WITH THE STORE. Over the load model, every
// resolve sends at most maxResolveStatements statements, whatever the subject.
// The distributions of what it walked are logged for the build record.
func TestResolvingTheLoadModelSendsABoundedNumberOfStatements(t *testing.T) {
	fx := loadModel(t)
	s := openLoad(t, fx.path)
	defer s.Close()
	ctx := context.Background()
	var statements, edges, governing, pending, informational, blocked, legacy []int
	tooLarge := 0
	for _, subj := range fx.subjects {
		subj, err := subj.normalize()
		if err != nil {
			t.Fatal(err)
		}
		err = s.ws.ReadSnapshot(ctx, func(tx *sql.Tx) error {
			counter := &countingQuerier{q: tx}
			eff, err := resolveIn(ctx, counter, subj, s.now().UTC())
			statements = append(statements, counter.count)
			if errors.Is(err, ErrGoverningTooLarge) {
				tooLarge++
				return nil
			}
			if err != nil {
				return err
			}
			c, err := readClosure(ctx, tx, subj.Refs)
			if err != nil {
				return err
			}
			edges = append(edges, len(c.edges))
			governing = append(governing, len(eff.Governing))
			pending = append(pending, len(eff.Pending))
			informational = append(informational, len(eff.Informational.Items))
			n, named := 0, 0
			for _, a := range eff.Governing {
				n += len(a.Blocked)
				if a.Legacy != nil {
					named++
				}
			}
			blocked = append(blocked, n)
			legacy = append(legacy, named)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for name, v := range map[string]int{"folders": loadFolders, "chats": loadChats} {
		fx.counts[name] = v
	}
	t.Logf("load model: %v", fx.counts)
	t.Logf("statements per resolve %s", spread(statements))
	t.Logf("closure edges %s; governing %s (with a legacy name %s); pending %s; informational %s; blocked paths %s; over the governing bound %d of %d",
		spread(edges), spread(governing), spread(legacy), spread(pending), spread(informational), spread(blocked), tooLarge, len(fx.subjects))
	if worst := percentile(statements, 100); worst > maxResolveStatements {
		t.Fatalf("a resolve sent %d statements; the bound is %d", worst, maxResolveStatements)
	}
	if fx.counts["placements"] != loadPlacements || fx.counts["records"] != loadRecords {
		t.Fatalf("the load model is not the load model: %v", fx.counts)
	}
	if percentile(governing, 50) == 0 || percentile(legacy, 50) == 0 {
		t.Fatal("the median subject is governed by nothing, or by nothing imported; the fixture does not exercise the resolver")
	}
}

func percentile(xs []int, p int) int {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int(nil), xs...)
	sort.Ints(s)
	i := (len(s)*p + 99) / 100
	if i < 1 {
		i = 1
	}
	return s[i-1]
}

func spread(xs []int) string {
	return fmt.Sprintf("p50 %d p90 %d p99 %d max %d", percentile(xs, 50), percentile(xs, 90), percentile(xs, 99), percentile(xs, 100))
}

// ── EXPLAIN goldens ─────────────────────────────────────────────────────────

// scanAllowed is what a plan may scan: the short key lists the queries drive
// from, and the recursive walk's own queue. Anything else — above all
// direction_live and placements — is a scan of a table that grows (L10).
var scanAllowed = regexp.MustCompile(`^SCAN (q|c|x|keys|up|u|(\d+ )?CONSTANT ROWS?)$`)

// A statement's name is the comment named() puts first.
var statementName = regexp.MustCompile(`^/\* ([a-z-]+) \*/ `)

// A plan's row counts and subquery numbers vary with the length of a key list;
// they are spelled the same in the goldens whatever the length.
var (
	constantRows   = regexp.MustCompile(`SCAN (\d+ )?CONSTANT ROWS?`)
	subqueryNumber = regexp.MustCompile(`SUBQUERY \d+`)
)

type plannedQuery struct {
	name  string
	query string
	args  []any
}

// recordingQuerier keeps every statement a resolve sends, with its arguments.
type recordingQuerier struct {
	q    querier
	sent []plannedQuery
}

func (r *recordingQuerier) note(query string, args []any) {
	name := "unnamed"
	if m := statementName.FindStringSubmatch(query); m != nil {
		name = m[1]
	}
	r.sent = append(r.sent, plannedQuery{name: name, query: query, args: append([]any(nil), args...)})
}

func (r *recordingQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.note(query, args)
	return r.q.QueryContext(ctx, query, args...)
}

func (r *recordingQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	r.note(query, args)
	return r.q.QueryRowContext(ctx, query, args...)
}

// resolveStatements are the names every resolve of a fully furnished subject
// sends. The plan test finds such a subject and plans what it actually sent.
var resolveStatements = []string{"closure", "snapshot", "candidates", "exclusions", "links", "bodies", "legacy", "informational"}

// sentByAResolve resolves subjects of the load model until one sends every
// statement in resolveStatements, and returns what that resolve sent.
func sentByAResolve(t *testing.T, s *Store, subjects []Subject) []plannedQuery {
	t.Helper()
	ctx := context.Background()
	for _, subj := range subjects {
		subj, err := subj.normalize()
		if err != nil {
			t.Fatal(err)
		}
		rec := &recordingQuerier{}
		err = s.ws.ReadSnapshot(ctx, func(tx *sql.Tx) error {
			rec.q = tx
			_, err := resolveIn(ctx, rec, subj, loadNow)
			return err
		})
		if err != nil && !errors.Is(err, ErrGoverningTooLarge) {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, pq := range rec.sent {
			seen[pq.name] = true
		}
		all := true
		for _, name := range resolveStatements {
			all = all && seen[name]
		}
		if all {
			return rec.sent
		}
	}
	t.Fatalf("no subject of the load model sends every one of %v; the fixture no longer exercises the resolver", resolveStatements)
	return nil
}

// writeStatements are the write path's hot lookups, spelled by the same
// variables the write path uses, with arguments of the right shape.
func writeStatements() []plannedQuery {
	return []plannedQuery{
		{"live-refresh", liveRefresh, []any{hexID(1<<40 + 1)}},
		{"rejected-text", rejectedText, []any{strings.Repeat("0", 64), Rejected}},
		{"legacy-key", legacyByContent, []any{"standing", "hold-1", strings.Repeat("0", 64)}},
		{"legacy-newest", legacyNewestBySource, []any{"standing", "hold-1"}},
	}
}

func explain(t *testing.T, s *Store, pq plannedQuery) []string {
	t.Helper()
	ctx := context.Background()
	var lines []string
	err := s.ws.ReadSnapshot(ctx, func(tx *sql.Tx) error {
		type step struct {
			id, parent int
			detail     string
		}
		steps, err := collect(ctx, tx, "EXPLAIN QUERY PLAN "+pq.query, pq.args, func(r scanner) (step, error) {
			var st step
			var unused int
			return st, r.Scan(&st.id, &st.parent, &unused, &st.detail)
		})
		if err != nil {
			return err
		}
		depth := map[int]int{0: -1}
		for _, st := range steps {
			depth[st.id] = depth[st.parent] + 1
			lines = append(lines, strings.Repeat("  ", depth[st.id])+st.detail)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("%s: %v", pq.name, err)
	}
	return lines
}

func checkNoScan(t *testing.T, where string, pq plannedQuery, plan []string) {
	t.Helper()
	for _, line := range plan {
		detail := strings.TrimSpace(line)
		if strings.HasPrefix(detail, "SCAN ") && !scanAllowed.MatchString(detail) {
			t.Errorf("%s: %s plans a scan of a table that grows: %q\n%s", where, pq.name, detail, strings.Join(plan, "\n"))
		}
	}
}

// QUERY PLANS ARE PART OF REVIEW (L10). The statements are the ones a resolve
// of the load model actually sent — captured, not listed by hand — plus the
// write path's hot lookups, spelled by the variables it uses. Each is planned
// against the load model with its statistics and checked in, by the name the
// statement carries; every execution of a name must plan the same way, no
// plan scans a table that grows, and every checked-in plan must still be a
// statement something sends. The same statements are planned against an
// empty store with no statistics, where the planner has nothing to go on, and
// must not scan either: CROSS JOIN pins the order, not luck.
func TestTheResolversPlansSeekAndNeverScanAGrowingTable(t *testing.T) {
	fx := loadModel(t)
	s := openLoad(t, fx.path)
	defer s.Close()
	statements := append(sentByAResolve(t, s, fx.subjects), writeStatements()...)
	planned := map[string]string{}
	for _, pq := range statements {
		if pq.name == "unnamed" {
			t.Errorf("a resolve sent a statement with no name, so no plan can be checked in for it:\n%s", pq.query)
			continue
		}
		plan := explain(t, s, pq)
		checkNoScan(t, "load model", pq, plan)
		got := constantRows.ReplaceAllString(strings.Join(plan, "\n"), "SCAN n CONSTANT ROWS")
		got = subqueryNumber.ReplaceAllString(got, "SUBQUERY n") + "\n"
		if prev, ok := planned[pq.name]; ok && prev != got {
			t.Errorf("%s plans two ways in one resolve:\n%s\n---\n%s", pq.name, prev, got)
		}
		planned[pq.name] = got
	}
	dir := filepath.Join("testdata", "explain")
	for name, got := range planned {
		golden := filepath.Join(dir, name+".txt")
		if *updateGoldens {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%s has no checked-in plan (go test -run %s ./internal/direction/ -args -update-explain): %v", name, t.Name(), err)
		}
		if string(want) != got {
			t.Errorf("%s's plan changed; review it and rerun with -update-explain if it is right:\nwant\n%s\ngot\n%s", name, want, got)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".txt")
		if _, ok := planned[name]; !ok {
			if *updateGoldens {
				_ = os.Remove(filepath.Join(dir, e.Name()))
				continue
			}
			t.Errorf("testdata/explain/%s is the plan of a statement nothing sends; remove it", e.Name())
		}
	}
	empty := openTest(t)
	for _, pq := range statements {
		checkNoScan(t, "empty store", pq, explain(t, empty, pq))
	}
}

// ── the measurement ─────────────────────────────────────────────────────────

// THE p99 IS MEASURED, NOT GATED. PERF.md allows no wall-clock threshold in
// the suite, so this runs only when asked for, on the machine the number is
// claimed for:
//
//	AFORGE_DIRECTION_MEASURE=1 go test -run TestMeasureResolveLatency -v ./internal/direction/
//
// Each of the 1,000 subjects pays what a turn pays: open the store (and its
// schema check), resolve, close (and its optimize). The design's budget is
// p99 ≤ 50 ms on Spark; the hard requirement is sub-second.
func TestMeasureResolveLatency(t *testing.T) {
	if os.Getenv("AFORGE_DIRECTION_MEASURE") == "" {
		t.Skip("set AFORGE_DIRECTION_MEASURE=1 to measure resolve latency on this machine")
	}
	fx := loadModel(t)
	ctx := context.Background()
	perCall := make([]time.Duration, 0, len(fx.subjects))
	resolveOnly := make([]time.Duration, 0, len(fx.subjects))
	for _, subj := range fx.subjects {
		start := time.Now()
		s := openLoad(t, fx.path)
		opened := time.Now()
		if _, err := s.Resolve(ctx, subj); err != nil && !errors.Is(err, ErrGoverningTooLarge) {
			t.Fatal(err)
		}
		resolved := time.Now()
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		perCall = append(perCall, time.Since(start))
		resolveOnly = append(resolveOnly, resolved.Sub(opened))
	}
	report := func(name string, ds []time.Duration) time.Duration {
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		at := func(p int) time.Duration { return ds[(len(ds)*p+99)/100-1] }
		t.Logf("%s over %d subjects: p50 %v  p90 %v  p99 %v  max %v", name, len(ds), at(50), at(90), at(99), ds[len(ds)-1])
		return at(99)
	}
	p99 := report("open+resolve+close", perCall)
	report("resolve only", resolveOnly)
	// What a full integrity check costs here, for the decision not to run
	// one on every open (BUILD-T03B): it reads every current record.
	s := openLoad(t, fx.path)
	start := time.Now()
	if err := s.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("Verify over the load model: %v", time.Since(start))
	_ = s.Close()
	if p99 > 50*time.Millisecond {
		t.Errorf("p99 %v is over the design's 50 ms budget", p99)
	}
}
