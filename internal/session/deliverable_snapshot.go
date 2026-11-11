package session

// deliverable_snapshot.go registers a live-view snapshot the desktop has
// ALREADY SAVED, and owns the one definition of a loopback address.
//
// THE ENGINE DOES NOT CAPTURE. Taking the picture needs the web view, which is
// the desktop's. What the engine does is refuse to vouch for anything it cannot
// check: the PNG must be a real file inside the session's own folders, the
// address must be a genuine loopback origin, and a process named as the source
// must be one this session ran.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SnapshotMaxBytes bounds a registered snapshot.
const SnapshotMaxBytes = 32 << 20

// snapshotURLMax bounds the address a snapshot records.
const snapshotURLMax = 2048

// LoopbackOrigin parses raw as an HTTP address on this machine and answers its
// ORIGIN (`http://localhost:3000`). Only `http`, only the literal hosts
// `localhost` and `127.0.0.1`, a bare `host:port` taken as http, a port of
// 1-65535 with no leading zero, and no userinfo. Anything else is false: this
// is a refusal of off-machine addresses, so it must not be generous.
func LoopbackOrigin(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > snapshotURLMax {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Opaque != "" {
		return "", false
	}
	host := parsed.Hostname()
	if host != "localhost" && host != "127.0.0.1" {
		return "", false
	}
	port := parsed.Port()
	if port == "" || port[0] == '0' {
		return "", false
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", false
	}
	return "http://" + host + ":" + port, true
}

// LoopbackURL is [LoopbackOrigin] that keeps the path and query of the page,
// for the address a snapshot was taken of. The fragment is dropped.
func LoopbackURL(raw string) (string, bool) {
	origin, ok := LoopbackOrigin(raw)
	if !ok {
		return "", false
	}
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, _ := url.Parse(raw)
	parsed.Fragment, parsed.RawFragment = "", ""
	out := origin + parsed.EscapedPath()
	if parsed.RawQuery != "" {
		out += "?" + parsed.RawQuery
	}
	if len(out) > snapshotURLMax {
		return "", false
	}
	return out, true
}

// Why a snapshot was refused.
var (
	ErrSnapshotFile = errors.New("that is not a saved picture of this conversation")
	ErrSnapshotURL  = errors.New("a snapshot can only be of a page served from this computer")
)

// Bounds of a registered picture's pixels. A snapshot is a screen: wide, never
// absurd. The header says them before any pixel is read.
const (
	snapshotMaxSide   = 16384
	snapshotMaxPixels = 100_000_000
)

// snapshotLanes serialize registrations OF ONE FILE: the question "is this file
// already registered" and the append that answers it are one step, so two
// callers cannot both find it absent. A file always takes the same lane, so the
// dedupe is exact; different files mostly take different lanes, and a wait for
// a lane is bounded by the registration's own deadline rather than queued
// behind every other search in the process.
var snapshotLanes = func() (lanes [64]chan struct{}) {
	for i := range lanes {
		lanes[i] = make(chan struct{}, 1)
	}
	return lanes
}()

// snapshotSearchMax is the whole time one registration may spend waiting for
// its lane and searching the history, across every page. It is one bound for
// the whole question, never one per page.
var snapshotSearchMax = 10 * time.Second

func snapshotLane(index, canonical string) chan struct{} {
	sum := sha256.Sum256([]byte(index + "\x00" + canonical))
	return snapshotLanes[int(sum[0])%len(snapshotLanes)]
}

// pngHeader reads the dimensions from the IHDR chunk that must open the file.
func pngHeader(head []byte) (width, height int, ok bool) {
	if len(head) < 24 || !bytes.HasPrefix(head, pngSignature) || binary.BigEndian.Uint32(head[8:12]) != 13 || string(head[12:16]) != "IHDR" {
		return 0, 0, false
	}
	w, h := int(binary.BigEndian.Uint32(head[16:20])), int(binary.BigEndian.Uint32(head[20:24]))
	if w < 1 || h < 1 || w > snapshotMaxSide || h > snapshotMaxSide || w*h > snapshotMaxPixels {
		return 0, 0, false
	}
	return w, h, true
}

// findSnapshot is the saved-by-you picture already registered for this exact
// file and content, over the whole session history. A read that cannot finish
// is an error, never "not there".
func findSnapshot(ctx context.Context, index, sessionID, canonical, sum string) (DeliverableRow, bool, error) {
	deadline, _ := ctx.Deadline()
	cursor := ""
	for {
		if ctx.Err() != nil {
			return DeliverableRow{}, false, ErrMediaBusy
		}
		scan, err := ListDeliverables(index, DeliverableQuery{Sessions: map[string]bool{sessionID: true}, Text: filepath.Base(canonical)},
			cursor, MediaPageMax, deadline, 0, nil)
		if err != nil {
			return DeliverableRow{}, false, err
		}
		for _, row := range scan.Rows {
			if row.Artifact.Path == canonical && row.Fact.Origin == DeliverOriginSavedByYou && row.Fact.File != nil && row.Fact.File.SHA256 == sum {
				return row, true, nil
			}
		}
		if scan.Next == "" {
			return DeliverableRow{}, false, nil
		}
		cursor = scan.Next
	}
}

// RegisterSnapshot records a PNG the host saved as an image deliverable made by
// the person. roots are the session's own folders; the file must be a regular
// file inside one, validated and hashed through ONE descriptor that is checked
// again afterwards. The same file and content registered again answers the
// deliverable it already is. The engine captures nothing.
//
// The search for an earlier registration has ONE deadline, ctx's or
// snapshotSearchMax whichever is sooner, and stops when ctx ends. A search that
// cannot finish is [ErrMediaBusy] and records nothing: an unknown answer is
// never taken as "not there", which would be a second row for one picture.
func RegisterSnapshot(ctx context.Context, index, sessionID, path, rawURL string, roots []string) (DeliverableRow, bool, error) {
	if index == "" || sessionID == "" {
		return DeliverableRow{}, false, ErrSnapshotFile
	}
	address, ok := LoopbackURL(rawURL)
	if !ok {
		return DeliverableRow{}, false, ErrSnapshotURL
	}
	named, err := os.Lstat(path)
	if err != nil || !named.Mode().IsRegular() {
		return DeliverableRow{}, false, ErrSnapshotFile
	}
	canonical := canonicalFile(path)
	if !filepath.IsAbs(canonical) || !withinAny(roots, canonical) {
		return DeliverableRow{}, false, ErrSnapshotFile
	}
	file, err := os.Open(canonical)
	if err != nil {
		return DeliverableRow{}, false, ErrSnapshotFile
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, named) || info.Size() > SnapshotMaxBytes || info.Size() < 24 {
		return DeliverableRow{}, false, ErrSnapshotFile
	}
	head := make([]byte, 64)
	if _, err := io.ReadFull(file, head); err != nil {
		return DeliverableRow{}, false, ErrSnapshotFile
	}
	width, height, ok := pngHeader(head)
	if !ok {
		return DeliverableRow{}, false, ErrSnapshotFile
	}
	hash := sha256.New()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return DeliverableRow{}, false, ErrSnapshotFile
	}
	if n, err := io.Copy(hash, io.LimitReader(file, SnapshotMaxBytes+1)); err != nil || n != info.Size() {
		return DeliverableRow{}, false, ErrSnapshotFile
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return DeliverableRow{}, false, ErrSnapshotFile
	}
	sum := hex.EncodeToString(hash.Sum(nil))

	ctx, cancel := context.WithTimeout(ctx, snapshotSearchMax)
	defer cancel()
	lane := snapshotLane(index, canonical)
	select {
	case lane <- struct{}{}:
		defer func() { <-lane }()
	case <-ctx.Done():
		return DeliverableRow{}, false, ErrMediaBusy
	}
	if row, found, err := findSnapshot(ctx, index, sessionID, canonical, sum); err != nil {
		return DeliverableRow{}, false, err
	} else if found {
		return row, false, nil
	}
	identity := identityOf(canonical, info)
	identity.SHA256, identity.MIME = sum, "image/png"
	media := MediaFact{
		Origin: MediaGenerated, MIME: "image/png", Width: width, Height: height, Canonical: canonical,
		Bytes: identity.Bytes, ModNS: identity.ModNS, CtimeNS: identity.CtimeNS, Dev: identity.Dev, Ino: identity.Ino, SHA256: sum,
	}
	fact := DeliverableFact{
		ID: NewDeliverableID(), At: time.Now().UTC(), From: []string{address},
		State: DeliverDraft, Origin: DeliverOriginSavedByYou, File: identity, Version: "sha256:" + sum,
	}
	artifact := Artifact{Path: canonical, Session: sessionID, Title: filepath.Base(canonical), Kind: "image", Created: time.Now(), Deliver: &fact}
	id, written := recordMediaRow(index, artifact, media)
	if id == "" || written.Deliver == nil {
		// Without its fact the row would read as a tool-made picture: not a snapshot.
		return DeliverableRow{}, false, fmt.Errorf("the snapshot could not be recorded")
	}
	row, ok := deliverableRowOf(written)
	if !ok {
		return DeliverableRow{}, false, ErrSnapshotFile
	}
	return row, true, nil
}
