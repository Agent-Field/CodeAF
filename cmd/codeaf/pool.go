// The Model Pool from the command line.
//
// `codeaf pool` answers what the pool is on this machine: the mode and the
// addresses in force with the word saying where each came from, the index
// cached from the last good read, and — under status — what is waiting to be
// sent and whether the relay and the mirror answered. It spends nothing, and
// reaches the network only under `status` and `verify`, each of which fetches
// the index: `status` to say whether an address answers, `verify` to check a
// fresh one's signature. THE KEY STANDS IN FRONT OF THE FETCH: a signature
// nobody can check is a fetch nobody should make, so a build left with no key
// in hand refuses verify at the door rather than downloading bytes it cannot
// vouch for.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/index"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
	"github.com/Agent-Field/codeaf/internal/pool/poolcfg"
	"github.com/Agent-Field/codeaf/internal/pool/poolkey"
	"github.com/Agent-Field/codeaf/internal/pool/pull"
	"github.com/Agent-Field/codeaf/internal/pool/record"
	"github.com/Agent-Field/codeaf/internal/tui2/reltime"
)

// poolPublicKeys are the ed25519 public keys a fetched index's signature is
// checked under when no key is stored beside the pool row. The list carries
// the key the index signer publishes (relay/wrangler.toml's POOL_PUBLIC_KEY),
// and it decodes it once, at init, in the one package that spells the literal
// ([poolkey]): a build whose literal could not decode would verify nothing, so
// the test beside the verb pins the length.
var poolPublicKeys = poolkey.Keys()

// poolTrustedKeys resolves the keys a fetched index is checked under: the
// stored key when one is set, AND ONLY IT — the row is the one word the
// install trusts, so a word that does not decode is a key nobody can vouch
// for, and the answer is no keys rather than a fall back to the built-in one
// a fetch under would verify nothing with. Nothing stored: the key the build
// carries. Whether the stored word was there at all or there and broken is
// [poolTrustedKeysErr]'s to say.
func poolTrustedKeys(cfg poolcfg.Config) []ed25519.PublicKey {
	keys, _ := poolTrustedKeysErr(cfg)
	return keys
}

// poolTrustedKeysErr is the same answer with the stored word's health beside
// it: a key set but not decodable is not the ordinary nothing — the install
// holds a word it cannot vouch for anything under, and the verb that would
// fetch is refused with the row's name rather than left silent.
func poolTrustedKeysErr(cfg poolcfg.Config) ([]ed25519.PublicKey, error) {
	word := strings.TrimSpace(cfg.PublicKey)
	if word == "" {
		return poolPublicKeys, nil
	}
	raw, err := base64.StdEncoding.DecodeString(word)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errPoolKeyDoesNotDecode
	}
	return []ed25519.PublicKey{ed25519.PublicKey(raw)}, nil
}

// errPoolKeyDoesNotDecode marks a stored word that is not a key; the sentence
// a person reads is [poolKeyReason]'s, which names the word where it was set.
var errPoolKeyDoesNotDecode = errors.New("the stored public key does not decode")

// poolKeyReason is the one line a key that does not decode is refused by: the
// row when the word was stored, the environment pin when poolcfg resolved it
// from there — the remedy belongs to whichever word is in force.
func poolKeyReason(cfg poolcfg.Config) string {
	if cfg.Source.PublicKey == "env" {
		return "CODEAF_MODEL_POOL_PUBLIC_KEY does not decode (set it to the base64 Ed25519 public key of your relay, or clear it)"
	}
	return "models.pool.public_key does not decode (set it to the base64 Ed25519 public key of your relay, or clear it)"
}

// poolKeys is --key's value: a base64 ed25519 public key, repeatable, each
// decoded the moment it is typed rather than carried as text and decoded at
// the fetch. A key that does not decode is refused where it was typed.
type poolKeys []ed25519.PublicKey

func (k *poolKeys) String() string {
	if len(*k) == 0 {
		return ""
	}
	return fmt.Sprintf("%d key(s)", len(*k))
}

func (k *poolKeys) Set(word string) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(word))
	if err != nil {
		return fmt.Errorf("--key: %q is not base64", word)
	}
	if len(raw) != ed25519.PublicKeySize {
		return fmt.Errorf("--key: %d bytes, want an ed25519 public key's %d", len(raw), ed25519.PublicKeySize)
	}
	*k = append(*k, ed25519.PublicKey(raw))
	return nil
}

func runPool(args []string) error {
	return runPoolWith(args, os.Stdout, config.ProfileDir(), time.Now, os.LookupEnv)
}

// runPoolWith is the verb with its outside readings injectable: where the
// profile is, what time it is, and the environment the resolver reads — the
// last so a test can pin the pool's names without touching the process. The
// stored word is resolved once, here, and every form below answers from that
// one Config. The pool's files live in the profile, and the profile is
// resolved the way every other file under it is ([config.ProfilePath]) —
// emptiness is the state root's own profile, never a directory called "pool"
// beside wherever the command happened to run.
func runPoolWith(args []string, output io.Writer, profileDir string, now func() time.Time, lookup func(string) (string, bool)) error {
	cfg := poolcfg.Resolve(config.ModelPoolSettingAt(profileDir), config.ModelPoolPublicKeySettingAt(profileDir), lookup)
	poolDir := config.ProfilePath(profileDir, "pool")
	if len(args) == 0 {
		args = []string{"show"}
	}
	switch args[0] {
	case "show":
		return showPool(args[1:], output, poolDir, cfg, now)
	case "status":
		return statusPool(args[1:], output, poolDir, cfg, now)
	case "verify":
		return verifyPool(args[1:], output, poolDir, cfg, now)
	default:
		// `codeaf pool --help` reaches here rather than a flag set, because
		// the reading form parses nothing at all (usage.go).
		if askedForHelp(args) {
			return commandHelp("pool")
		}
		return fmt.Errorf("usage: codeaf pool [show|status|verify] [--json]")
	}
}

// showPool is the reading form. statusPool is the same answer with the outbox,
// the two doors the mode opens, and the relay's own answer added to it — the
// last is why status, alone among the reading forms, asks the network.
func showPool(args []string, output io.Writer, poolDir string, cfg poolcfg.Config, now func() time.Time) error {
	flags := commandFlags("pool show")
	asJSON := flags.Bool("json", false, "print the answer as one JSON object")
	// --cells, and not a fourth verb: the cells are part of what show shows —
	// the held document read one layer deeper, under the metric lines the form
	// already prints — so they ride show's own answer and its --json rather
	// than a `pool cells` verb that would carry a summary of its own.
	withCells := flags.Bool("cells", false, "list the held index's cells, one per line")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: codeaf pool show [--json] [--cells]")
	}
	return printPool(output, poolDir, cfg, now(), *asJSON, *withCells, false, nil)
}

func statusPool(args []string, output io.Writer, poolDir string, cfg poolcfg.Config, now func() time.Time) error {
	flags := commandFlags("pool status")
	asJSON := flags.Bool("json", false, "print the answer as one JSON object")
	var keys poolKeys
	flags.Var(&keys, "key", "an ed25519 public key, base64; repeatable")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: codeaf pool status [--json] [--key key]")
	}
	return printPool(output, poolDir, cfg, now(), *asJSON, false, true, keys)
}

// printPool is the reading form's whole answer. The config first — every value
// beside the word saying where it came from, one of default, setting, env or
// ci — then the cached index with its age, its cells under --cells, then the
// install's own sheet, then, for status, the outbox and the two doors the
// mode opens.
func printPool(output io.Writer, poolDir string, cfg poolcfg.Config, now time.Time, asJSON, withCells, withStatus bool, keys []ed25519.PublicKey) error {
	// The cache is read the way show reads everything else, as an answer and
	// not as an argument: a document that does not parse is not there yet,
	// and the line below says so. The signature and the puller's version mark
	// are verify's business; show reports what a person has.
	cached := readCachedIndex(poolDir)
	if asJSON {
		return printPoolJSON(output, poolDir, cfg, cached, now, withCells, withStatus, keys)
	}
	for _, line := range []string{
		fmt.Sprintf("mode %s · %s", cfg.Mode, cfg.Source.Mode),
		fmt.Sprintf("relay %s · %s", cfg.RelayURL, cfg.Source.RelayURL),
		fmt.Sprintf("index %s · %s", cfg.IndexURL, cfg.Source.IndexURL),
		fmt.Sprintf("mirror %s · %s", orNowhere(cfg.MirrorURL), cfg.Source.MirrorURL),
		fmt.Sprintf("submit %s · %s", orNowhere(cfg.SubmitURL), cfg.Source.SubmitURL),
		fmt.Sprintf("ttl %s · %s", reltime.Elapsed(cfg.TTL), cfg.Source.TTL),
	} {
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
	}
	held := cached
	// For status the probe runs BEFORE the index line, because the probe is
	// what fetches and caches: the line below must describe the document this
	// run now holds, not the one it held before the fetch. show asks nothing,
	// so its line is the cache's own. prior is the cache as it stood before
	// the fetch, read to name what a stored document replaced.
	prior := cached
	var relay, mirror probeSummary
	if withStatus {
		relay, mirror = probePool(poolDir, cfg, now, keys)
		if relay.stored || mirror.stored {
			cached = readCachedIndex(poolDir)
			held = cached
		}
	}
	var indexLine string
	if held == nil {
		// A nothing is said in a sentence, the way an empty cache is:
		// silence and a bare header both read as a command that broke. And the
		// index the build carries is named beside it, so a person knows there
		// are numbers before any fetch: the seed is what a pick reads until a
		// fresher signed one is cached.
		indexLine = "no index cached yet"
		if seed, err := index.SeedIndex(); err == nil {
			indexLine = fmt.Sprintf("no index cached yet · built-in seed of %s, %s", seed.Generated().Format("2006-01-02"), countWord(indexCellCount(seed), "cell", "cells"))
			held = seed
		}
	} else {
		generated := held.Generated()
		indexLine = fmt.Sprintf("index · generated %s · %s old · schema %d · %s · %s · %s · min installs %d",
			generated.Format("2006-01-02"), reltime.Elapsed(now.Sub(generated)),
			held.Schema(), countWord(len(held.Metrics()), "metric", "metrics"),
			countWord(len(held.Judges()), "judge", "judges"), countWord(indexCellCount(held), "cell", "cells"), held.MinInstalls())
		// The tail says what THIS run's probe did to the cache: a document it
		// stored is named as cached now, beside what stood there before — the
		// built-in seed, or the day of the document it replaced. A probe that
		// stored nothing leaves the line as it has always been.
		if withStatus && (relay.stored || mirror.stored) {
			was := "built-in seed"
			if prior != nil {
				was = prior.Generated().Format("2006-01-02")
			}
			indexLine += fmt.Sprintf(" · cached now (was %s)", was)
		}
	}
	if _, err := fmt.Fprintln(output, indexLine); err != nil {
		return err
	}
	// One line per declared metric, after the index line: the count above
	// says how many, these say which — and which of them are judged scores
	// and which are graded shares.
	for _, line := range metricLines(held) {
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
	}
	// Under --cells the held document is said one layer deeper, one line per
	// cell under a table header that follows the metric lines above: the
	// count says how many, the metric lines which, these what each cell is.
	if withCells {
		for _, line := range cellTableLines(held) {
			if _, err := fmt.Fprintln(output, line); err != nil {
				return err
			}
		}
	}
	// The install's own sheet is said the way every other nothing here is
	// said: a sheet that holds no cell yet is `none`, and one that holds some
	// is counted with its noun.
	own := ownSheetSummary(poolDir)
	if own.Cells == 0 {
		if _, err := fmt.Fprintln(output, "own sheet: none"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(output, "own sheet: %s, %s\n",
		countWord(own.Cells, "cell", "cells"), countWord(own.Observations, "observation", "observations")); err != nil {
		return err
	}
	// The last judge's record is read the way every other nothing here is
	// read: a missing or malformed file is no judge yet, said in a sentence.
	last := readJudgeLast(poolDir)
	if withStatus {
		if _, err := fmt.Fprintln(output, judgeLastLine(last)); err != nil {
			return err
		}
		// What is waiting for a judge and what the last sweep did are said
		// beside it: the three lines together answer whether a run — a chat
		// landing or one a headless door left — is being scored at all.
		if _, err := fmt.Fprintln(output, pendingJudgeLine(readPendingJudge(poolDir), now)); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(output, sweepLastLine(readSweepLast(poolDir), judgedTotal(poolDir), now)); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(output, relayStatusLine(cfg, relay, mirror, cached)); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output, pendingRowsLine(poolDir, cfg))
		return err
	}
	return nil
}

// pendingJudgeSummary is the pending file as status carries it: the rows no
// judged marker retires yet, and — when any wait — the oldest one's door and
// moment. OldestAt is nil when the oldest row was written before rows carried
// a moment, which the line says as an unknown age rather than inventing one.
type pendingJudgeSummary struct {
	Count      int        `json:"count"`
	OldestDoor string     `json:"oldest_door,omitempty"`
	OldestAt   *time.Time `json:"oldest_at,omitempty"`
}

// readPendingJudge counts what the pending file is holding for the restart
// sweep: the rows a judged marker does not retire. A file that is missing, or
// a line that is torn, reads as the rows it does hold — the reading form
// reports what a person has. The oldest row is the earliest moment among
// them, and a row with no moment is the oldest there can be: it predates the
// stamp.
func readPendingJudge(poolDir string) pendingJudgeSummary {
	var summary pendingJudgeSummary
	data, err := os.ReadFile(pendingPath(poolDir))
	if err != nil {
		return summary
	}
	var oldestAt time.Time
	var oldestDoor string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row pendingLanding
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		if alreadyJudged(poolDir, row.Landing.ID, row.Landing.Attempt) {
			continue
		}
		summary.Count++
		door := row.Door
		if door == "" {
			door = "task"
		}
		if summary.Count == 1 || row.At.Before(oldestAt) {
			oldestAt, oldestDoor = row.At, door
		}
	}
	if summary.Count > 0 {
		summary.OldestDoor = oldestDoor
		if !oldestAt.IsZero() {
			summary.OldestAt = &oldestAt
		}
	}
	return summary
}

// pendingJudgeLine is the one line status says about the pending file: how
// many runs wait for a judge and the oldest one's door and age. A row written
// before rows carried a moment says an unknown age — it predates the stamp —
// and a file with nothing waiting is said in a sentence, the way every other
// nothing here is said.
func pendingJudgeLine(summary pendingJudgeSummary, now time.Time) string {
	if summary.Count == 0 {
		return "pending judge: none"
	}
	age := "age unknown"
	if summary.OldestAt != nil {
		age = reltime.Short(*summary.OldestAt, now)
	}
	return fmt.Sprintf("pending judge: %d · oldest %s run %s", summary.Count, summary.OldestDoor, age)
}

// poolProbeBudget is what status spends asking one address. It is short on
// purpose: the line is a reading a person waits for, and an address that takes
// longer than this to answer has not answered.
const poolProbeBudget = 3 * time.Second

// readCachedIndex reads the document under the profile's pool directory the
// way every reading form reads it: a file that does not parse is not there
// yet. The signature and the puller's version mark are verify's business; a
// reading reports what a person has.
func readCachedIndex(poolDir string) *index.Index {
	doc, err := os.ReadFile(filepath.Join(poolDir, "doc.json"))
	if err != nil {
		return nil
	}
	parsed, err := index.Parse(doc)
	if err != nil {
		return nil
	}
	return parsed
}

// probeSummary is one address's answer under status: whether it answered, the
// index version it served when it did, and — when it did not — the one-line
// reason. fromCache says the document was the copy already on disk, which the
// relay line words differently; it is not part of the JSON shape. stored says
// this pull wrote a document the cache did not hold before, which is what the
// index line's tail reports.
type probeSummary struct {
	Reachable bool   `json:"reachable"`
	Version   int64  `json:"version"`
	Reason    string `json:"reason"`
	fromCache bool
	stored    bool
}

// probePool asks the relay for the index, and the mirror when the relay does
// not answer, under a pull that always goes to the network — TTL 0 — inside a
// three-second budget. It never fails a reading form: an address that does not
// answer is what the line is for, and status exits 0 either way. A mode that
// forbids reading asks nothing at all.
func probePool(poolDir string, cfg poolcfg.Config, now time.Time, keys []ed25519.PublicKey) (probeSummary, probeSummary) {
	if !cfg.CanRead() {
		off := probeSummary{Reason: "not read (model_pool off)"}
		return off, off
	}
	// --key REPLACES the resolved keys: what the person typed is the whole
	// list a probe is checked under, so a key nobody signed with can prove a
	// document does not verify under it. Nothing typed: the stored word when
	// it decodes, else the key the build carries — and a word that does not
	// decode is the reason itself, with no address asked: a signature nobody
	// can check is a fetch nobody should make.
	trusted := keys
	if len(trusted) == 0 {
		resolved, keyErr := poolTrustedKeysErr(cfg)
		if keyErr != nil {
			broken := probeSummary{Reason: poolKeyReason(cfg)}
			return broken, broken
		}
		trusted = resolved
	}
	clock := func() time.Time { return now }
	relay := probeAddress(cfg.IndexURL, poolDir, trusted, clock)
	if relay.Reachable {
		return relay, probeSummary{}
	}
	if cfg.MirrorURL == "" {
		return relay, probeSummary{Reason: "not configured"}
	}
	return relay, probeAddress(cfg.MirrorURL, poolDir, trusted, clock)
}

// probeAddress pulls one address the way status asks it: a fresh fetch — TTL 0
// so a young cache is not what answers — inside the probe budget, its failure
// turned into the one-line reason.
func probeAddress(url, poolDir string, keys []ed25519.PublicKey, now func() time.Time) probeSummary {
	puller := &pull.Puller{
		URL:      url,
		Keys:     keys,
		CacheDir: poolDir,
		TTL:      0,
		Budget:   poolProbeBudget,
		Now:      now,
	}
	result, err := puller.Pull(context.Background())
	if err != nil {
		return probeSummary{Reason: oneLine(err.Error())}
	}
	return probeSummary{Reachable: true, Version: result.Version, fromCache: result.FromCache, stored: result.Changed}
}

// relayStatusLine is the one line status says about the addresses: whether the
// relay answered, the mirror beside it when the relay did not, and — when
// neither did — what the reading fell back to. A mode that forbids reading
// asks nothing and says that instead.
func relayStatusLine(cfg poolcfg.Config, relay, mirror probeSummary, cached *index.Index) string {
	if !cfg.CanRead() {
		return "relay: not read (model_pool off)"
	}
	if relay.Reachable {
		if relay.fromCache {
			return fmt.Sprintf("relay: reachable · index unchanged, version %d", relay.Version)
		}
		return fmt.Sprintf("relay: reachable · index version %d", relay.Version)
	}
	if mirror.Reachable {
		return fmt.Sprintf("relay: unreachable (%s) · mirror: reachable · index version %d", relay.Reason, mirror.Version)
	}
	where := "built-in seed"
	if cached != nil {
		where = "cache"
	}
	return fmt.Sprintf("relay: unreachable (%s) · mirror: unreachable (%s) · reading %s", relay.Reason, mirror.Reason, where)
}

// oneLine is a reason said on one line: any newline becomes a space, because
// the relay line holds the reason in parentheses beside the next word.
func oneLine(reason string) string {
	return strings.ReplaceAll(strings.TrimSpace(reason), "\n", " ")
}

// ownSummary is the install's own sheet as the reading forms carry it: the
// cells the picker reads beside the index, and the observations behind them.
type ownSummary struct {
	Cells        int `json:"cells"`
	Observations int `json:"observations"`
}

// ownSheetSummary counts what the install's own sheet holds for the picker:
// the role_quality cells recorded with no dim labels and the observations
// behind them. A sheet that is missing, or one that does not parse, reads as
// none — the reading form reports what a person has.
func ownSheetSummary(poolDir string) ownSummary {
	sheet, err := record.LoadSheet(record.OwnSheetPath(poolDir))
	if err != nil {
		return ownSummary{}
	}
	var summary ownSummary
	for _, cell := range record.Cells(sheet) {
		summary.Cells++
		summary.Observations += cell.N
	}
	return summary
}

// poolAnswer is the --json shape of the reading forms: the config flat, the
// cached index under index or null. The status fields are pointers so a show
// carries none of them — a field a form does not answer reads as not-asked
// rather than as zero.
type poolAnswer struct {
	Mode         string               `json:"mode"`
	ModeSource   string               `json:"mode_source"`
	RelayURL     string               `json:"relay_url"`
	IndexURL     string               `json:"index_url"`
	MirrorURL    string               `json:"mirror_url"`
	SubmitURL    string               `json:"submit_url"`
	TTLSeconds   int                  `json:"ttl_seconds"`
	Pending      *int                 `json:"pending,omitempty"`
	Dropped      *int                 `json:"dropped,omitempty"`
	DroppedLast  *string              `json:"dropped_last,omitempty"`
	PendingJudge *pendingJudgeSummary `json:"pending_judge,omitempty"`
	CanSend      *bool                `json:"can_send,omitempty"`
	CanRead      *bool                `json:"can_read,omitempty"`
	// Identity is status's own: whether the install has minted the nonce it
	// sends under, a pointer like every other status field so a show carries
	// none of it.
	Identity  *bool         `json:"identity,omitempty"`
	Relay     *probeSummary `json:"relay,omitempty"`
	Mirror    *probeSummary `json:"mirror,omitempty"`
	LastJudge *judgeLast    `json:"last_judge"`
	LastSweep *sweepLast    `json:"last_sweep"`
	// JudgedTotal is every landing this install has had judged, counted from
	// the markers; nil off status, so a show's shape is the one it always had.
	JudgedTotal *int          `json:"judged_total,omitempty"`
	Index       *indexSummary `json:"index"`
	Cells       []cellSummary `json:"cells,omitempty"`
	Own         ownSummary    `json:"own"`
}

// indexSummary is the cached index as the reading forms carry it: the
// document's own day, its age against the asking clock, and the counts a
// person checks before trusting it. Source says where the document came from —
// "cache" for the one under the profile, "seed" for the one the build carries
// when there is no cache.
type indexSummary struct {
	Generated  string `json:"generated"`
	AgeSeconds int    `json:"age_seconds"`
	Schema     int    `json:"schema"`
	// Metrics stays the count readers of today's shape already read;
	// metric_list is the per-metric detail beside it, not instead of it.
	Metrics     int             `json:"metrics"`
	MetricList  []metricSummary `json:"metric_list"`
	Judges      int             `json:"judges"`
	Cells       int             `json:"cells"`
	MinInstalls int             `json:"min_installs"`
	Source      string          `json:"source"`
	// CachedNow is true on the one status whose probe stored this document,
	// and left out otherwise so a reader sees it only when it happened.
	CachedNow bool `json:"cached_now,omitempty"`
}

// metricSummary is one declared metric as the reading forms carry it: the
// words the document spells for it, its cells counted, and — when its cells
// are split by the source that produced each measurement — the distinct
// sources beside them. A metric whose cells are not split by one carries no
// sources.
type metricSummary struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Unit    string   `json:"unit"`
	Dims    []string `json:"dims"`
	Cells   int      `json:"cells"`
	Sources []string `json:"sources,omitempty"`
}

// cellSummary is one cell of the held document as --cells carries it in
// --json: the metric it belongs to, the role and canonical model it is
// addressed by, the dims it spells beyond them, the measurement, its rows,
// and the installs behind it — zero where the cell spells none.
type cellSummary struct {
	Metric   string            `json:"metric"`
	Role     string            `json:"role"`
	Model    string            `json:"model"`
	Dims     map[string]string `json:"dims,omitempty"`
	Mean     float64           `json:"mean"`
	SD       float64           `json:"sd"`
	N        int               `json:"n"`
	Installs int               `json:"installs,omitempty"`
}

func printPoolJSON(output io.Writer, poolDir string, cfg poolcfg.Config, cached *index.Index, now time.Time, withCells, withStatus bool, keys []ed25519.PublicKey) error {
	answer := poolAnswer{
		Mode:       cfg.Mode.String(),
		ModeSource: cfg.Source.Mode,
		RelayURL:   cfg.RelayURL,
		IndexURL:   cfg.IndexURL,
		MirrorURL:  cfg.MirrorURL,
		SubmitURL:  cfg.SubmitURL,
		TTLSeconds: int(cfg.TTL / time.Second),
	}
	// The probe runs BEFORE the index is summarized, for the same reason
	// status's line waits for it: the summary is about the document this run
	// holds after the fetch, and cached_now says this run stored it.
	relay, mirror := probeSummary{}, probeSummary{}
	cachedNow := false
	if withStatus {
		relay, mirror = probePool(poolDir, cfg, now, keys)
		if relay.stored || mirror.stored {
			cached = readCachedIndex(poolDir)
			cachedNow = true
		}
	}
	// A cached document is reported as itself; with no cache the build's seed
	// stands in, so a script reading `index` sees the index a pick would read
	// and the source field says which one it was.
	held, source := cached, "cache"
	if held == nil {
		if seed, err := index.SeedIndex(); err == nil {
			held, source = seed, "seed"
		}
	}
	if held != nil {
		generated := held.Generated()
		answer.Index = &indexSummary{
			Generated:   generated.Format("2006-01-02"),
			AgeSeconds:  int(now.Sub(generated) / time.Second),
			Schema:      held.Schema(),
			Metrics:     len(held.Metrics()),
			MetricList:  indexMetricSummaries(held),
			Judges:      len(held.Judges()),
			Cells:       indexCellCount(held),
			MinInstalls: held.MinInstalls(),
			Source:      source,
			CachedNow:   cachedNow,
		}
	}
	// The cells ride beside the summary only when they were asked for:
	// without --cells the object is the object it has always been.
	if withCells && held != nil {
		answer.Cells = indexCellSummaries(held)
	}
	if withStatus {
		pending := pendingRows(poolDir)
		send, read := cfg.CanSend(), cfg.CanRead()
		answer.Pending = &pending
		answer.CanSend = &send
		answer.CanRead = &read
		identity := installIdentitySet(poolDir)
		answer.Identity = &identity
		dropped, last := droppedRows(poolDir)
		answer.Dropped = &dropped
		if last != "" {
			answer.DroppedLast = &last
		}
		judged := readPendingJudge(poolDir)
		answer.PendingJudge = &judged
		total := judgedTotal(poolDir)
		answer.JudgedTotal = &total
		answer.Relay = &relay
		answer.Mirror = &mirror
	}
	answer.Own = ownSheetSummary(poolDir)
	// The records are null when there is none, so a script can tell a judge
	// that has not run from one that failed.
	if last := readJudgeLast(poolDir); last != nil {
		answer.LastJudge = last
	}
	if swept := readSweepLast(poolDir); swept != nil {
		answer.LastSweep = swept
	}
	encoded, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "%s\n", encoded)
	return err
}

// verifyPool is the one form that reaches the network: fetch the index now —
// TTL 0, the one door that means now — and check its signature. It refuses at
// the door when the mode is off and when no key is in hand; a failed fetch
// says its error and leaves on exit 1 through the one door every failure
// leaves by.
func verifyPool(args []string, output io.Writer, poolDir string, cfg poolcfg.Config, now func() time.Time) error {
	flags := commandFlags("pool verify")
	asJSON := flags.Bool("json", false, "print the result as one JSON object")
	var keys poolKeys
	flags.Var(&keys, "key", "an ed25519 public key, base64; repeatable")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: codeaf pool verify [--json] [--key key]")
	}
	// OFF IS A REFUSAL AT THE DOOR, and so is a build with no key: neither
	// fetches, and the sentence is the answer, on stdout where the answer
	// goes (streams.go).
	//
	// THE RUNG IS EXIT 2 and not exit 1: nothing was attempted, but the
	// question asked of this build does not stand, and the remedy is on the
	// line above the exit. verify's third refusal — a fetch or a signature
	// that fails — is exit 1, in the error main() says in front of it.
	//
	// OFF IS A REFUSAL AT THE DOOR, and so is a key nobody can vouch for:
	// neither fetches, and the sentence is the answer, on stdout where the
	// answer goes (streams.go).
	//
	// THE RUNG IS EXIT 2 and not exit 1: nothing was attempted, but the
	// question asked of this build does not stand, and the remedy is on the
	// line above the exit. verify's third refusal — a fetch or a signature
	// that fails — is exit 1, in the error main() says in front of it.
	if cfg.Mode == poolcfg.Off {
		if _, err := fmt.Fprintln(output, "the Model Pool is off in settings"); err != nil {
			return err
		}
		return exitIncomplete
	}
	// --key REPLACES the resolved key: keys the person typed are the whole
	// list, so a key nobody signed with can prove a document does not verify
	// under it. Nothing typed: the stored word when it decodes, else the key
	// the build carries — and a word that does not decode is refused by the
	// name of where it was set, before anything is fetched. Only a build
	// that shipped no key at all, with nothing stored, reaches the line
	// after it.
	trusted := []ed25519.PublicKey(keys)
	if len(trusted) == 0 {
		resolved, keyErr := poolTrustedKeysErr(cfg)
		if keyErr != nil {
			if _, err := fmt.Fprintln(output, poolKeyReason(cfg)); err != nil {
				return err
			}
			return exitIncomplete
		}
		trusted = resolved
	}
	if len(trusted) == 0 {
		if _, err := fmt.Fprintln(output, "no public key built into this build; pass --key"); err != nil {
			return err
		}
		return exitIncomplete
	}
	puller := &pull.Puller{
		URL:      cfg.IndexURL,
		Keys:     trusted,
		CacheDir: poolDir,
		TTL:      0,
		Budget:   pull.DefaultBudget,
		Now:      now,
	}
	result, err := puller.Pull(context.Background())
	if err != nil {
		return err
	}
	held, err := index.Parse(result.Doc)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	generated := held.Generated().Format("2006-01-02")
	if *asJSON {
		encoded, err := json.Marshal(struct {
			Verified   bool            `json:"verified"`
			Version    int64           `json:"version"`
			Generated  string          `json:"generated"`
			Metrics    int             `json:"metrics"`
			MetricList []metricSummary `json:"metric_list"`
		}{true, result.Version, generated, len(held.Metrics()), indexMetricSummaries(held)})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "%s\n", encoded)
		return err
	}
	// The names where the count was: a count said how many, the names say
	// which. A document that declares none still says so, with the count's
	// own word for a nothing.
	said := "none"
	if names := held.Metrics(); len(names) > 0 {
		said = strings.Join(names, ", ")
	}
	_, err = fmt.Fprintf(output, "signature good: version %d, generated %s, metrics %s\n",
		result.Version, generated, said)
	return err
}

// readJudgeLast reads what the hook left about the last landing it judged:
// the judge that answered and the seats it scored, or the candidates tried
// and why none of them did. A file that is missing, or one that does not
// parse, reads as none yet — the reading form reports what a person has.
func readJudgeLast(poolDir string) *judgeLast {
	data, err := os.ReadFile(filepath.Join(poolDir, "judge-last.json"))
	if err != nil {
		return nil
	}
	var last judgeLast
	if json.Unmarshal(data, &last) != nil {
		return nil
	}
	return &last
}

// readSweepLast reads what the sweep left about its own run: what it judged,
// what its budget left waiting, and whether the deadline ended it. A file
// that is missing, or one that does not parse, reads as none yet — the
// reading form reports what a person has.
func readSweepLast(poolDir string) *sweepLast {
	data, err := os.ReadFile(filepath.Join(poolDir, "sweep-last.json"))
	if err != nil {
		return nil
	}
	var last sweepLast
	if json.Unmarshal(data, &last) != nil {
		return nil
	}
	return &last
}

// judgeLastLine is the one line status says about the last judge: which model
// answered and which seats it scored, or — when none did — how many were
// asked, the first of them, and the one-line reason the last one failed. The
// moment is the record's own, said in local hours and minutes. A record only a
// landed task ever writes, so no record at all is said for what it means —
// nothing has landed to be judged — rather than as a judge that never ran.
func judgeLastLine(last *judgeLast) string {
	if last == nil {
		return "last judge: none yet (no landing judged)"
	}
	at := last.At.Format("15:04")
	if last.Judge != "" {
		return fmt.Sprintf("last judge: %s · %s · scored %s", at, last.Judge, strings.Join(last.Scored, ", "))
	}
	asked := ""
	if len(last.Tried) > 0 {
		asked = last.Tried[0]
		if len(last.Tried) > 1 {
			asked += ", …"
		}
	}
	return fmt.Sprintf("last judge: %s · failed after %d candidates (%s) · %s", at, len(last.Tried), asked, last.Reason)
}

// sweepLastLine is the one line status says about the last sweep: what it
// judged, what it left waiting when the deadline cut it, and how much of the
// sweep's own budget it spent. Beside them stands the total every landing this
// install has ever had judged, folded on when there is a marker to count —
// what THIS sweep judged is the record's own, and the two together say whether
// judging is happening at all. The moment is the record's own, said the way
// the surface says every when — relatively. A record with no moment is no
// record worth reporting, the same reading a missing file takes.
func sweepLastLine(last *sweepLast, total int, now time.Time) string {
	line := "last sweep: none yet"
	if last != nil && !last.At.IsZero() {
		parts := []string{
			reltime.Short(last.At, now) + " ago",
			fmt.Sprintf("judged %d", last.Judged),
		}
		if last.Left > 0 {
			parts = append(parts, fmt.Sprintf("%d still pending", last.Left))
		}
		parts = append(parts, fmt.Sprintf("%s of %s",
			reltime.Elapsed(time.Duration(last.BudgetUsed)*time.Second), reltime.Elapsed(poolSweepBudget)))
		line = "last sweep: " + strings.Join(parts, " · ")
	}
	if total > 0 {
		line += fmt.Sprintf(" · judged %d in all", total)
	}
	return line
}

// pendingRows counts what the outbox is holding. It reads the file by count
// and not by opening it, because [outbox.Open] CREATES the file when it is
// not there and a reading form must not write.
func pendingRows(poolDir string) int {
	path := filepath.Join(poolDir, "outbox.jsonl")
	if _, err := os.Stat(path); err != nil {
		return 0
	}
	box, err := outbox.Open(path)
	if err != nil {
		return 0
	}
	defer box.Close()
	return len(box.Pending())
}

// droppedRows reads the outbox's dropped markers: how many rows nothing will
// send again, and the reason on the most recent of them — empty for a row a
// build that kept no reason dropped. It reads the file by path like
// [pendingRows], because [outbox.Open] creates an absent outbox and a reading
// form must not write.
func droppedRows(poolDir string) (int, string) {
	path := filepath.Join(poolDir, "outbox.jsonl")
	if _, err := os.Stat(path); err != nil {
		return 0, ""
	}
	box, err := outbox.Open(path)
	if err != nil {
		return 0, ""
	}
	defer box.Close()
	drops := box.Dropped()
	if len(drops) == 0 {
		return 0, ""
	}
	return len(drops), drops[len(drops)-1].Reason
}

// pendingRowsLine is the one line status says about the outbox: how many rows
// wait to be sent, how many the relay or the cap dropped and the most recent
// reason, the two doors the mode opens, and — when it has been minted — the
// install's own identity. The dropped segment is left out entirely when
// nothing was dropped, and the identity segment when no nonce has been drawn,
// so a working install reads the way it always did.
func pendingRowsLine(poolDir string, cfg poolcfg.Config) string {
	line := fmt.Sprintf("pending %d", pendingRows(poolDir))
	if dropped, last := droppedRows(poolDir); dropped > 0 {
		line += fmt.Sprintf(" · dropped %d", dropped)
		if last != "" {
			line += fmt.Sprintf(" (last: %s)", oneLine(last))
		}
	}
	line += fmt.Sprintf(" · can send %s · can read %s", yesNo(cfg.CanSend()), yesNo(cfg.CanRead()))
	if installIdentitySet(poolDir) {
		line += " · identity set"
	}
	return line
}

// installIdentitySet says whether this install has minted its nonce: a reading
// form may not call [installNonce], which DRAWS one on the first read, so
// status asks the file's presence alone — [installFile] is there or it is not,
// and the word inside it never leaves the file.
func installIdentitySet(poolDir string) bool {
	_, err := os.Stat(filepath.Join(poolDir, installFile))
	return err == nil
}

// judgedTotal counts the markers under [judgedDir] — every landing this
// install has ever had judged, one file per landing-and-attempt. A directory
// that is missing, or an entry that is not a file, counts nothing, the way
// every other nothing here reads.
func judgedTotal(poolDir string) int {
	entries, err := os.ReadDir(judgedDir(poolDir))
	if err != nil {
		return 0
	}
	total := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			total++
		}
	}
	return total
}

// orNowhere is an empty submit address said rather than printed empty: the
// pin holding nothing means send nowhere, and an empty column says nothing.
func orNowhere(address string) string {
	if address == "" {
		return "nowhere"
	}
	return address
}

// indexCellCount is the held document's cells summed over its metrics: what
// the picker can read, since a cell below min installs never parses in.
func indexCellCount(held *index.Index) int {
	total := 0
	for _, metric := range held.Metrics() {
		total += len(held.Cells(metric))
	}
	return total
}

// indexMetricSummaries is one summary per declared metric, in the index's
// sorted order: the words the document spells for it, its cells counted,
// and the distinct sources gathered off the cells of a metric whose cells
// are split by one. The reader keeps the spellings a cell carried, so a
// dim key and a source value are matched and said the way the index folds
// a name — lowercased, trimmed.
func indexMetricSummaries(held *index.Index) []metricSummary {
	names := held.Metrics()
	out := make([]metricSummary, 0, len(names))
	for _, name := range names {
		kind, _ := held.Kind(name)
		summary := metricSummary{
			Name:  name,
			Kind:  kind,
			Unit:  held.Unit(name),
			Dims:  append([]string{"role", "model"}, held.Dims(name)...),
			Cells: len(held.Cells(name)),
		}
		seen := map[string]bool{}
		for _, cell := range held.Cells(name) {
			source, spelled := cellDim(cell, "source")
			if !spelled {
				continue
			}
			folded := poolFold(source)
			if folded != "" && !seen[folded] {
				seen[folded] = true
				summary.Sources = append(summary.Sources, folded)
			}
		}
		sort.Strings(summary.Sources)
		out = append(out, summary)
	}
	return out
}

// indexCellSummaries is one summary per cell of the held document: metrics
// in the index's order and, within one, the cells in the index's own order —
// the same order the --cells table prints, never a sort by a number.
func indexCellSummaries(held *index.Index) []cellSummary {
	names := held.Metrics()
	out := make([]cellSummary, 0, indexCellCount(held))
	for _, name := range names {
		for _, cell := range held.Cells(name) {
			out = append(out, cellSummary{
				Metric:   name,
				Role:     cell.Role,
				Model:    cell.Model,
				Dims:     cell.Dims,
				Mean:     cell.Mean,
				SD:       cell.SD,
				N:        cell.N,
				Installs: cell.Installs,
			})
		}
	}
	return out
}

// metricLines is one line per metric the held document declares, in the
// index's sorted order. A nothing answers nothing, the way every other
// reading form reads what a person has.
func metricLines(held *index.Index) []string {
	if held == nil {
		return nil
	}
	summaries := indexMetricSummaries(held)
	lines := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		lines = append(lines, metricLine(summary))
	}
	return lines
}

// metricLine is one declared metric said on one line: `role_quality:
// gaussian score · 12 cells · dims role, model` — the kind and unit the
// document spells (an absent word is left out rather than printed empty),
// the cells counted with their noun, the dims a cell of the metric is
// addressed by, and the distinct sources beside them when the cells are
// split by one.
func metricLine(summary metricSummary) string {
	parts := make([]string, 0, 4)
	if summary.Kind != "" || summary.Unit != "" {
		words := make([]string, 0, 2)
		if summary.Kind != "" {
			words = append(words, summary.Kind)
		}
		if summary.Unit != "" {
			words = append(words, summary.Unit)
		}
		parts = append(parts, strings.Join(words, " "))
	}
	parts = append(parts, countWord(summary.Cells, "cell", "cells"),
		"dims "+strings.Join(summary.Dims, ", "))
	if len(summary.Sources) > 0 {
		parts = append(parts, "sources "+strings.Join(summary.Sources, ", "))
	}
	return summary.Name + ": " + strings.Join(parts, " · ")
}

// cellTableLines is the held document said one cell per line, under a
// `cells:` header that follows the metric lines it details: each line names
// its metric, the role and model it is addressed by, the dims it spells in
// the order the metric declares them, the measurement — a share for a
// graded metric, said by the kind the document spells — its rows, and the
// installs behind it where the document carries them. The order is the
// index's own, a property of the document, never a sort by a number; a
// metric with no cells says none on a line of its own, the way every other
// nothing here is said.
func cellTableLines(held *index.Index) []string {
	if held == nil {
		return nil
	}
	lines := []string{"cells:"}
	for _, name := range held.Metrics() {
		word := "mean"
		if kind, ok := held.Kind(name); ok && kind == "bernoulli" {
			word = "share"
		}
		cells := held.Cells(name)
		if len(cells) == 0 {
			lines = append(lines, name+" · none")
			continue
		}
		for _, cell := range cells {
			parts := []string{name, cell.Role, cell.Model}
			for _, dim := range held.Dims(name) {
				if value, spelled := cellDim(cell, dim); spelled {
					parts = append(parts, dim+" "+value)
				}
			}
			parts = append(parts, fmt.Sprintf("%s %v", word, cell.Mean))
			if cell.SD != 0 {
				parts = append(parts, fmt.Sprintf("sd %v", cell.SD))
			}
			parts = append(parts, fmt.Sprintf("n %d", cell.N))
			if cell.Installs > 0 {
				parts = append(parts, fmt.Sprintf("installs %d", cell.Installs))
			}
			lines = append(lines, strings.Join(parts, " · "))
		}
	}
	return lines
}

// poolFold is the way the index matches a name or a value — lowercased and
// trimmed — spelled here because the reader keeps the spellings a cell
// carried and the display says what matches.
func poolFold(word string) string {
	return strings.ToLower(strings.TrimSpace(word))
}

// cellDim reads one dim off a cell, under the way the index matches a dim
// key: the declared spelling wins over the case the cell happened to spell.
func cellDim(cell index.Cell, dim string) (string, bool) {
	for key, value := range cell.Dims {
		if poolFold(key) == dim {
			return value, true
		}
	}
	return "", false
}

// countWord is a count with its noun: one metric, three metrics, no judges.
// A zero is said rather than printed bare, for the reason every other
// nothing here is said.
func countWord(n int, one, many string) string {
	switch n {
	case 0:
		return "no " + many
	case 1:
		return "1 " + one
	default:
		return fmt.Sprintf("%d %s", n, many)
	}
}

func yesNo(answer bool) string {
	if answer {
		return "yes"
	}
	return "no"
}
