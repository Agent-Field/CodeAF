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
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/index"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
	"github.com/Agent-Field/codeaf/internal/pool/poolcfg"
	"github.com/Agent-Field/codeaf/internal/pool/pull"
	"github.com/Agent-Field/codeaf/internal/pool/record"
	"github.com/Agent-Field/codeaf/internal/tui2/reltime"
)

// poolPublicKeys are the ed25519 public keys a fetched index's signature is
// checked under when no key is stored beside the pool row. The list carries
// the key the index signer publishes (relay/wrangler.toml's POOL_PUBLIC_KEY),
// decoded once here from its base64 literal: a build whose literal could not
// decode would verify nothing, so the test beside the verb pins the length.
var poolPublicKeys = mustPoolKey("WOAo+g/oKxAV9vVqv2Q14w1TyyyiouwFO2fC0zgcps0=")

func mustPoolKey(encoded string) []ed25519.PublicKey {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		panic(fmt.Sprintf("pool: the built-in public key does not decode: %q", encoded))
	}
	return []ed25519.PublicKey{ed25519.PublicKey(raw)}
}

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
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: codeaf pool show [--json]")
	}
	return printPool(output, poolDir, cfg, now(), *asJSON, false, nil)
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
	return printPool(output, poolDir, cfg, now(), *asJSON, true, keys)
}

// printPool is the reading form's whole answer. The config first — every value
// beside the word saying where it came from, one of default, setting, env or
// ci — then the cached index with its age, then the install's own sheet,
// then, for status, the outbox and the two doors the mode opens.
func printPool(output io.Writer, poolDir string, cfg poolcfg.Config, now time.Time, asJSON, withStatus bool, keys []ed25519.PublicKey) error {
	var cached *index.Index
	// The cache is read the way show reads everything else, as an answer and
	// not as an argument: a document that does not parse is not there yet,
	// and the line below says so. The signature and the puller's version mark
	// are verify's business; show reports what a person has.
	if doc, err := os.ReadFile(filepath.Join(poolDir, "doc.json")); err == nil {
		if parsed, err := index.Parse(doc); err == nil {
			cached = parsed
		}
	}
	if asJSON {
		return printPoolJSON(output, poolDir, cfg, cached, now, withStatus, keys)
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
	if cached == nil {
		// A nothing is said in a sentence, the way an empty cache is:
		// silence and a bare header both read as a command that broke. And the
		// index the build carries is named beside it, so a person knows there
		// are numbers before any fetch: the seed is what a pick reads until a
		// fresher signed one is cached.
		line := "no index cached yet"
		if seed, err := index.SeedIndex(); err == nil {
			line = fmt.Sprintf("no index cached yet · built-in seed of %s", seed.Generated().Format("2006-01-02"))
		}
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
	} else {
		generated := cached.Generated()
		if _, err := fmt.Fprintf(output,
			"index · generated %s · %s old · schema %d · %s · %s · min installs %d\n",
			generated.Format("2006-01-02"), reltime.Elapsed(now.Sub(generated)),
			cached.Schema(), countWord(len(cached.Metrics()), "metric", "metrics"),
			countWord(len(cached.Judges()), "judge", "judges"), cached.MinInstalls()); err != nil {
			return err
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
		relay, mirror := probePool(poolDir, cfg, now, keys)
		if _, err := fmt.Fprintln(output, relayStatusLine(cfg, relay, mirror, cached)); err != nil {
			return err
		}
		_, err := fmt.Fprintf(output, "pending %d · can send %s · can read %s\n",
			pendingRows(poolDir), yesNo(cfg.CanSend()), yesNo(cfg.CanRead()))
		return err
	}
	return nil
}

// poolProbeBudget is what status spends asking one address. It is short on
// purpose: the line is a reading a person waits for, and an address that takes
// longer than this to answer has not answered.
const poolProbeBudget = 3 * time.Second

// probeSummary is one address's answer under status: whether it answered, the
// index version it served when it did, and — when it did not — the one-line
// reason. fromCache says the document was the copy already on disk, which the
// relay line words differently; it is not part of the JSON shape.
type probeSummary struct {
	Reachable bool   `json:"reachable"`
	Version   int64  `json:"version"`
	Reason    string `json:"reason"`
	fromCache bool
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
	return probeSummary{Reachable: true, Version: result.Version, fromCache: result.FromCache}
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
// cached index under index or null. The three status fields are pointers so a
// show carries none of them — a field a form does not answer reads as
// not-asked rather than as zero.
type poolAnswer struct {
	Mode       string        `json:"mode"`
	ModeSource string        `json:"mode_source"`
	RelayURL   string        `json:"relay_url"`
	IndexURL   string        `json:"index_url"`
	MirrorURL  string        `json:"mirror_url"`
	SubmitURL  string        `json:"submit_url"`
	TTLSeconds int           `json:"ttl_seconds"`
	Pending    *int          `json:"pending,omitempty"`
	CanSend    *bool         `json:"can_send,omitempty"`
	CanRead    *bool         `json:"can_read,omitempty"`
	Relay      *probeSummary `json:"relay,omitempty"`
	Mirror     *probeSummary `json:"mirror,omitempty"`
	LastJudge  *judgeLast    `json:"last_judge"`
	Index      *indexSummary `json:"index"`
	Own        ownSummary    `json:"own"`
}

// indexSummary is the cached index as the reading forms carry it: the
// document's own day, its age against the asking clock, and the counts a
// person checks before trusting it. Source says where the document came from —
// "cache" for the one under the profile, "seed" for the one the build carries
// when there is no cache.
type indexSummary struct {
	Generated   string `json:"generated"`
	AgeSeconds  int    `json:"age_seconds"`
	Schema      int    `json:"schema"`
	Metrics     int    `json:"metrics"`
	Judges      int    `json:"judges"`
	MinInstalls int    `json:"min_installs"`
	Source      string `json:"source"`
}

func printPoolJSON(output io.Writer, poolDir string, cfg poolcfg.Config, cached *index.Index, now time.Time, withStatus bool, keys []ed25519.PublicKey) error {
	answer := poolAnswer{
		Mode:       cfg.Mode.String(),
		ModeSource: cfg.Source.Mode,
		RelayURL:   cfg.RelayURL,
		IndexURL:   cfg.IndexURL,
		MirrorURL:  cfg.MirrorURL,
		SubmitURL:  cfg.SubmitURL,
		TTLSeconds: int(cfg.TTL / time.Second),
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
			Judges:      len(held.Judges()),
			MinInstalls: held.MinInstalls(),
			Source:      source,
		}
	}
	if withStatus {
		pending := pendingRows(poolDir)
		send, read := cfg.CanSend(), cfg.CanRead()
		answer.Pending = &pending
		answer.CanSend = &send
		answer.CanRead = &read
		relay, mirror := probePool(poolDir, cfg, now, keys)
		answer.Relay = &relay
		answer.Mirror = &mirror
	}
	answer.Own = ownSheetSummary(poolDir)
	// The record is null when there is none, so a script can tell a judge
	// that has not run from one that failed.
	if last := readJudgeLast(poolDir); last != nil {
		answer.LastJudge = last
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
			Verified  bool   `json:"verified"`
			Version   int64  `json:"version"`
			Generated string `json:"generated"`
			Metrics   int    `json:"metrics"`
		}{true, result.Version, generated, len(held.Metrics())})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "%s\n", encoded)
		return err
	}
	_, err = fmt.Fprintf(output, "signature good: version %d, generated %s, %d metrics\n",
		result.Version, generated, len(held.Metrics()))
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

// judgeLastLine is the one line status says about the last judge: which model
// answered and which seats it scored, or — when none did — how many were
// asked, the first of them, and the one-line reason the last one failed. The
// moment is the record's own, said in local hours and minutes.
func judgeLastLine(last *judgeLast) string {
	if last == nil {
		return "last judge: none yet"
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

// orNowhere is an empty submit address said rather than printed empty: the
// pin holding nothing means send nowhere, and an empty column says nothing.
func orNowhere(address string) string {
	if address == "" {
		return "nowhere"
	}
	return address
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
