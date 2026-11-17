package cellsync

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math/bits"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
	"golang.org/x/crypto/chacha20poly1305"
)

const frameHintBudget = directory.FrameHintBudget

type openKeyer interface {
	OpenKeys() (cellKey, dedup []byte)
	OpenCache(c cell.Cell) string
}

// latestFrameHints uses actual final uploaded frame sizes, including one reserved
// EOF/oversize probe per frame. Larger uploads simply have no warm optimization.
func latestFrameHints(frames []FrameFile, ids []string) []directory.FrameHint {
	if len(frames) == 0 || len(frames) > directory.MaxFrameHints || len(frames) != len(ids) {
		return nil
	}
	out := make([]directory.FrameHint, 0, len(frames))
	remaining := frameHintBudget
	for i, f := range frames {
		if f.Bytes <= 0 || f.Bytes >= remaining || !blobstore.ValidRID(ids[i]) {
			return nil
		}
		remaining -= f.Bytes + 1
		out = append(out, directory.FrameHint{ID: ids[i], Bytes: f.Bytes})
	}
	return out
}

func validWarmHints(hints []directory.FrameHint) bool {
	if len(hints) == 0 || len(hints) > directory.MaxFrameHints {
		return false
	}
	remaining := frameHintBudget
	seen := map[string]bool{}
	for _, hint := range hints {
		if hint.Bytes <= 0 || hint.Bytes >= remaining || !blobstore.ValidRID(hint.ID) || seen[hint.ID] {
			return false
		}
		remaining -= hint.Bytes + 1
		seen[hint.ID] = true
	}
	return true
}

func (f *Fetcher) primeWarm(ctx context.Context, c cell.Cell, head, inbox string) error {
	want, err := f.Engine.Want(ctx, c, head)
	if err != nil {
		return err
	}
	if len(want) == 0 {
		return nil
	}
	view, err := f.cellView(ctx, c.ID)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || view.Cell.Head != head || !validWarmHints(view.Cell.FrameHints) {
		return nil
	}
	for _, hint := range view.Cell.FrameHints {
		raw, err := blobstore.ReadFrameBounded(ctx, f.Store, hint.ID, hint.Bytes)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil
		}
		_, objects, err := blobstore.Decode(raw)
		if err != nil {
			return nil
		}
		if err := keep(inbox, objects); err != nil {
			return err
		}
	}
	_, err = f.Engine.ImportPrimed(ctx, c, head, inbox)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		clearInbox(inbox)
	}
	return nil
}

// ensureHintInbox gives every attempt an exclusive directory. No attempt can
// clear another fetcher's primed files while the native import reads them.
func ensureHintInbox(parent string) (string, error) { return os.MkdirTemp(parent, ".frame-prime-") }

var blake3Perm = [16]int{2, 6, 3, 10, 7, 0, 4, 13, 1, 11, 12, 5, 9, 14, 15, 8}

func blake3G(v *[16]uint32, a, b, c, d int, mx, my uint32) {
	v[a] = v[a] + v[b] + mx
	v[d] = bits.RotateLeft32(v[d]^v[a], -16)
	v[c] = v[c] + v[d]
	v[b] = bits.RotateLeft32(v[b]^v[c], -12)
	v[a] = v[a] + v[b] + my
	v[d] = bits.RotateLeft32(v[d]^v[a], -8)
	v[c] = v[c] + v[d]
	v[b] = bits.RotateLeft32(v[b]^v[c], -7)
}

func blake3Keyed(key [32]byte, domain string, id [32]byte) [32]byte {
	var k [8]uint32
	for i := range 8 {
		k[i] = binary.LittleEndian.Uint32(key[4*i : 4*i+4])
	}
	var block [64]byte
	n := copy(block[:], domain)
	n += copy(block[n:], id[:])
	var m [16]uint32
	for i := range 16 {
		m[i] = binary.LittleEndian.Uint32(block[4*i : 4*i+4])
	}
	v := [16]uint32{
		k[0], k[1], k[2], k[3], k[4], k[5], k[6], k[7],
		0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A,
		0, 0, uint32(n), 0x1b,
	}
	for r := range 7 {
		blake3G(&v, 0, 4, 8, 12, m[0], m[1])
		blake3G(&v, 1, 5, 9, 13, m[2], m[3])
		blake3G(&v, 2, 6, 10, 14, m[4], m[5])
		blake3G(&v, 3, 7, 11, 15, m[6], m[7])
		blake3G(&v, 0, 5, 10, 15, m[8], m[9])
		blake3G(&v, 1, 6, 11, 12, m[10], m[11])
		blake3G(&v, 2, 7, 8, 13, m[12], m[13])
		blake3G(&v, 3, 4, 9, 14, m[14], m[15])
		if r < 6 {
			var next [16]uint32
			for i := range 16 {
				next[i] = m[blake3Perm[i]]
			}
			m = next
		}
	}
	var out [32]byte
	for i := range 8 {
		binary.LittleEndian.PutUint32(out[4*i:4*i+4], v[i]^v[i+8])
	}
	return out
}

func metaRID(cellKey, oid [32]byte) string {
	sum := blake3Keyed(cellKey, "furrow:remote-id:v1\x00", oid)
	return hex.EncodeToString(sum[:])
}

func chunkRID(dedup, cid [32]byte) string {
	sum := blake3Keyed(dedup, "codeaf:chunk-rid:v1\x00", cid)
	return hex.EncodeToString(sum[:])
}

type metaObj struct {
	rid  string
	kind byte
	oid  [32]byte
	body []byte
	raw  blobstore.Object
}

func decryptMetaObject(cellKey [32]byte, rid string, raw []byte) (metaObj, bool) {
	if len(raw) < 5+24+16+33 || string(raw[:5]) != "AGEO\x01" {
		return metaObj{}, false
	}
	aad, err := hex.DecodeString(rid)
	if err != nil || len(aad) != 32 {
		return metaObj{}, false
	}
	aead, err := chacha20poly1305.NewX(cellKey[:])
	if err != nil {
		return metaObj{}, false
	}
	plain, err := aead.Open(nil, raw[5:29], raw[29:], aad)
	if err != nil || len(plain) < 33 {
		return metaObj{}, false
	}
	var oid [32]byte
	copy(oid[:], plain[1:33])
	return metaObj{
		rid:  rid,
		kind: plain[0],
		oid:  oid,
		body: plain[33:],
		raw:  blobstore.Object{RID: rid, Bytes: raw},
	}, true
}

func decryptChunkObject(dedup, cid [32]byte, rid string, raw []byte) ([]byte, bool) {
	if len(raw) < 5+24+16+33 || string(raw[:5]) != "AGEO\x01" {
		return nil, false
	}
	aad, err := hex.DecodeString(rid)
	if err != nil || len(aad) != 32 {
		return nil, false
	}
	cKey := blake3Keyed(dedup, "codeaf:chunk-key:v1\x00", cid)
	aead, err := chacha20poly1305.NewX(cKey[:])
	if err != nil {
		return nil, false
	}
	plain, err := aead.Open(nil, raw[5:29], raw[29:], aad)
	if err != nil || len(plain) < 33 || plain[0] != 1 {
		return nil, false
	}
	var gotID [32]byte
	copy(gotID[:], plain[1:33])
	if gotID != cid {
		return nil, false
	}
	return plain[33:], true
}

type rawBytes []byte

func (r *rawBytes) UnmarshalJSON(data []byte) error {
	var nums []uint16
	if err := json.Unmarshal(data, &nums); err != nil {
		return err
	}
	out := make([]byte, len(nums))
	for i, v := range nums {
		out[i] = byte(v)
	}
	*r = out
	return nil
}

func (r rawBytes) id32() ([32]byte, bool) {
	var id [32]byte
	if len(r) != 32 {
		return id, false
	}
	copy(id[:], r)
	return id, true
}

type furrowSnapshot struct {
	RootTree rawBytes `json:"root_tree"`
	Label    string   `json:"label"`
}

type furrowDir struct {
	Entries []furrowDirEntry `json:"entries"`
	Pages   []furrowDirPage  `json:"pages"`
}

type furrowDirEntry struct {
	Name   rawBytes `json:"name"`
	Kind   string   `json:"kind"`
	Target rawBytes `json:"target"`
	Mode   uint32   `json:"mode"`
}

type furrowDirPage struct {
	FirstName rawBytes `json:"first_name"`
	LastName  rawBytes `json:"last_name"`
	Target    rawBytes `json:"target"`
}

type furrowBlob struct {
	Chunks   []furrowChunkRef `json:"chunks"`
	TotalLen uint64           `json:"total_len"`
}

type furrowChunkRef struct {
	ID  rawBytes `json:"id"`
	Len uint64   `json:"len"`
}

const (
	openSmallFileLimit  = 32 << 10
	openCarryBytesLimit = 64 << 10
)

func parseKeys(eng Engine, c cell.Cell) (cellKey, dedup [32]byte, cacheDir string, ok bool) {
	k, has := eng.(openKeyer)
	if !has {
		return cellKey, dedup, "", false
	}
	ck, dk := k.OpenKeys()
	if len(ck) != 32 || len(dk) != 32 {
		return cellKey, dedup, "", false
	}
	copy(cellKey[:], ck)
	copy(dedup[:], dk)
	return cellKey, dedup, k.OpenCache(c), true
}

func parseHexOID(h string) ([32]byte, bool) {
	var id [32]byte
	raw, err := hex.DecodeString(strings.TrimSpace(h))
	if err != nil || len(raw) != 32 {
		return id, false
	}
	copy(id[:], raw)
	return id, true
}

func loadObjectsFromDir(dir string, cellKey [32]byte, rawByRID map[string]blobstore.Object, metaByOID map[[32]byte]metaObj) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !blobstore.ValidRID(name) {
			continue
		}
		if _, exists := rawByRID[name]; exists {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(b) == 0 {
			continue
		}
		rawByRID[name] = blobstore.Object{RID: name, Bytes: b}
		if m, ok := decryptMetaObject(cellKey, name, b); ok {
			metaByOID[m.oid] = m
		}
	}
}

func decodeDirEntries(dirOID [32]byte, metaByOID map[[32]byte]metaObj) ([]furrowDirEntry, [][32]byte, bool) {
	m, ok := metaByOID[dirOID]
	if !ok || m.kind != 3 {
		return nil, [][32]byte{dirOID}, false
	}
	var d furrowDir
	if json.Unmarshal(m.body, &d) != nil {
		return nil, nil, false
	}
	if len(d.Pages) == 0 {
		return d.Entries, nil, true
	}
	var entries []furrowDirEntry
	var missing [][32]byte
	for _, p := range d.Pages {
		pid, ok := p.Target.id32()
		if !ok {
			continue
		}
		subEntries, subMissing, subOK := decodeDirEntries(pid, metaByOID)
		if !subOK {
			missing = append(missing, subMissing...)
			continue
		}
		entries = append(entries, subEntries...)
	}
	if len(missing) > 0 {
		return nil, missing, false
	}
	return entries, nil, true
}

func findDirEntryInPages(dirOID [32]byte, targetName string, metaByOID map[[32]byte]metaObj) (furrowDirEntry, [][32]byte, bool) {
	m, ok := metaByOID[dirOID]
	if !ok || m.kind != 3 {
		return furrowDirEntry{}, [][32]byte{dirOID}, false
	}
	var d furrowDir
	if json.Unmarshal(m.body, &d) != nil {
		return furrowDirEntry{}, nil, false
	}
	if len(d.Pages) > 0 {
		tBytes := []byte(targetName)
		for _, p := range d.Pages {
			if bytes.Compare(p.FirstName, tBytes) <= 0 && bytes.Compare(tBytes, p.LastName) <= 0 {
				pid, ok := p.Target.id32()
				if !ok {
					return furrowDirEntry{}, nil, false
				}
				return findDirEntryInPages(pid, targetName, metaByOID)
			}
		}
		return furrowDirEntry{}, nil, false
	}
	for _, e := range d.Entries {
		if string(e.Name) == targetName {
			return e, nil, true
		}
	}
	return furrowDirEntry{}, nil, false
}

// cacheOpenObjects caches small open-state objects (.cell metadata, env, and
// workspace-carrier manifest) from frames so later publishes can include them
// in the head frame without mutating outbox files on disk (R2).
func cacheOpenObjects(eng Engine, c cell.Cell, head string, frames []FrameFile) {
	if len(frames) == 0 {
		return
	}
	cellKey, dedup, cacheDir, ok := parseKeys(eng, c)
	if !ok || cacheDir == "" {
		return
	}
	headOID, ok := parseHexOID(head)
	if !ok {
		return
	}
	rawByRID := map[string]blobstore.Object{}
	metaByOID := map[[32]byte]metaObj{}
	for _, ff := range frames {
		frameBytes, err := os.ReadFile(ff.Path)
		if err != nil {
			return
		}
		_, objs, err := blobstore.Decode(frameBytes)
		if err != nil {
			return
		}
		for _, o := range objs {
			rawByRID[o.RID] = o
			if m, mOK := decryptMetaObject(cellKey, o.RID, o.Bytes); mOK {
				metaByOID[m.oid] = m
			}
		}
	}
	loadObjectsFromDir(cacheDir, cellKey, rawByRID, metaByOID)
	wantedRIDs := openStateRIDs(cellKey, dedup, headOID, metaByOID)
	if len(wantedRIDs) == 0 {
		return
	}
	_ = os.MkdirAll(cacheDir, 0o700)
	for _, rid := range wantedRIDs {
		if o, ok := rawByRID[rid]; ok {
			_ = writeWhole(cacheDir, o)
		}
	}
}

// appendOpenObjects returns frame with any cached open-state objects for head
// that were sealed in an earlier turn appended in memory, leaving the outbox
// file on disk untouched.
func appendOpenObjects(eng Engine, c cell.Cell, head string, frame []byte) []byte {
	cellKey, dedup, cacheDir, ok := parseKeys(eng, c)
	if !ok || cacheDir == "" {
		return frame
	}
	headOID, ok := parseHexOID(head)
	if !ok {
		return frame
	}
	hdr, lastObjects, err := blobstore.Decode(frame)
	if err != nil || hdr.CellKeyID == "" {
		return frame
	}
	rawByRID := map[string]blobstore.Object{}
	metaByOID := map[[32]byte]metaObj{}
	inLast := map[string]bool{}
	for _, o := range lastObjects {
		rawByRID[o.RID] = o
		inLast[o.RID] = true
		if m, mOK := decryptMetaObject(cellKey, o.RID, o.Bytes); mOK {
			metaByOID[m.oid] = m
		}
	}
	loadObjectsFromDir(cacheDir, cellKey, rawByRID, metaByOID)
	wantedRIDs := openStateRIDs(cellKey, dedup, headOID, metaByOID)
	var addedBytes int64
	var added int
	for _, rid := range wantedRIDs {
		if inLast[rid] {
			continue
		}
		o, ok := rawByRID[rid]
		if !ok {
			continue
		}
		if addedBytes+int64(len(o.Bytes)) > openCarryBytesLimit {
			break
		}
		lastObjects = append(lastObjects, o)
		inLast[rid] = true
		addedBytes += int64(len(o.Bytes))
		added++
	}
	if added == 0 {
		return frame
	}
	encoded, err := blobstore.Encode(hdr.CellKeyID, lastObjects)
	if err != nil {
		return frame
	}
	return encoded
}

func openStateRIDs(cellKey, dedup, headOID [32]byte, metaByOID map[[32]byte]metaObj) []string {
	snapObj, ok := metaByOID[headOID]
	if !ok || snapObj.kind != 4 {
		return nil
	}
	var snap furrowSnapshot
	if json.Unmarshal(snapObj.body, &snap) != nil {
		return nil
	}
	rootOID, ok := snap.RootTree.id32()
	if !ok {
		return nil
	}
	cellEntry, _, ok := findDirEntryInPages(rootOID, cell.StateDir, metaByOID)
	if !ok || cellEntry.Kind != "Directory" {
		return nil
	}
	cellDirOID, ok := cellEntry.Target.id32()
	if !ok {
		return nil
	}
	cellEntries, _, ok := decodeDirEntries(cellDirOID, metaByOID)
	if !ok {
		return nil
	}

	var wantedRIDs []string
	seenWanted := map[string]bool{}
	addWanted := func(rid string) {
		if rid != "" && !seenWanted[rid] {
			seenWanted[rid] = true
			wantedRIDs = append(wantedRIDs, rid)
		}
	}
	addBlob := func(blobOID [32]byte) {
		addWanted(metaRID(cellKey, blobOID))
		bm, ok := metaByOID[blobOID]
		if !ok || bm.kind != 2 {
			return
		}
		var b furrowBlob
		if json.Unmarshal(bm.body, &b) != nil || b.TotalLen > openSmallFileLimit {
			return
		}
		for _, ch := range b.Chunks {
			if cid, ok := ch.ID.id32(); ok {
				addWanted(chunkRID(dedup, cid))
			}
		}
	}

	addWanted(snapObj.rid)
	addWanted(metaRID(cellKey, rootOID))
	addWanted(metaRID(cellKey, cellDirOID))

	for _, e := range cellEntries {
		name := string(e.Name)
		oid, ok := e.Target.id32()
		if !ok {
			continue
		}
		switch e.Kind {
		case "File":
			addBlob(oid)
		case "Directory":
			if name != "env" && name != "workspace-carrier" {
				continue
			}
			addWanted(metaRID(cellKey, oid))
			subEntries, _, subOK := decodeDirEntries(oid, metaByOID)
			if !subOK {
				continue
			}
			for _, se := range subEntries {
				if se.Kind != "File" {
					continue
				}
				if name == "workspace-carrier" && string(se.Name) != "manifest.json" {
					continue
				}
				if soid, ok := se.Target.id32(); ok {
					addBlob(soid)
				}
			}
		}
	}
	return wantedRIDs
}

type openFileSpec struct {
	relPath string
	blobOID [32]byte
	mode    os.FileMode
}

func (f *Fetcher) stageOpenFromInbox(ctx context.Context, c cell.Cell, head, stageInbox, mainInbox string) (bool, error) {
	cellKey, dedup, openCache, ok := parseKeys(f.Engine, c)
	if !ok {
		return false, nil
	}
	headOID, ok := parseHexOID(head)
	if !ok {
		return false, nil
	}
	rawByRID := map[string]blobstore.Object{}
	metaByOID := map[[32]byte]metaObj{}
	loadObjectsFromDir(stageInbox, cellKey, rawByRID, metaByOID)
	if openCache != "" {
		loadObjectsFromDir(openCache, cellKey, rawByRID, metaByOID)
	}

	fetchMissingMeta := func(oids [][32]byte) error {
		var rids []string
		seen := map[string]bool{}
		for _, oid := range oids {
			if _, have := metaByOID[oid]; have {
				continue
			}
			rid := metaRID(cellKey, oid)
			if !seen[rid] {
				seen[rid] = true
				rids = append(rids, rid)
			}
		}
		if len(rids) == 0 {
			return nil
		}
		f.addOpenRound(1)
		if err := f.gather(ctx, stageInbox, rids); err != nil {
			return err
		}
		loadObjectsFromDir(stageInbox, cellKey, rawByRID, metaByOID)
		return nil
	}

	if _, ok := metaByOID[headOID]; !ok {
		if err := fetchMissingMeta([][32]byte{headOID}); err != nil {
			return false, err
		}
	}
	snapObj, ok := metaByOID[headOID]
	if !ok || snapObj.kind != 4 {
		return false, nil
	}
	var snap furrowSnapshot
	if json.Unmarshal(snapObj.body, &snap) != nil {
		return false, nil
	}
	rootOID, ok := snap.RootTree.id32()
	if !ok {
		return false, nil
	}
	if _, ok := metaByOID[rootOID]; !ok {
		if err := fetchMissingMeta([][32]byte{rootOID}); err != nil {
			return false, err
		}
	}
	cellEntry, missingPages, ok := findDirEntryInPages(rootOID, cell.StateDir, metaByOID)
	if !ok && len(missingPages) > 0 {
		if err := fetchMissingMeta(missingPages); err != nil {
			return false, err
		}
		cellEntry, _, ok = findDirEntryInPages(rootOID, cell.StateDir, metaByOID)
	}
	if !ok || cellEntry.Kind != "Directory" {
		return false, nil
	}
	cellDirOID, ok := cellEntry.Target.id32()
	if !ok {
		return false, nil
	}
	if _, ok := metaByOID[cellDirOID]; !ok {
		if err := fetchMissingMeta([][32]byte{cellDirOID}); err != nil {
			return false, err
		}
	}
	cellEntries, missingCellPages, ok := decodeDirEntries(cellDirOID, metaByOID)
	if !ok && len(missingCellPages) > 0 {
		if err := fetchMissingMeta(missingCellPages); err != nil {
			return false, err
		}
		cellEntries, _, ok = decodeDirEntries(cellDirOID, metaByOID)
	}
	if !ok {
		return false, nil
	}

	var subDirOIDs [][32]byte
	var envDirOID, carrierDirOID, receiptsDirOID [32]byte
	var hasEnvDir, hasCarrierDir, hasReceiptsDir bool
	var files []openFileSpec

	for _, e := range cellEntries {
		name := string(e.Name)
		oid, ok := e.Target.id32()
		if !ok {
			continue
		}
		switch e.Kind {
		case "File":
			files = append(files, openFileSpec{
				relPath: filepath.Join(cell.StateDir, name),
				blobOID: oid,
				mode:    fileModeOf(e.Mode),
			})
		case "Directory":
			switch name {
			case "env":
				envDirOID, hasEnvDir = oid, true
				subDirOIDs = append(subDirOIDs, oid)
			case "workspace-carrier":
				carrierDirOID, hasCarrierDir = oid, true
				subDirOIDs = append(subDirOIDs, oid)
			case "receipts":
				receiptsDirOID, hasReceiptsDir = oid, true
			}
		}
	}

	if len(subDirOIDs) > 0 {
		if err := fetchMissingMeta(subDirOIDs); err != nil {
			return false, err
		}
	}
	if hasEnvDir {
		envEntries, missingEnv, envOK := decodeDirEntries(envDirOID, metaByOID)
		if !envOK && len(missingEnv) > 0 {
			if err := fetchMissingMeta(missingEnv); err != nil {
				return false, err
			}
			envEntries, _, envOK = decodeDirEntries(envDirOID, metaByOID)
		}
		if envOK {
			for _, se := range envEntries {
				if se.Kind == "File" {
					if soid, ok := se.Target.id32(); ok {
						files = append(files, openFileSpec{
							relPath: filepath.Join(cell.StateDir, "env", string(se.Name)),
							blobOID: soid,
							mode:    fileModeOf(se.Mode),
						})
					}
				}
			}
		}
	}
	if hasCarrierDir {
		if me, _, mOK := findDirEntryInPages(carrierDirOID, "manifest.json", metaByOID); mOK && me.Kind == "File" {
			if soid, ok := me.Target.id32(); ok {
				files = append(files, openFileSpec{
					relPath: filepath.Join(cell.StateDir, "workspace-carrier", "manifest.json"),
					blobOID: soid,
					mode:    fileModeOf(me.Mode),
				})
			}
		}
	}
	if hasReceiptsDir && snap.Label != "" {
		if re, _, rOK := findDirEntryInPages(receiptsDirOID, snap.Label+".json", metaByOID); rOK && re.Kind == "File" {
			if soid, ok := re.Target.id32(); ok {
				files = append(files, openFileSpec{
					relPath: filepath.Join(cell.StateDir, "receipts", snap.Label+".json"),
					blobOID: soid,
					mode:    fileModeOf(re.Mode),
				})
			}
		}
	}

	var blobOIDs [][32]byte
	for _, spec := range files {
		blobOIDs = append(blobOIDs, spec.blobOID)
	}
	if err := fetchMissingMeta(blobOIDs); err != nil {
		return false, err
	}

	blobs := make([]furrowBlob, len(files))
	var missingChunks []string
	seenChunk := map[string]bool{}
	for i, spec := range files {
		bm, ok := metaByOID[spec.blobOID]
		if !ok || bm.kind != 2 {
			return false, nil
		}
		if json.Unmarshal(bm.body, &blobs[i]) != nil {
			return false, nil
		}
		for _, ch := range blobs[i].Chunks {
			cid, ok := ch.ID.id32()
			if !ok {
				return false, nil
			}
			rid := chunkRID(dedup, cid)
			if _, have := rawByRID[rid]; !have && !seenChunk[rid] {
				seenChunk[rid] = true
				missingChunks = append(missingChunks, rid)
			}
		}
	}
	if len(missingChunks) > 0 {
		f.addOpenRound(1)
		if err := f.gather(ctx, stageInbox, missingChunks); err != nil {
			return false, err
		}
		loadObjectsFromDir(stageInbox, cellKey, rawByRID, metaByOID)
	}

	if openCache != "" {
		_ = os.MkdirAll(openCache, 0o700)
	}
	for i, spec := range files {
		var buf bytes.Buffer
		buf.Grow(int(blobs[i].TotalLen))
		if openCache != "" {
			if bm, ok := metaByOID[spec.blobOID]; ok {
				_ = writeWhole(openCache, bm.raw)
			}
		}
		for _, ch := range blobs[i].Chunks {
			cid, _ := ch.ID.id32()
			rid := chunkRID(dedup, cid)
			ro, ok := rawByRID[rid]
			if !ok {
				return false, nil
			}
			plain, ok := decryptChunkObject(dedup, cid, rid, ro.Bytes)
			if !ok || uint64(len(plain)) != ch.Len {
				return false, nil
			}
			buf.Write(plain)
			if openCache != "" && blobs[i].TotalLen <= openSmallFileLimit {
				_ = writeWhole(openCache, ro)
			}
		}
		if uint64(buf.Len()) != blobs[i].TotalLen {
			return false, nil
		}
		dst := filepath.Join(c.Root, spec.relPath)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return false, err
		}
		_ = os.Remove(dst)
		if err := os.WriteFile(dst, buf.Bytes(), spec.mode); err != nil {
			return false, err
		}
	}
	if _, err := os.Stat(filepath.Join(c.Root, filepath.FromSlash(cell.MetaPath))); err != nil {
		return false, nil
	}
	return true, nil
}

func fileModeOf(m uint32) os.FileMode {
	perm := os.FileMode(m) & 0o777
	if perm == 0 {
		return 0o600
	}
	return perm
}

func markEngineFrames(eng Engine, c cell.Cell, frames []string) {
	if _, _, cacheDir, ok := parseKeys(eng, c); ok && cacheDir != "" {
		mainInbox := strings.TrimSuffix(cacheDir, ".open")
		markSeenFrames(mainInbox, frames)
	}
}

func markSeenFrames(inbox string, frames []string) {
	if inbox == "" || len(frames) == 0 {
		return
	}
	seen, _ := readSeenFrames(inbox)
	var b strings.Builder
	for id := range seen {
		b.WriteString(id)
		b.WriteByte('\n')
	}
	for _, id := range frames {
		if id != "" && !seen[id] {
			seen[id] = true
			b.WriteString(id)
			b.WriteByte('\n')
		}
	}
	_ = os.WriteFile(inbox+".frames-seen", []byte(b.String()), 0o600)
}

func readSeenFrames(inboxes ...string) (seen map[string]bool, hasPersistent bool) {
	seen = map[string]bool{}
	for idx, inbox := range inboxes {
		if inbox == "" {
			continue
		}
		raw, err := os.ReadFile(inbox + ".frames-seen")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				seen[line] = true
				if idx == 0 {
					hasPersistent = true
				}
			}
		}
	}
	return seen, hasPersistent
}
