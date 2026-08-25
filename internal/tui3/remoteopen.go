package tui3

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/cas"
	"github.com/Agent-Field/aforge-v2/internal/filedoor"
	"github.com/Agent-Field/aforge-v2/internal/home"
)

// OPENING A FILE THAT IS ON ANOTHER MACHINE, WITH THIS MACHINE'S OWN VIEWER.
//
// remotefiles.go says which paths are real and hands out the door's URLs; this
// file is the other half of the same errand — the one where a person does not
// want a browser tab, they want Preview, or their editor, or whatever this
// laptop opens a `.csv` with. The three steps between those two wishes are
// content, a name, and a handoff.
//
// ── ONE: THE CACHE, KEYED BY CONTENT ────────────────────────────────────────
//
// [remote.FetchedFile] carries the sha256 of its own bytes, which is the digest
// internal/cas keys on — so "have I already got this" is one map lookup and one
// stat, and a file opened twice crosses the wire once. The digest is VERIFIED
// against the bytes rather than trusted: it arrived from another machine, and a
// content-addressed store whose keys are somebody else's arithmetic is not
// content-addressed. A hash the surface computed itself is what goes in.
//
// ── TWO: THE MIRROR, BECAUSE A VIEWER SHOWS ITS TITLE BAR ───────────────────
//
// A CAS blob is named by its digest, so handing one to `open` puts
// `3f9a…c17b` in somebody's window title and in their recent-files menu. The
// mirror is the same bytes under the name they actually have:
//
//	~/.aforge/v3/remote/mirror/<host>/<the engine's own path>
//
// It is HARDLINKED from the blob where the filesystem allows it, so the second
// copy costs an inode and no bytes, and copied where it does not (a store and a
// mirror on different devices, which is what happens the moment somebody points
// AFORGE_HOME at another disk). Either way the file is read-only, which is not
// an accident: THE MIRROR IS BYTES TO LOOK AT. Editing it changes nothing on the
// machine that owns the file, this wave has no write-back, and a read-only copy
// is the surface saying so in the one language every editor understands.
//
// ── THREE: THE HANDOFF, AND THE SILENCE AROUND IT ───────────────────────────
//
// The platform opener is opener.go's, unchanged and for its reason: a second
// spelling of "open this" is a second thing to keep in step with the platforms.
//
// THE EMPTINESS LAW OWNS THE WAIT. A fetch that finishes inside
// [remoteOpenQuiet] says NOTHING — a cache hit and a fast link are the same
// experience and neither is news. Past that, one quiet note names the file and
// its weight if the weight is known, and nothing else is drawn: no spinner, no
// progress, no second line when it lands. A failure is the ENGINE'S OWN
// SENTENCE, once — including the refusal for a file over the wire's 16MB
// ceiling, which is passed through exactly as internal/remote spells it, because
// a surface that paraphrased a limit would be a second place that states it.

// remoteOpenedMsg is one open flow finishing, either way.
type remoteOpenedMsg struct {
	target string
	err    error
}

// remoteOpenSlowMsg is the quiet note's own clock: it fires once, and says
// something only if the fetch it was started for is still out.
type remoteOpenSlowMsg struct{ target string }

// openRemotePath is the whole gesture: a path on the engine's disk becomes a
// file this machine's viewer is holding.
//
// It takes the name AS WRITTEN — relative to the engine's workspace, the way
// everything in a hosted conversation is — and resolves it the one way this
// surface is allowed to ([app.remoteTarget]).
func (a *app) openRemotePath(name string) tea.Cmd {
	r := a.rfiles
	if r == nil {
		a.note(filesLocalWord)
		return nil
	}
	target := a.remoteTarget(name)
	if target == "" {
		a.note(filesNotAPathWord)
		return nil
	}
	if r.opening[target] {
		// A second press while the first fetch is still out is a person who did
		// not see anything happen, which is exactly what the quiet window is for.
		// Two fetches of one file would be two copies of the same bytes and two
		// viewers of the same document.
		return nil
	}
	r.opening[target] = true
	return tea.Batch(
		func() tea.Msg { return remoteOpenedMsg{target: target, err: r.open(target)} },
		tea.Tick(remoteOpenQuiet, func(time.Time) tea.Msg { return remoteOpenSlowMsg{target: target} }),
		a.wake(),
	)
}

// open is the three steps, off the loop: the bytes, the mirror, the handoff.
func (r *remoteFiles) open(target string) error {
	blob, _, err := r.fetch(target)
	if err != nil {
		return err
	}
	mirror, err := r.mirror(target, blob)
	if err != nil {
		return err
	}
	if err := processOpener(mirror); err != nil {
		// THE PLATFORM'S SENTENCE IS NOT SHOWN, for deliverables.go's reason:
		// what comes back from the handoff is written about a browser, and a
		// person who asked for a file wants to know the file did not open.
		return errors.New(filesOpenFailedWord + path.Base(target))
	}
	return nil
}

// remoteOpened files the answer and says the one thing there is to say.
func (a *app) remoteOpened(msg remoteOpenedMsg) tea.Cmd {
	r := a.rfiles
	if r == nil {
		return nil
	}
	delete(r.opening, msg.target)
	if msg.err != nil {
		// THE ENGINE'S SENTENCE, UNCHANGED. A refusal that crossed the wire is
		// the far machine explaining its own law — the two roots, the 16MB
		// ceiling — and it is the only account of that law this side has.
		a.note(strings.TrimSpace(msg.err.Error()))
	}
	a.touch()
	return nil
}

// remoteOpenSlow is the quiet note, drawn only if the fetch is still out.
func (a *app) remoteOpenSlow(msg remoteOpenSlowMsg) tea.Cmd {
	r := a.rfiles
	if r == nil || !r.opening[msg.target] {
		return nil
	}
	said := fetchingWord + path.Base(msg.target)
	// The weight rides along when this surface already knows it — a listing or
	// a prefetch put it in the table — and is silent when it does not, which is
	// the emptiness law rather than a missing feature: a fetch is not worth a
	// round trip to find out how heavy it is before saying it is happening.
	if weight := byteWord(int(r.facts[msg.target].size)); weight != "" {
		said += " — " + weight
	}
	a.note(said)
	a.touch()
	return nil
}

// ── the bytes ───────────────────────────────────────────────────────────────

// fetch answers with one remote file, from the cache when the cache has it.
//
// IT IS CALLED FROM THE DOOR'S GOROUTINES AS WELL AS FROM THE LOOP, so it
// touches nothing but the wire and the mutex-guarded blob cache.
func (r *remoteFiles) fetch(target string) (remoteBlob, filedoor.File, error) {
	store, err := r.blobStore()
	if err != nil {
		// The cache is this machine's own state directory failing, which is not a
		// sentence about the file and not one written in anybody's vocabulary but
		// the filesystem's.
		return remoteBlob{}, filedoor.File{}, errors.New(filesCacheWord)
	}
	// THE CACHE IS CHECKED BEFORE THE WIRE and the blob is checked before it is
	// believed: a ref this surface wrote down is only as good as the object
	// still being there, which [cas.Store.Stat] answers without reading it.
	if blob, known := r.ref(target); known {
		if _, there, err := store.Stat(blob.ref); err == nil && there {
			if data, err := readBlob(store, blob.ref); err == nil {
				return blob, filedoor.File{Name: blob.name, MIME: blob.mime, Bytes: data}, nil
			}
		}
	}
	fetched, err := r.wire.FetchFile(target)
	if err != nil {
		return remoteBlob{}, filedoor.File{}, err
	}
	// THE DIGEST IS DERIVED HERE AND NOT TAKEN ON TRUST. The engine sends one
	// and it is worth having — a surface can compare — but the key an object is
	// stored under has to be a fact about the bytes in hand.
	sum := sha256.Sum256(fetched.Bytes)
	if want := strings.ToLower(strings.TrimSpace(fetched.Hash)); want != "" && want != hex.EncodeToString(sum[:]) {
		return remoteBlob{}, filedoor.File{}, fmt.Errorf("%s arrived damaged — the bytes do not match what the engine said it sent", path.Base(target))
	}
	ref, err := store.PutBytes(fetched.Bytes)
	if err != nil {
		return remoteBlob{}, filedoor.File{}, err
	}
	blob := remoteBlob{ref: ref, name: fetched.Name, mime: fetched.MIME}
	if blob.name == "" {
		blob.name = path.Base(target)
	}
	r.setRef(target, blob)
	return blob, filedoor.File{Name: blob.name, MIME: blob.mime, Bytes: fetched.Bytes}, nil
}

// readBlob reads one object out of the store whole. Everything that crosses this
// wire is already bounded by internal/remote's 16MB ceiling, so there is nothing
// here a stream would save.
func readBlob(store *cas.Store, ref cas.Ref) ([]byte, error) {
	reader, err := store.Get(ref)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	return io.ReadAll(reader)
}

// ── the mirror ──────────────────────────────────────────────────────────────

// mirror puts one cached blob at a path that names it, and answers with that
// path. See this file's header for why a viewer needs one.
func (r *remoteFiles) mirror(target string, blob remoteBlob) (string, error) {
	store, err := r.blobStore()
	if err != nil {
		return "", errors.New(filesCacheWord)
	}
	source, err := store.Path(blob.ref)
	if err != nil {
		return "", err
	}
	mirror := mirrorPath(r.host, target)
	if mirror == "" {
		return "", fmt.Errorf("%s cannot be mirrored on this machine", path.Base(target))
	}
	if err := os.MkdirAll(filepath.Dir(mirror), 0o700); err != nil {
		return "", fmt.Errorf("could not make room for %s here", path.Base(target))
	}
	// ALREADY THERE AND ALREADY THE SAME OBJECT is the common case on a second
	// open, and it is answered without touching the disk twice: a hardlink to
	// the blob IS the blob, so a mirror whose bytes are that object's is
	// finished. A mirror carrying older bytes — the far file changed, the digest
	// changed with it — is replaced rather than kept.
	if same, err := sameFile(source, mirror); err == nil && same {
		return mirror, nil
	}
	_ = os.Remove(mirror)
	if err := os.Link(source, mirror); err == nil {
		return mirror, nil
	}
	// A LINK ACROSS DEVICES IS NOT AN ERROR TO REPORT, it is the other case: the
	// store and the mirror are on different filesystems, which is what happens
	// the moment somebody points AFORGE_HOME somewhere else. Copy, and say
	// nothing about it — a person opening a file does not need to know which of
	// two ways their filesystem allowed.
	if err := copyFileReadOnly(source, mirror); err != nil {
		return "", fmt.Errorf("could not keep a copy of %s here", path.Base(target))
	}
	return mirror, nil
}

// mirrorPath is where one file from one machine lands, and it is the plan's own
// shape: ~/.aforge/v3/remote/mirror/<host>/<the engine's absolute path>.
//
// THE HOST IS A DIRECTORY NAME AND THE ENGINE'S PATH IS THE REST OF IT, which is
// what makes two machines with the same file layout two different trees here —
// and what makes `ls ~/.aforge/v3/remote/mirror` the answer to "whose files am I
// holding".
//
// It answers "" for anything it will not write. The path came off the wire, and
// A BOUNDARY THAT TRUSTS ITS INPUT IS NOT A BOUNDARY (internal/remote's
// attachmentName states the same rule from the other end): a `..` in it would be
// the far machine choosing a directory on this one.
func mirrorPath(host, target string) string {
	name := hostDirName(host)
	clean := path.Clean(strings.TrimSpace(target))
	if name == "" || !strings.HasPrefix(clean, "/") {
		return ""
	}
	for _, part := range strings.Split(strings.Trim(clean, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			return ""
		}
	}
	return home.Join("v3", "remote", "mirror", name,
		filepath.FromSlash(strings.TrimPrefix(clean, "/")))
}

// hostDirName is the machine's name as a directory. An ssh destination is
// allowed to carry a user, a colon and a port, and none of those is a thing to
// put in a path — so everything outside a plain name becomes a dash, which
// keeps two different destinations two different directories without letting
// either of them name a place.
func hostDirName(host string) string {
	var out strings.Builder
	for _, r := range strings.TrimSpace(host) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.' || r == '-' || r == '_':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	name := strings.Trim(out.String(), ".-")
	if name == "" || name == ".." {
		return ""
	}
	return name
}

// sameFile reports whether two paths are the same object on disk, which is what
// a hardlink makes them.
func sameFile(a, b string) (bool, error) {
	first, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	second, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(first, second), nil
}

// copyFileReadOnly is the fallback when a hardlink is not available. The mode is
// 0o400 for this file's header's reason: the mirror is bytes to look at.
func copyFileReadOnly(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	// Written under a temporary name and renamed, so a copy interrupted half way
	// never becomes a mirror somebody opens: the same law internal/cas keeps
	// about its own objects.
	temp, err := os.CreateTemp(filepath.Dir(to), ".mirror-*")
	if err != nil {
		return err
	}
	if _, err := io.Copy(temp, source); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	if err := os.Chmod(temp.Name(), 0o400); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	if err := os.Rename(temp.Name(), to); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	return nil
}

// The sentences this file says.
const (
	// fetchingWord opens the one quiet note a slow fetch draws.
	fetchingWord = "fetching "
	// filesLocalWord is the whole of what /files <path> means on a session that
	// is not on another machine. It names the gesture that IS the answer here,
	// because a refusal that leaves somebody with nothing is a refusal that made
	// them ask twice.
	filesLocalWord = "that form of /files is for a session on another machine — this one is local, so the paths in it are already yours to open"
	// filesNotAPathWord is a name this surface will not resolve against the far
	// workspace: an empty one, or one that climbs out of it.
	filesNotAPathWord = "that is not a path inside the workspace on that machine"
	// filesCacheWord is this machine's own state directory refusing to hold the
	// copy. It says WHERE rather than WHAT, because every way it can happen —
	// no room, no permission, a read-only home — is answered in the same place.
	filesCacheWord = "there is nowhere to keep a copy of it on this machine · check ~/.aforge"
)
