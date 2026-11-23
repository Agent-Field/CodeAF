#!/usr/bin/env python3
"""Generate a test-only Go overlay for four-crew accounting counts.

Usage: python3 scripts/fourcrew-stream-probe.py TREE OUTPUT_DIRECTORY
Build with GOTOOLCHAIN=local GOPROXY=off TMPDIR=/var/tmp, using:
  nice -n 10 go test -tags=e2e -overlay=OUTPUT_DIRECTORY/overlay.json -c ./cmd/codeaf
The tree must contain usage_fourcrew_live_e2e_test.go. No source is modified.
CODEAF_FOURCREW_SYNC_BLOCK_MS delays only background journal sync in this build.
Counters include setup and clean close; compare totals with provider call count.
"""
import json
import pathlib
import re
import sys


def replace_one(source, old, new):
    if source.count(old) != 1:
        raise ValueError(f"expected exactly one instrumentation site: {old}")
    return source.replace(old, new, 1)


def generate(tree, output):
    tree = pathlib.Path(tree).resolve()
    output = pathlib.Path(output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    replacements = {}

    def save(relative, source):
        original = tree / relative
        dest = output / relative.replace('/', '_')
        dest.write_text(source)
        replacements[str(original)] = str(dest)

    names = ('usage_ledger.go', 'usage_identity.go', 'usage_journal.go')
    for name in names:
        path = tree / 'internal/session' / name
        if not path.exists():
            continue
        s = path.read_text()
        s = re.sub(r'\b(\w+)\.Sync\(\)', r'fourCrewProbeSync(\1, false)', s)
        s = s.replace('filelock.Lock(', 'fourCrewProbeLock(')
        if name == 'usage_ledger.go':
            s = replace_one(s, 'func RecordUsage(path string, line UsageLine) {', 'func RecordUsage(path string, line UsageLine) {\n fourCrewProbe[0].Add(1)')
            s = s.replace('import (', 'import (\n "github.com/Agent-Field/codeaf/internal/filelock"', 1)
            s += '''
// These counters exist only in the generated diagnostic overlay.
var fourCrewProbe [6]atomic.Int64
func FourCrewProbeCounts() map[string]int64 {
 names := []string{"record_usage", "journal_append", "ledger_shared", "ledger_exclusive", "usage_fsync", "background_fsync"}
 out := map[string]int64{}
 for i, name := range names { out[name] = fourCrewProbe[i].Load() }
 return out
}
func fourCrewProbeLock(f *os.File, exclusive, nonblocking bool) error {
 err := filelock.Lock(f, exclusive, nonblocking)
 if err == nil { if exclusive { fourCrewProbe[3].Add(1) } else { fourCrewProbe[2].Add(1) } }
 return err
}
func fourCrewProbeSync(f *os.File, background bool) error {
 fourCrewProbe[4].Add(1)
 if background {
  fourCrewProbe[5].Add(1)
  if ms, _ := strconv.Atoi(os.Getenv("CODEAF_FOURCREW_SYNC_BLOCK_MS")); ms > 0 { time.Sleep(time.Duration(ms)*time.Millisecond) }
 }
 return f.Sync()
}
'''
        if name == 'usage_journal.go':
            s = replace_one(s, 'n, err = f.Write(row.line)', 'n, err = f.Write(row.line)\n if err == nil { fourCrewProbe[1].Add(1) }')
            # Only the syncBatches caller is delayed; recovery sync is untouched.
            s = replace_one(s, 'return p.syncJournal(f)', 'return fourCrewProbeSync(f, true)')
        save('internal/session/' + name, s)

    path = tree / 'internal/cellstore/wal.go'
    s = path.read_text()
    s = s.replace('import (', 'import (\n "sync/atomic"', 1)
    s = re.sub(r'\b(\w+)\.Sync\(\)', r'fourCrewWALSync(\1)', s)
    # The old base has no model-only WAL operation.
    anchor = 'if _, err := f.Write(append(line, \'\\n\')); err != nil {'
    if 'if r.Model != nil {' in s:
        end = s.index('\n\tif r.Model != nil {', s.index(anchor))
        s = s[:end] + '\n if r.Model != nil { fourCrewWALModels.Add(1) }' + s[end:]
    s += '''
var fourCrewWALModels, fourCrewWALSyncs, fourCrewStoreSyncs atomic.Int64
func FourCrewProbeCounts() map[string]int64 { return map[string]int64{"wal_model_write": fourCrewWALModels.Load(), "wal_fsync": fourCrewWALSyncs.Load(), "store_other_fsync": fourCrewStoreSyncs.Load()} }
func fourCrewWALSync(f *os.File) error { fourCrewWALSyncs.Add(1); return f.Sync() }
'''
    save('internal/cellstore/wal.go', s)

    for name in ('syncpath_unix.go', 'store_busy.go'):
        path = 'internal/cellstore/' + name
        source = (tree / path).read_text()
        source = re.sub(r'\b(\w+)\.Sync\(\)', r'fourCrewStoreSync(\1)', source)
        save(path, source)
    s = (output / 'internal_cellstore_wal.go').read_text()
    s += '\nfunc fourCrewStoreSync(f *os.File) error { fourCrewStoreSyncs.Add(1); return f.Sync() }\n'
    save('internal/cellstore/wal.go', s)

    path = 'cmd/codeaf/usage_fourcrew_live_e2e_test.go'
    s = (tree / path).read_text()
    s = s.replace('"context"', '"context"\n "github.com/Agent-Field/codeaf/internal/cellstore"', 1)
    s = replace_one(s, 'timing.report(t)', 'timing.report(t)\n t.Logf("FOURCREW_PROBE session=%v wal=%v", session.FourCrewProbeCounts(), cellstore.FourCrewProbeCounts())')
    report = 't.Logf("FOURCREW_PROBE phase=PHASE session=%v wal=%v", session.FourCrewProbeCounts(), cellstore.FourCrewProbeCounts())'
    s = s.replace('ctx, cancel := context.WithTimeout', report.replace('PHASE', 'ready') + '\n ctx, cancel := context.WithTimeout', 1)
    s = s.replace('wall := time.Since(started)', 'wall := time.Since(started)\n' + report.replace('PHASE', 'wall_done'), 1)
    save(path, s)
    (output / 'overlay.json').write_text(json.dumps({'Replace': replacements}, indent=2))
    print(output / 'overlay.json')


if __name__ == '__main__':
    generate(*sys.argv[1:])
