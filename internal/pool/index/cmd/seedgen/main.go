// Command seedgen regenerates the seed index the binary carries from the live
// relay.
//
// THE SEED IS A COPY, NOT A COMPOSITION. internal/pool/index/seed.json is the
// fallback a machine with no cache picks from, and it must be the pool the
// relay publishes rather than a hand-authored snapshot that lags it. This
// command fetches the signed index through internal/pool/pull — the same
// signature and size checks a running install makes — reads it through
// internal/pool/index.Parse, and writes the seed back out deterministically:
// fixed key order, cells sorted by metric then by the metric's own dims in the
// order they were declared.
//
// IT COPIES BY DECLARATION. Every metric the document declares is copied with
// its declaration, and every cell with its metric's dims, VERBATIM — so a
// metric or a dim this build has never seen passes through unchanged as long as
// Parse accepts the document, and no metric name or cell address is hard-coded
// in the copier. The only thing merged rather than copied is the aliases: the
// embedded seed's entries sit UNDER the live document's, so an alias the relay
// has since dropped from its own map still survives in the seed.
//
// Usage:
//
//	go run ./internal/pool/index/cmd/seedgen [-url url] [-mirror url]
//	    [-key base64 ...] [-out path] [-cells path] [-check]
//
// Run it from the repository root: the default -out and -cells are the
// repository's own paths. -check diffs the files against what the relay would
// write and writes nothing, leaving on exit 1 when they drift.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/pool/index"
	"github.com/Agent-Field/codeaf/internal/pool/poolcfg"
	"github.com/Agent-Field/codeaf/internal/pool/poolkey"
	"github.com/Agent-Field/codeaf/internal/pool/pull"
)

// The defaults. The seed and its figure are the repository's own files, so the
// command regenerates what the release carries when run from the root; the two
// addresses are the ones the binary reads by default, the mirror being a second
// address for the same signed document.
const (
	defaultOut   = "internal/pool/index/seed.json"
	defaultCells = "docs/design/model-pool/data/seed-cells.csv"
)

// maxSeed bounds the document this command will write. It sits well under the
// 4 MiB a fetched document is allowed: a seed is a fallback carried in the
// binary, and a fallback that grew into a document is a payload every launch
// pays for.
const maxSeed = 1 << 20

// budget bounds one fetch of the live document.
const budget = 30 * time.Second

// errDrift is what -check leaves by when a file is not what the relay would
// write. It is its own error so the exit is one, not a fetch or a decode that
// failed.
var errDrift = errors.New("seedgen: the file is not what the relay would write")

// roleQualityMetric names the metric the figure's CSV is built from. IT IS
// NAMED HERE AND NOT IN THE COPIER: the paper's table is role_quality's, so
// this one figure knows the metric it draws, while the seed's copier knows no
// metric at all.
const roleQualityMetric = "role_quality"

// keyList collects --key's value: a base64 ed25519 public key, repeatable, each
// decoded the moment it is typed. A key that does not decode is refused where
// it was typed rather than carried as text.
type keyList []ed25519.PublicKey

func (k *keyList) String() string {
	if len(*k) == 0 {
		return ""
	}
	return fmt.Sprintf("%d key(s)", len(*k))
}

func (k *keyList) Set(word string) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(word))
	if err != nil {
		return fmt.Errorf("-key: %q is not base64", word)
	}
	if len(raw) != ed25519.PublicKeySize {
		return fmt.Errorf("-key: %d bytes, want an ed25519 public key's %d", len(raw), ed25519.PublicKeySize)
	}
	*k = append(*k, ed25519.PublicKey(raw))
	return nil
}

func main() {
	var (
		url    = flag.String("url", poolcfg.DefaultIndexURL, "the relay's index address")
		mirror = flag.String("mirror", poolcfg.DefaultMirrorURL, "a second address for the same signed document")
		out    = flag.String("out", defaultOut, "the seed file to write")
		cells  = flag.String("cells", defaultCells, "the role_quality cells as CSV, for the paper's figure")
		check  = flag.Bool("check", false, "diff against the files and write nothing; exit 1 on drift")
	)
	var keys keyList
	flag.Var(&keys, "key", "an ed25519 public key, base64; repeatable (default: the build's own key)")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: seedgen [-url url] [-mirror url] [-key base64 ...] [-out path] [-cells path] [-check]")
		os.Exit(2)
	}
	trusted := []ed25519.PublicKey(keys)
	if len(trusted) == 0 {
		trusted = poolkey.Keys()
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	if err := run(ctx, *url, *mirror, *out, *cells, *check, trusted); err != nil {
		fmt.Fprintln(os.Stderr, "seedgen:", err)
		os.Exit(1)
	}
}

// run fetches the live document, renders the seed from it, and either writes
// the two files or — under check — reports how they drift.
func run(ctx context.Context, url, mirror, out, cellsPath string, check bool, keys []ed25519.PublicKey) error {
	cacheDir, err := os.MkdirTemp("", "seedgen-cache-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(cacheDir)

	live, err := fetch(ctx, url, mirror, keys, cacheDir)
	if err != nil {
		return fmt.Errorf("fetch the index: %w", err)
	}
	// The reader has to accept it — the same refusal a running install makes,
	// schema included — before a byte of it is copied anywhere.
	if _, err := index.Parse(live); err != nil {
		return fmt.Errorf("the fetched document is not one this reader accepts: %w", err)
	}

	seed, err := render(live, index.Seed())
	if err != nil {
		return err
	}
	figure, err := cellsCSV(seed)
	if err != nil {
		return err
	}

	if check {
		if err := checkFile(out, seed); err != nil {
			return err
		}
		if err := checkFile(cellsPath, figure); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "seedgen: %s and %s are what the relay would write\n", out, cellsPath)
		return nil
	}
	if err := writeAtomic(out, seed); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	if err := writeAtomic(cellsPath, figure); err != nil {
		return fmt.Errorf("write %s: %w", cellsPath, err)
	}
	fmt.Fprintf(os.Stdout, "seedgen: wrote %s (%d bytes) and %s\n", out, len(seed), cellsPath)
	return nil
}

// fetch reads the signed index, from the relay and — when the relay does not
// answer — from the mirror. pull verifies the signature under keys, refuses a
// document over its size cap, and its TTL of zero keeps a cached copy from
// answering in place of a fresh one.
func fetch(ctx context.Context, url, mirror string, keys []ed25519.PublicKey, cacheDir string) ([]byte, error) {
	doc, err := pullOnce(ctx, url, keys, cacheDir)
	if err == nil {
		return doc, nil
	}
	if mirror == "" || mirror == url {
		return nil, err
	}
	doc, mirrorErr := pullOnce(ctx, mirror, keys, cacheDir)
	if mirrorErr == nil {
		return doc, nil
	}
	return nil, fmt.Errorf("relay: %v; mirror: %v", err, mirrorErr)
}

// pullOnce is one address under pull: a fresh fetch, its failure said rather
// than fallen back to nothing. TTL zero makes no cached copy an answer, and the
// cache directory is a throwaway so one address's bytes never serve the other.
func pullOnce(ctx context.Context, url string, keys []ed25519.PublicKey, cacheDir string) ([]byte, error) {
	p := &pull.Puller{URL: url, Keys: keys, CacheDir: cacheDir, TTL: 0, Budget: pull.DefaultBudget}
	result, err := p.Pull(ctx)
	if err != nil {
		return nil, err
	}
	if result.Doc == nil {
		return nil, errors.New("the address answered with no document")
	}
	return result.Doc, nil
}

// seedCell is one cell on its way into the seed: its raw bytes, copied
// verbatim, and the dim values its address sorts by.
type seedCell struct {
	key []string
	raw json.RawMessage
}

// render builds the seed from the live document and the embedded one. It is
// pure: the same two documents always render the same bytes, which is what
// makes -check meaningful. It refuses a document the reader would not trust
// whole — a version that is not an integer, a day that does not parse, a cell
// below the floor.
func render(live, embedded []byte) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(live, &top); err != nil {
		return nil, fmt.Errorf("the live document is not a json object: %w", err)
	}

	// THE FIXED FIELDS. Each is required: a document missing one is refused
	// rather than copied with a hole, because a seed is read by every fresh
	// install and a hole is worse than a failure here.
	schema, err := required(top, "schema")
	if err != nil {
		return nil, err
	}
	version, err := required(top, "version")
	if err != nil {
		return nil, err
	}
	if _, err := strconv.ParseInt(strings.TrimSpace(string(version)), 10, 64); err != nil {
		return nil, fmt.Errorf("the live document's version is not an integer: %s", version)
	}
	generatedRaw, err := required(top, "generated")
	if err != nil {
		return nil, err
	}
	var generated string
	if err := json.Unmarshal(generatedRaw, &generated); err != nil {
		return nil, fmt.Errorf("the live document's generated is not a string: %s", generatedRaw)
	}
	if _, err := time.Parse("2006-01-02", generated); err != nil {
		return nil, fmt.Errorf("the live document's generated day does not parse: %q", generated)
	}
	minRaw, err := required(top, "min_installs")
	if err != nil {
		return nil, err
	}
	var liveMin int
	if err := json.Unmarshal(minRaw, &liveMin); err != nil {
		return nil, fmt.Errorf("the live document's min_installs is not a number: %s", minRaw)
	}
	judges, err := required(top, "judges")
	if err != nil {
		return nil, err
	}
	rubrics, err := required(top, "rubrics")
	if err != nil {
		return nil, err
	}

	// The metric declarations, raw, keyed by the name spelled, with each
	// declaration's dims in declared order — the order a cell's address is
	// sorted by and copied by.
	var decls map[string]json.RawMessage
	if raw, ok := top["metrics"]; !ok {
		return nil, errors.New("the live document has no metrics")
	} else if err := json.Unmarshal(raw, &decls); err != nil {
		return nil, fmt.Errorf("the live document's metrics are not an object: %w", err)
	}
	dims := make(map[string][]string, len(decls))
	for name, raw := range decls {
		var decl struct {
			Dims []string `json:"dims"`
		}
		if err := json.Unmarshal(raw, &decl); err != nil {
			return nil, fmt.Errorf("metric %q is not a declaration: %w", name, err)
		}
		dims[name] = decl.Dims
	}

	// THE CELLS, copied verbatim and grouped by metric. A cell whose metric the
	// document does not declare, or one below the floor, is refused: the reader
	// would drop it, and a seed that dropped a cell Parse kept would not be the
	// document it claims to be.
	type cell = seedCell
	byMetric := map[string][]cell{}
	var rawCells []json.RawMessage
	if raw, ok := top["cells"]; !ok {
		return nil, errors.New("the live document has no cells")
	} else if err := json.Unmarshal(raw, &rawCells); err != nil {
		return nil, fmt.Errorf("the live document's cells are not an array: %w", err)
	}
	for i, raw := range rawCells {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("cell %d is not an object: %w", i, err)
		}
		var metric string
		if err := json.Unmarshal(fields["metric"], &metric); err != nil || metric == "" {
			return nil, fmt.Errorf("cell %d names no metric", i)
		}
		if _, declared := decls[metric]; !declared {
			return nil, fmt.Errorf("cell %d names metric %q, which the document does not declare", i, metric)
		}
		floor, err := cellFloor(fields)
		if err != nil {
			return nil, fmt.Errorf("cell %d: %w", i, err)
		}
		if floor < liveMin {
			return nil, fmt.Errorf("cell %d (%s) is below the floor of %d: it stands on %d", i, metric, liveMin, floor)
		}
		key := make([]string, 0, len(dims[metric]))
		for _, dim := range dims[metric] {
			key = append(key, stringField(fields[dim]))
		}
		byMetric[metric] = append(byMetric[metric], cell{key: key, raw: raw})
	}

	// The aliases: the live document's entries stand on top, and the embedded
	// seed's entries fill in beneath, so an alias the relay has dropped survives
	// in the seed.
	aliases, err := mergeAliases(top["aliases"], embedded)
	if err != nil {
		return nil, err
	}

	// The floor the seed will carry is the live document's, never a lower one:
	// a floor below the relay's would admit cells the relay itself dropped, so
	// the copier does not offer a way to set it.
	seedMin := liveMin

	compact := objectOf([][2][]byte{
		{quote("schema"), schema},
		{quote("version"), version},
		{quote("generated"), generatedRaw},
		{quote("min_installs"), quoteNumber(seedMin)},
		{quote("judges"), judges},
		{quote("rubrics"), rubrics},
		{quote("aliases"), aliases},
		{quote("metrics"), metricObject(decls)},
		{quote("cells"), cellArray(byMetric, dims)},
	})

	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", " "); err != nil {
		return nil, fmt.Errorf("the rendered seed is not valid json: %w", err)
	}
	out.WriteByte('\n')
	if out.Len() > maxSeed {
		return nil, fmt.Errorf("the rendered seed is %d bytes, over the %d-byte cap", out.Len(), maxSeed)
	}
	return out.Bytes(), nil
}

// required reads one top-level field, or refuses because the document has none.
func required(top map[string]json.RawMessage, name string) (json.RawMessage, error) {
	raw, ok := top[name]
	if !ok {
		return nil, fmt.Errorf("the live document has no %s", name)
	}
	return raw, nil
}

// mergeAliases renders the seed's aliases: the live document's entries as they
// stand, with the embedded seed's entries added under any key the live map does
// not already claim. The keys are sorted so the same two maps always render the
// same bytes.
func mergeAliases(liveRaw, embedded []byte) ([]byte, error) {
	merged := map[string]json.RawMessage{}
	if len(liveRaw) > 0 {
		var live map[string]json.RawMessage
		if err := json.Unmarshal(liveRaw, &live); err != nil {
			return nil, fmt.Errorf("the live document's aliases are not an object: %w", err)
		}
		for k, v := range live {
			merged[k] = v
		}
	}
	var embeddedTop map[string]json.RawMessage
	if len(embedded) > 0 {
		if err := json.Unmarshal(embedded, &embeddedTop); err != nil {
			return nil, fmt.Errorf("the embedded seed is not a json object: %w", err)
		}
		var embeddedAliases map[string]json.RawMessage
		if raw, ok := embeddedTop["aliases"]; ok {
			if err := json.Unmarshal(raw, &embeddedAliases); err != nil {
				return nil, fmt.Errorf("the embedded seed's aliases are not an object: %w", err)
			}
		}
		for k, v := range embeddedAliases {
			if _, claimed := merged[k]; !claimed {
				merged[k] = v
			}
		}
	}
	names := make([]string, 0, len(merged))
	for k := range merged {
		names = append(names, k)
	}
	sort.Strings(names)
	return objectOf(rawPairs(names, merged)), nil
}

// cellFloor reads the count the reader floors a cell on: its installs when it
// carries them, its rows when it does not. A missing field reads as zero, the
// same way the reader reads it.
func cellFloor(fields map[string]json.RawMessage) (int, error) {
	if raw, ok := fields["installs"]; ok {
		return countOf(raw)
	}
	return countOf(fields["n"])
}

// countOf reads a JSON number as an int, and zero for anything else.
func countOf(raw json.RawMessage) (int, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, fmt.Errorf("%s is not a number", raw)
	}
	return int(f), nil
}

// stringField reads a JSON string, and "" for anything else — an absent or
// non-string dim value sorts as empty, the same way the reader addresses it.
func stringField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// metricObject renders the metrics declaration: every declared metric, in
// sorted name order, its declaration copied verbatim.
func metricObject(decls map[string]json.RawMessage) []byte {
	names := make([]string, 0, len(decls))
	for name := range decls {
		names = append(names, name)
	}
	sort.Strings(names)
	return objectOf(rawPairs(names, decls))
}

// cellArray renders the cells: grouped by metric in sorted name order, and
// within a metric sorted by the metric's declared dims in declared order. Each
// cell is its own bytes, verbatim.
func cellArray(byMetric map[string][]seedCell, dims map[string][]string) []byte {
	names := make([]string, 0, len(byMetric))
	for name := range byMetric {
		names = append(names, name)
	}
	sort.Strings(names)
	var items [][]byte
	for _, name := range names {
		cells := byMetric[name]
		sort.SliceStable(cells, func(i, j int) bool { return lessDims(cells[i].key, cells[j].key) })
		for _, c := range cells {
			items = append(items, c.raw)
		}
	}
	return arrayOf(items)
}

// lessDims orders two cells by their dim values in the metric's declared dim
// order, lexicographically.
func lessDims(a, b []string) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// rawPairs pairs each name with its quoted key and raw value, in the order
// given.
func rawPairs(names []string, values map[string]json.RawMessage) [][2][]byte {
	pairs := make([][2][]byte, 0, len(names))
	for _, name := range names {
		pairs = append(pairs, [2][]byte{quote(name), values[name]})
	}
	return pairs
}

// objectOf renders a compact JSON object whose keys are written in the order
// handed over — the fixed key order the relay's own renderer writes, so the
// seed and the live document read the same way.
func objectOf(pairs [][2][]byte) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(p[0])
		b.WriteByte(':')
		b.Write(p[1])
	}
	b.WriteByte('}')
	return b.Bytes()
}

// arrayOf renders a compact JSON array of the items given.
func arrayOf(items [][]byte) []byte {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(item)
	}
	b.WriteByte(']')
	return b.Bytes()
}

// quote renders a string as a JSON key.
func quote(s string) []byte {
	b, _ := json.Marshal(s)
	return b
}

// quoteNumber renders an int as a JSON number.
func quoteNumber(n int) []byte {
	return []byte(strconv.Itoa(n))
}

// cellsCSV renders the seed's role_quality cells as the paper's figure:
// role, model, mean, sd, n and installs, one row per cell, in the seed's own
// order. The metric is named here because the figure is role_quality's; the
// copier names none.
func cellsCSV(seed []byte) ([]byte, error) {
	var doc struct {
		Cells []struct {
			Metric   string  `json:"metric"`
			Role     string  `json:"role"`
			Model    string  `json:"model"`
			Mean     float64 `json:"mean"`
			SD       float64 `json:"sd"`
			N        int     `json:"n"`
			Installs int     `json:"installs"`
		} `json:"cells"`
	}
	if err := json.Unmarshal(seed, &doc); err != nil {
		return nil, fmt.Errorf("read the rendered seed for the figure: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("role,model,mean,sd,n,installs\n")
	for _, c := range doc.Cells {
		if c.Metric != roleQualityMetric {
			continue
		}
		fmt.Fprintf(&b, "%s,%s,%s,%s,%d,%d\n",
			c.Role, c.Model, floatText(c.Mean), floatText(c.SD), c.N, c.Installs)
	}
	return b.Bytes(), nil
}

// floatText renders a float with the shortest decimal that reads back to it, so
// a figure's number is the seed's number rather than a rounded stand-in.
func floatText(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// checkFile compares what the relay would write against what is on disk, and
// says how they drift when they do.
func checkFile(path string, want []byte) error {
	got, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s does not exist; run without -check to write it: %w", path, errDrift)
		}
		return err
	}
	if bytes.Equal(got, want) {
		return nil
	}
	fmt.Fprintf(os.Stderr, "seedgen: %s is not what the relay would write\n", path)
	writeDiff(os.Stderr, got, want)
	return errDrift
}

// writeAtomic writes data to a temporary file beside the target and renames it
// into place, so a run cut in half never leaves a half-written seed.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// lineOp is one line of a diff: ' ' common, '-' only in the file, '+' only in
// what the relay would write, '!' a run of unchanged lines elided.
type lineOp struct {
	kind byte
	text string
}

// writeDiff writes a line-level diff of got against want with a little
// unchanged context, so a release reads what moved rather than only that
// something did. A file pair too large to align is reported by size alone.
func writeDiff(w io.Writer, got, want []byte) {
	a, b := splitLines(got), splitLines(want)
	if len(a)*len(b) > 4_000_000 {
		fmt.Fprintf(w, "  (%d lines in the file, %d from the relay)\n", len(a), len(b))
		return
	}
	for _, op := range collapse(lineOps(a, b)) {
		switch op.kind {
		case '!':
			fmt.Fprintln(w, "  …")
		default:
			fmt.Fprintf(w, "%c %s\n", op.kind, op.text)
		}
	}
}

// splitLines splits text into lines, dropping one trailing newline so a file
// and its bytes compare as the same lines.
func splitLines(data []byte) []string {
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// lineOps aligns two line lists by their longest common subsequence and returns
// the common, removed and added lines in order.
func lineOps(a, b []string) []lineOp {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				dp[i][j] = dp[i+1][j+1] + 1
			case dp[i+1][j] >= dp[i][j+1]:
				dp[i][j] = dp[i+1][j]
			default:
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []lineOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, lineOp{' ', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, lineOp{'-', a[i]})
			i++
		default:
			ops = append(ops, lineOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, lineOp{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, lineOp{'+', b[j]})
	}
	return ops
}

// collapse trims long runs of unchanged lines to their first and last few, with
// an elision between, so one changed line in a long file does not print the
// whole file.
func collapse(ops []lineOp) []lineOp {
	const context = 2
	var out []lineOp
	for i := 0; i < len(ops); {
		if ops[i].kind != ' ' {
			out = append(out, ops[i])
			i++
			continue
		}
		j := i
		for j < len(ops) && ops[j].kind == ' ' {
			j++
		}
		run := ops[i:j]
		if len(run) <= 2*context+1 {
			out = append(out, run...)
		} else {
			out = append(out, run[:context]...)
			out = append(out, lineOp{kind: '!'})
			out = append(out, run[len(run)-context:]...)
		}
		i = j
	}
	return out
}
