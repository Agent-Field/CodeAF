package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// The profile file was the only consumer of a model name in the tree that did
// not resolve it, and it is the one where the cost compounds.
//
// The doctrine is stated at internal/provider/wire.go: "~x and x are one model".
// Every other reader honours it — the adapter memoises quirks under a normalised
// key, the catalog resolves aliases, the subharness translates a spelling before
// handing it to another process. The ruler keyed its file on slug(whatever the
// operator typed), so two spellings of one model accumulated two histories and
// two rulers: measured, medians of 13,223 and 44,704 tokens for the same model
// on the same machine, each of them a real average of half the evidence, and the
// planner sized against whichever half the day's spelling happened to open.
//
// The resolution is injected rather than imported. profile sits below the
// catalog in every other respect and importing it here to answer one question
// would drag a network-backed discovery service into a package whose whole job
// is reading a small JSON file. So a surface that has a catalog installs its
// answer once, and a surface that has none keeps the normalisation alone — which
// is exactly what every caller had before this existed.

// Resolve turns one spelling of a model into the identity its history is kept
// under. It must be pure, cheap and non-blocking: it is called on the launch
// path, and a resolver that waited on a fetch would put that fetch in front of
// the first frame of a run.
type Resolve func(model string) string

var identity struct {
	sync.Mutex
	resolve Resolve
	memo    map[string]string
}

// UseIdentity installs the catalog-backed resolution for this process. It is the
// same shape as plan.UseAnchors and is installed at the same place and time: once
// at launch, by the surface that owns the catalog.
//
// Installing a resolver clears the memo, so a process that installs late still
// gets one consistent answer from that point on; what was written under the
// unresolved spelling before it is not lost but adopted, by the merge in Load.
func UseIdentity(resolve Resolve) {
	identity.Lock()
	defer identity.Unlock()
	identity.resolve, identity.memo = resolve, nil
}

// Identity is the model a history belongs to.
//
// Two layers, and only the first is always there. The free one is the doctrine's
// own normalisation — the leading "~" is aforge's routing marker and not part of
// any slug, and case is not an identity — which is what a surface with no catalog
// gets. The second is whatever resolver was installed, applied over the first.
//
// The answer is memoised per spelling for the life of the process, because the
// installed resolver reads a catalog that warms in the background: asked before
// it lands and again after, it would honestly give two answers, and a file key
// that moved halfway through a run would split a history inside one process
// rather than across two. First answer wins; the merge in Load is what makes an
// early, unresolved answer cost nothing but a merge later.
func Identity(model string) string {
	normalized := normalizeModel(model)
	if normalized == "" {
		return ""
	}
	identity.Lock()
	defer identity.Unlock()
	if remembered, ok := identity.memo[normalized]; ok {
		return remembered
	}
	resolved := normalized
	if identity.resolve != nil {
		if answer := normalizeModel(identity.resolve(normalized)); answer != "" {
			resolved = answer
		}
	}
	if identity.memo == nil {
		identity.memo = map[string]string{}
	}
	identity.memo[normalized] = resolved
	return resolved
}

// normalizeModel is provider.normalizeModel's rule, restated where a file name
// is built: the leading "~" is aforge's own routing marker, not part of the
// slug, so "~minimax/minimax-m2.7" and "minimax/minimax-m2.7" are one model.
func normalizeModel(model string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(model), "~"))
}

// adoption is what one merge found: the records written under other spellings
// of this model, and the newest ruler any of them had rewritten for itself.
type adoption struct {
	records []Record
	anchors string
}

// adopted memoises one migration per target path, so the glob below runs once
// per process rather than once per read — and so a caller that reads a merged
// history without saving it still sees the same history on the next read.
var adopted sync.Map // path -> adoption

// adopt gathers the histories that were written under other spellings of one
// model, and it runs only where the model's own file does not exist yet.
//
// That condition is the whole of the safety. A resolved identity with a file of
// its own is already the one history; a resolved identity with no file is either
// a model nobody has run — where the glob finds nothing and costs a directory
// listing — or a model whose evidence is sitting under a name that resolves to
// it, which is the case this exists for. Merging is by timestamp and bounded by
// maxRecords, exactly as Add bounds the file, so an adoption cannot make a
// profile larger than a profile is allowed to be.
//
// It reads and never writes. The merged records materialise into the resolved
// file the first time somebody Saves one, which keeps Load a read — a listing
// view must not have side effects on disk — and keeps the operation idempotent:
// the sources are never mutated, so running it again produces the same answer.
func adopt(dir, resolved, subharness, target string) adoption {
	if remembered, ok := adopted.Load(target); ok {
		found := remembered.(adoption)
		return adoption{records: append([]Record(nil), found.records...), anchors: found.anchors}
	}
	records, anchors := scanForIdentity(dir, resolved, subharness, target)
	found := adoption{records: records, anchors: anchors}
	adopted.Store(target, found)
	return adoption{records: append([]Record(nil), found.records...), anchors: found.anchors}
}

// scanForIdentity reads every profile file for this subharness and keeps the
// records of the ones that name the same model under a different spelling.
func scanForIdentity(dir, resolved, subharness, target string) ([]Record, string) {
	matches, err := filepath.Glob(filepath.Join(dir, "profile-*-"+slug(subharness)+".json"))
	if err != nil || len(matches) == 0 {
		return nil, ""
	}
	sort.Strings(matches)
	var records []Record
	var anchors string
	var anchorsAt int64
	for _, candidate := range matches {
		if candidate == target {
			continue
		}
		data, readErr := os.ReadFile(candidate)
		if readErr != nil {
			continue
		}
		var header Profile
		if json.Unmarshal(data, &header) != nil {
			continue
		}
		// The file name is a slug and several spellings share one; the header is
		// the authority on which model actually wrote it.
		if slug(header.Subharness) != slug(subharness) || Identity(header.Model) != resolved {
			continue
		}
		records = append(records, header.Records...)
		if strings.TrimSpace(header.Anchors) != "" {
			if info, statErr := os.Stat(candidate); statErr == nil && info.ModTime().Unix() >= anchorsAt {
				anchors, anchorsAt = header.Anchors, info.ModTime().Unix()
			}
		}
	}
	if len(records) == 0 {
		return nil, ""
	}
	// Timestamped, so a merge of two histories is one history in the order it
	// happened. Records written before Record.Time existed carry the zero time
	// and sort to the front, which is where the oldest evidence belongs.
	sort.SliceStable(records, func(i, j int) bool { return records[i].Time.Before(records[j].Time) })
	if len(records) > maxRecords {
		records = records[len(records)-maxRecords:]
	}
	return records, anchors
}
