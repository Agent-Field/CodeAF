// Package outcomecache is the Go port of src/session/outcome-cache.ts —
// content-addressed memoization for outcomes that have passed verification.
//
// The TS module's own header states the invalidation rule; it is reproduced
// verbatim in behavior here and is worth re-reading before touching the key:
// the key binds (normalized brief text, git tree hash, model id); a RECORD is
// keyed by the tree that already contains the merged leaf and only while that
// tree is clean; a CHECK runs against the current tree hash. Over-invalidation
// is the safe direction — never narrow the key.
//
// Fidelity notes (deliberate, do not "fix"):
//
//   - outcomeCacheKey's normalization is `trim()` then `.replace(/\s+/g, " ")`.
//     JS \s is WhiteSpace ∪ LineTerminator — a much wider class than RE2's \s
//     ([\t\n\f\r ], which does not even include \v). The collapse is therefore
//     hand-rolled over the explicit JS class; getting this wrong changes every
//     cache key for any brief containing NBSP, an ideographic space, a BOM, …
//   - The digest is over the UTF-8 encoding of the JS string, so an unpaired
//     surrogate hashes as U+FFFD (Node's crypto `update(s, "utf8")`), which is
//     what toUTF8 reproduces.
//   - get() returns the JSON.parse'd value UNVALIDATED (`return parsed` after
//     two truthiness checks), so unknown keys and the entry's key order are
//     observable. CachedOutcome keeps the raw parse and marshals from it.
//   - loadOutcomeCacheStore's guard is `raw && typeof raw === "object"`, which
//     lets an ARRAY through: a JSON array file becomes a store keyed "0", "1",
//     … via Object.entries. Kept.
//   - Both Object.entries (load) and Object.fromEntries + JSON.stringify
//     (persist) apply JS own-property order, i.e. array-index-like keys
//     ASCENDING FIRST and only then insertion order. Real keys are sha256 hex
//     so this never fires in production, but a hand-written sidecar reaches it.
//   - Every fs and parse failure is swallowed on both sides: the reader yields
//     a cold cache, the writer silently drops the entry. The Go port returns no
//     errors from those paths for the same reason.
//   - The module reads no clock and no RNG, so there is no injectable now().
package outcomecache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// CachedOutcome mirrors the CachedOutcome interface. The raw parse is carried
// alongside the typed projection so a value returned by Get re-serializes
// byte-for-byte — extra keys, key order and number spelling included — exactly
// as `JSON.stringify(cache.get(k))` does in TS.
type CachedOutcome struct {
	Key     string      `json:"key"`
	Outcome LeafOutcome `json:"outcome"`

	raw *jsVal
}

// MarshalJSON emits the raw parsed entry when there is one; a hand-built value
// falls back to the declaration-order rendering of its two fields.
func (c CachedOutcome) MarshalJSON() ([]byte, error) {
	if c.raw != nil {
		return c.raw.appendJSON(nil), nil
	}
	obj := newJSObj()
	obj.set("key", stringVal(c.Key))
	obj.set("outcome", outcomeToJSVal(c.Outcome))
	return objectVal(obj).appendJSON(nil), nil
}

// OutcomeCacheKeyOpts is outcomeCacheKey's inline argument object.
type OutcomeCacheKeyOpts struct {
	BriefText string `json:"briefText"`
	TreeHash  string `json:"treeHash"`
	ModelID   string `json:"modelID"`
}

// isJSRegexSpace is the JS `\s` character class: WhiteSpace ∪ LineTerminator.
// RE2's \s is [\t\n\f\r ] and Go's unicode.IsSpace omits U+FEFF, so neither can
// stand in for it.
func isJSRegexSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// collapseJSWhitespaceRuns is `s.replace(/\s+/g, " ")`. Hand-rolled rather than
// regexp-driven both for the character class above and because it must copy
// malformed bytes (WTF-8 surrogates) through untouched instead of letting RE2
// rewrite them to U+FFFD.
func collapseJSWhitespaceRuns(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		if !isJSRegexSpace(cp) {
			b.WriteString(s[i : i+size])
			i += size
			continue
		}
		b.WriteByte(' ')
		i += size
		for i < len(s) {
			cp2, size2 := decodeWTF8(s, i)
			if !isJSRegexSpace(cp2) {
				break
			}
			i += size2
		}
	}
	return b.String()
}

// OutcomeCacheKey mirrors outcomeCacheKey: a stable cache key built from the
// brief, repository tree, and model.
func OutcomeCacheKey(opts OutcomeCacheKeyOpts) string {
	normalizedBrief := collapseJSWhitespaceRuns(jscompat.Trim(opts.BriefText))
	canonical := strings.Join([]string{normalizedBrief, opts.TreeHash, opts.ModelID}, "\n")
	sum := sha256.Sum256([]byte(toUTF8(canonical)))
	return hex.EncodeToString(sum[:])
}

// ExecResult is the shape computeTreeState's injected exec resolves to.
// ExitCode is a float64 because the TS compares it with `!== 0`: a NaN exit
// code is not equal to 0 and aborts, a -0 exit code is.
type ExecResult struct {
	Stdout   string  `json:"stdout"`
	ExitCode float64 `json:"exitCode"`
}

// TreeState mirrors the TreeState interface — and also computeTreeState's
// structurally identical inline return type.
type TreeState struct {
	TreeHash string `json:"treeHash"`
	Dirty    bool   `json:"dirty"`
}

// MarshalJSON renders the pair the way JSON.stringify would. Go's encoding/json
// escapes U+2028/U+2029 inside strings and V8 does not, and a tree hash is
// caller-supplied text, so the quoting goes through the package's own quoter.
func (s TreeState) MarshalJSON() ([]byte, error) {
	obj := newJSObj()
	obj.set("treeHash", stringVal(s.TreeHash))
	obj.set("dirty", boolVal(s.Dirty))
	return objectVal(obj).appendJSON(nil), nil
}

// ComputeTreeState mirrors computeTreeState: read the current tree identity and
// whether the worktree has changes. A non-zero exit from either command, or a
// rejected/throwing exec, yields nil.
func ComputeTreeState(exec func(cmd []string) (ExecResult, error)) *TreeState {
	hashResult, err := exec([]string{"git", "rev-parse", "HEAD^{tree}"})
	if err != nil {
		return nil
	}
	if hashResult.ExitCode != 0 {
		return nil
	}

	statusResult, err := exec([]string{"git", "status", "--porcelain"})
	if err != nil {
		return nil
	}
	if statusResult.ExitCode != 0 {
		return nil
	}

	return &TreeState{
		TreeHash: jscompat.Trim(hashResult.Stdout),
		// `statusResult.stdout.length > 0` — a UTF-16 code-unit count, but any
		// non-empty string has at least one unit, so this is the same predicate.
		Dirty: len(statusResult.Stdout) > 0,
	}
}

// PutEntry is createOutcomeCache().put's argument object.
type PutEntry struct {
	Key     string      `json:"key"`
	Outcome LeafOutcome `json:"outcome"`
	Dirty   bool        `json:"dirty"`
}

// PutResult is put's return object; the field order is the TS literal's.
type PutResult struct {
	Stored bool   `json:"stored"`
	Reason string `json:"reason"`
}

// OutcomeCache is the closure pair createOutcomeCache returns, over the same
// injected store.
type OutcomeCache struct {
	store *jscompat.OrderedMap[string, string]
}

// CreateOutcomeCache mirrors createOutcomeCache. A nil store stands for the TS
// default parameter `= new Map()`.
func CreateOutcomeCache(store *jscompat.OrderedMap[string, string]) *OutcomeCache {
	if store == nil {
		store = jscompat.NewOrderedMap[string, string]()
	}
	return &OutcomeCache{store: store}
}

// Store exposes the backing map, which the TS caller also holds a reference to
// (createOutcomeCache closes over the caller's Map — recordVerifiedOutcome
// persists that same object after put mutates it).
func (c *OutcomeCache) Store() *jscompat.OrderedMap[string, string] { return c.store }

// Get mirrors get: a missing entry, unparseable JSON, a falsy parse, a key
// mismatch or a falsy `outcome` all yield nil; otherwise the parsed value is
// returned as-is.
func (c *OutcomeCache) Get(key string) *CachedOutcome {
	raw, ok := c.store.Get(key)
	if !ok {
		return nil
	}
	parsed, parsedOK := parseJSON(raw)
	if !parsedOK {
		return nil
	}
	if !parsed.truthy() {
		return nil
	}
	// `parsed.key !== key`: an absent property is undefined, which is never
	// strictly equal to a string, and a non-string property never is either.
	pk, hasKey := parsed.prop("key")
	if !hasKey || pk.kind != jsString || pk.str != key {
		return nil
	}
	outcome, hasOutcome := parsed.prop("outcome")
	if !hasOutcome || !outcome.truthy() {
		return nil
	}
	kept := parsed
	return &CachedOutcome{
		Key:     pk.str,
		Outcome: leafOutcomeFromJSVal(outcome),
		raw:     &kept,
	}
}

// Put mirrors put, including the three distinct poison-guard reasons and their
// order: verdict first, evidence second, dirty tree last.
func (c *OutcomeCache) Put(entry PutEntry) PutResult {
	if entry.Outcome.Verdict != "pass" {
		return PutResult{Stored: false, Reason: "outcome verdict is not pass"}
	}

	evidence := entry.Outcome.Evidence
	if !(evidence != nil && (evidence.AuditCommandsRun > 0 || evidence.InRunTestsPassed)) {
		return PutResult{Stored: false, Reason: "pass outcome has no verification evidence"}
	}

	if entry.Dirty {
		return PutResult{Stored: false, Reason: "tree is dirty"}
	}

	// The TS wraps store.set in try/catch for the "outcome could not be
	// serialized" path, which only a circular structure or a BigInt could
	// trigger; a LeafOutcome value can hold neither, so the branch is dead in
	// both languages and is left unreachable here rather than faked.
	obj := newJSObj()
	obj.set("key", stringVal(entry.Key))
	obj.set("outcome", outcomeToJSVal(entry.Outcome))
	c.store.Set(entry.Key, objectVal(obj).stringify())
	return PutResult{Stored: true, Reason: "verified pass stored"}
}

// ============================================================================
// Workspace persistence — tolerant JSON sidecar under .codeaf/. The reader
// never fails (a missing or corrupt file is a cold cache) and the writer never
// fails (a failed write just means the entry is not persisted). Best-effort by
// design — a lost cache entry only costs a re-dispatch, which is always safe.
// ============================================================================

const cacheFile = ".codeaf/outcome-cache.json"

// LoadOutcomeCacheStore mirrors loadOutcomeCacheStore: a missing/corrupt cache
// file yields an empty store, and only string values are kept.
func LoadOutcomeCacheStore(workspace string) *jscompat.OrderedMap[string, string] {
	data, err := os.ReadFile(filepath.Join(workspace, cacheFile))
	if err != nil {
		return jscompat.NewOrderedMap[string, string]()
	}
	raw, ok := parseJSON(decodeUTF8Lossy(data))
	if !ok {
		return jscompat.NewOrderedMap[string, string]()
	}
	// `raw && typeof raw === "object"` — true for objects AND arrays, false for
	// null (typeof null is "object" but null is falsy) and every primitive.
	if raw.truthy() && (raw.kind == jsObject || raw.kind == jsArray) {
		store := jscompat.NewOrderedMap[string, string]()
		for _, e := range objectEntries(raw) {
			if e.val.kind == jsString {
				store.Set(e.key, e.val.str)
			}
		}
		return store
	}
	return jscompat.NewOrderedMap[string, string]()
}

type jsEntry struct {
	key string
	val jsVal
}

// objectEntries is Object.entries(v): own enumerable string-keyed properties in
// JS property order (array-index keys ascending, then insertion order). For an
// array that is "0", "1", … — `length` is not enumerable.
func objectEntries(v jsVal) []jsEntry {
	switch v.kind {
	case jsArray:
		out := make([]jsEntry, 0, len(v.arr))
		for i, el := range v.arr {
			out = append(out, jsEntry{key: jscompat.FormatNumber(float64(i)), val: el})
		}
		return out
	case jsObject:
		keys := v.obj.orderedKeys()
		out := make([]jsEntry, 0, len(keys))
		for _, k := range keys {
			out = append(out, jsEntry{key: k, val: v.obj.vals[k]})
		}
		return out
	}
	return nil
}

// PersistOutcomeCacheStore mirrors persistOutcomeCacheStore: a defensive writer
// that never fails. Note the serialized object is built with
// Object.fromEntries, so JS property order — not the Map's insertion order —
// decides the file's key order.
func PersistOutcomeCacheStore(workspace string, store *jscompat.OrderedMap[string, string]) {
	file := filepath.Join(workspace, cacheFile)
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		return
	}
	if store == nil {
		// `Object.fromEntries(undefined)` is a TypeError, which the TS catch
		// swallows AFTER the mkdir has already run: directory created, no file.
		// Unreachable through the typed signature, kept for shape.
		return
	}
	obj := newJSObj()
	for _, e := range store.Entries() {
		obj.set(e.Key, stringVal(e.Val))
	}
	_ = os.WriteFile(file, []byte(toUTF8(objectVal(obj).stringify())), 0o666)
}

// LookupOpts is lookupVerifiedOutcome's inline lookup object.
type LookupOpts struct {
	BriefText string    `json:"briefText"`
	TreeState TreeState `json:"treeState"`
	ModelID   string    `json:"modelID"`
}

// LookupVerifiedOutcome mirrors lookupVerifiedOutcome: look up a previously
// VERIFIED-pass outcome for this exact (brief, CLEAN tree, model). Returns nil
// on a dirty tree — a dirty tree's committed hash does not describe the working
// state, so honoring a hit could skip real work — and on a miss or any error.
func LookupVerifiedOutcome(workspace string, lookup LookupOpts) *CachedOutcome {
	if lookup.TreeState.Dirty {
		return nil
	}
	store := LoadOutcomeCacheStore(workspace)
	cache := CreateOutcomeCache(store)
	key := OutcomeCacheKey(OutcomeCacheKeyOpts{
		BriefText: lookup.BriefText,
		TreeHash:  lookup.TreeState.TreeHash,
		ModelID:   lookup.ModelID,
	})
	return cache.Get(key)
}

// RecordOpts is recordVerifiedOutcome's inline entry object.
type RecordOpts struct {
	BriefText string      `json:"briefText"`
	TreeState TreeState   `json:"treeState"`
	ModelID   string      `json:"modelID"`
	Outcome   LeafOutcome `json:"outcome"`
}

// RecordVerifiedOutcome mirrors recordVerifiedOutcome: record a VERIFIED-pass
// outcome keyed by the tree state that ALREADY CONTAINS the merged result (see
// the package header). All poison-guard policy is delegated to Put; the sidecar
// is written only when the entry is actually stored.
//
// The TS catch arm (`record failed: ${String(err).slice(0, 120)}`) has no
// reachable trigger — loadOutcomeCacheStore, createOutcomeCache,
// outcomeCacheKey and put each swallow or cannot raise — so the port has no
// error path either.
func RecordVerifiedOutcome(workspace string, entry RecordOpts) PutResult {
	store := LoadOutcomeCacheStore(workspace)
	cache := CreateOutcomeCache(store)
	key := OutcomeCacheKey(OutcomeCacheKeyOpts{
		BriefText: entry.BriefText,
		TreeHash:  entry.TreeState.TreeHash,
		ModelID:   entry.ModelID,
	})
	result := cache.Put(PutEntry{Key: key, Outcome: entry.Outcome, Dirty: entry.TreeState.Dirty})
	if result.Stored {
		PersistOutcomeCacheStore(workspace, store)
	}
	return result
}
