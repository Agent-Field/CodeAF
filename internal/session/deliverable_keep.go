package session

// deliverable_keep.go is Keep: taking a deliverable OUT of the session that
// made it, on purpose, into a folder the person chose.
//
// It is /files ctrl+y's promotion (tui3/deliverables.go copyFileTo) made safe
// for a host to drive: the same refusal to overwrite, the same private mode,
// and a missing parent is still not created. What it adds is everything a
// remote caller needs the engine to hold for it:
//
//   - THE SOURCE IS THE ENGINE'S, never a caller's. The file is the one the
//     index recorded, checked live against the same scope law media uses, and
//     it is opened and compared to what was checked, so a swap between the
//     check and the open is a refusal and not a copy of somebody else's file.
//   - THE SOURCE IS NEVER TOUCHED. It is read once, streamed in bounded chunks,
//     and its identity is read again after the last byte; a file that moved
//     under the copy fails the copy.
//   - NOTHING IS OVERWRITTEN, ATOMICALLY. The bytes go to a private temporary
//     file in the destination directory and are PUBLISHED with a hard link,
//     which fails if the name exists. Where a filesystem cannot link, an
//     exclusive create takes its place and a failure removes what it made.
//   - THE INDEX SAYS KEPT ONLY AFTER A COMPLETE COPY. If the row cannot be
//     appended the copy this call made is removed, so there is no Keep the
//     index does not know about and no Kept the disk does not hold.
//
// WHO CHOSE THE DESTINATION is not something the engine can know. The contract
// (ENGINE-CONTRACT.md §7.3) is that only the native host calls this, with a
// directory its own picker returned.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// KeepMaxBytes is the largest file Keep copies.
const KeepMaxBytes = 512 << 20

// keepChunk is how much one read of the copy takes, and so how often a
// cancellation is noticed.
const keepChunk = 256 << 10

// Why a Keep was refused.
var (
	ErrKeepAlready  = errors.New("this deliverable is already kept")
	ErrKeepSource   = errors.New("the file is not there to keep")
	ErrKeepChanged  = errors.New("the file changed since you looked; look again")
	ErrKeepDest     = errors.New("that folder cannot be kept into")
	ErrKeepName     = errors.New("that is not a file name")
	ErrKeepTooLarge = errors.New("the file is too large to keep")
	ErrKeepRecord   = errors.New("the copy was not recorded, so it was removed")
	// ErrKeepUnconfirmed is a copy left in place because the index could not say
	// whether it had accepted the record.
	ErrKeepUnconfirmed = errors.New("the copy was made but the record could not be confirmed")
)

// KeepExistsError is a name already taken in the destination. Nothing was
// written; the person is asked for another name.
type KeepExistsError struct{ Path string }

func (e *KeepExistsError) Error() string { return e.Path + " already exists" }

// KeepRequest is what a host asks for.
type KeepRequest struct {
	// Dir is an absolute, existing directory the host's picker returned.
	Dir string
	// Name defaults to the file's own name; it is one path element.
	Name string
	// ExpectVersion is the live version the person saw (the "file:" form). It
	// is required when the file has changed since it was recorded, and when
	// given it must be exactly the file's version now.
	ExpectVersion string
	// Workspace is the owning conversation's checkout, set by the engine from
	// the conversation and never from a caller's words: the repository whose
	// task branches [ResolveDeliverableSource] asks about.
	Workspace string
}

// The seams the tests use to put a race or a failure exactly where it matters.
// Production leaves them as they are.
var (
	keepBeforeOpen   = func(path string) {}
	keepAfterPublish = func(final string) {}
	keepAppend       = appendDeliverableUpdate
)

// KeepDeliverable copies the deliverable out and records it kept. roots are
// the scope law's roots for the owning session and home is the engine's own
// state directory, which is never a destination.
func KeepDeliverable(ctx context.Context, index string, row DeliverableRow, roots []string, home string, req KeepRequest) (DeliverableRow, string, error) {
	release, err := deliverKeepAdmission.acquire(ctx, keepKey{index, row.Artifact.Session, row.ID})
	if err != nil {
		return row, "", err
	}
	defer release()

	// The newest row decides, read under the lock: two Keeps of one deliverable
	// cannot both pass.
	latest, err := FindDeliverable(index, row.Artifact.Session, row.ID, time.Now().Add(8*time.Second))
	if err != nil {
		return row, "", err
	}
	row = latest
	if row.Fact.State == DeliverKept {
		return row, "", ErrKeepAlready
	}
	// THE SOURCE IS THE ONE AUTHORITY'S, never a path's: a file only on its
	// task branch is not in the checkout to keep, and whatever sits at the
	// path it will have is not it ([ResolveDeliverableSource]).
	authority := ResolveDeliverableSource(ctx, row, roots, req.Workspace)
	live := authority.Live
	if authority.Undecided {
		return row, "", ErrMediaBusy
	}
	if authority.OnBranch() {
		return row, "", fmt.Errorf("%w (it is only on its task branch until that lands)", ErrKeepSource)
	}
	if (live.State != MediaAvailable && live.State != MediaChanged) || live.Path == "" {
		// No path is a file the authority would not vouch for at all (a
		// worker's checkout path holding other bytes): never copied.
		return row, "", fmt.Errorf("%w (%s)", ErrKeepSource, live.State)
	}
	if live.State == MediaChanged && req.ExpectVersion == "" {
		return row, "", ErrKeepChanged
	}

	dir, name, err := keepDestination(live.Path, home, req)
	if err != nil {
		return row, "", err
	}

	keepBeforeOpen(live.Path)
	source, err := os.Open(live.Path)
	if err != nil {
		return row, "", fmt.Errorf("%w (%v)", ErrKeepSource, err)
	}
	defer source.Close()
	before, err := source.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return row, "", ErrKeepSource
	}
	if named, err := os.Lstat(live.Path); err != nil || !os.SameFile(named, before) {
		return row, "", ErrKeepChanged
	}
	if before.Size() > KeepMaxBytes {
		return row, "", ErrKeepTooLarge
	}
	ctimeNS, dev, ino := fileIdentity(before)
	version := fileVersion(before.Size(), before.ModTime().UnixNano(), ctimeNS, dev, ino)
	// THE DESCRIPTOR WE COPY FROM MUST BE THE FILE THAT WAS CHECKED. The path
	// was resolved and then opened again; a file swapped in between has another
	// version, and it is refused rather than copied as "available".
	if version != live.Version || (req.ExpectVersion != "" && req.ExpectVersion != version) {
		return row, "", ErrKeepChanged
	}

	temp, err := os.CreateTemp(dir, ".codeaf-keep-*")
	if err != nil {
		return row, "", fmt.Errorf("%w (%v)", ErrKeepDest, err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath) // the published name is a second link; this one always goes
	_ = temp.Chmod(0o600)
	hash := sha256.New()
	copied, err := keepStream(ctx, io.MultiWriter(temp, hash), source, before.Size())
	if err == nil {
		err = temp.Sync()
	}
	// The copy is identified by the descriptor that wrote it: the inode that
	// holds exactly these bytes, and the only inode this call may ever remove.
	var tempInfo os.FileInfo
	if err == nil {
		if tempInfo, err = temp.Stat(); err == nil && tempInfo.Size() != before.Size() {
			err = ErrKeepChanged
		}
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return row, "", err
	}
	after, err := source.Stat()
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return row, "", ErrKeepChanged
	}
	if again, err := os.Lstat(live.Path); err != nil || !os.SameFile(again, before) {
		return row, "", ErrKeepChanged
	}

	// AND THE BYTES ARE THE ONES THE SOURCE VOUCHED FOR. Where the authority
	// knows the digest, a copy that hashes to anything else is not this
	// deliverable and is never published, whatever was swapped in since.
	sum := hex.EncodeToString(hash.Sum(nil))
	if authority.Expect != "" && live.State == MediaAvailable && sum != authority.Expect {
		return row, "", ErrKeepChanged
	}
	final := filepath.Join(dir, name)
	owned, err := keepPublish(tempPath, final, tempInfo)
	if err != nil {
		return row, "", err
	}
	// The temporary name goes now: removing the second link changes the file's
	// change time, so the identity is read AFTER, as the person will find it. A
	// final that is not the inode we published was replaced by someone else and
	// is NOT ours to judge or to remove.
	_ = os.Remove(tempPath)
	keepAfterPublish(final)
	finalInfo, err := os.Lstat(final)
	if err != nil || !os.SameFile(finalInfo, owned) {
		return row, "", fmt.Errorf("%w (the name was taken over after the copy was published)", ErrKeepDest)
	}
	if !finalInfo.Mode().IsRegular() || finalInfo.Size() != copied {
		keepRemoveOwned(final, owned)
		return row, "", fmt.Errorf("%w (the copy is not what was written)", ErrKeepDest)
	}
	updated, ok := keepAppend(index, row, func(f *DeliverableFact) {
		f.State = DeliverKept
		identity := identityOf(final, finalInfo)
		identity.SHA256 = sum
		f.Kept = &DeliverableKept{Path: final, At: time.Now().UTC(), Bytes: copied, SHA256: sum, How: KeptCopied, File: identity}
	})
	if ok {
		return updated, final, nil
	}
	return keepReconcile(index, row, final, owned)
}

// keepReconcile settles a Keep whose index append reported failure. A failed
// append is AMBIGUOUS - the row may have reached the disk - so the copy is
// removed only once the index has been read and shown not to hold it; if it
// does, the Keep succeeded; if the index cannot be read, the copy stays, named
// in the error, because data the index may have accepted is never deleted.
func keepReconcile(index string, row DeliverableRow, final string, owned os.FileInfo) (DeliverableRow, string, error) {
	latest, err := FindDeliverable(index, row.Artifact.Session, row.ID, time.Now().Add(8*time.Second))
	switch {
	case err != nil:
		return row, "", ErrKeepUnconfirmed
	case latest.Fact.State == DeliverKept && latest.Fact.Kept != nil && latest.Fact.Kept.Path == final:
		return latest, final, nil
	}
	if !keepRemoveOwned(final, owned) {
		// The record is absent and the copy could not be taken back, so a copy
		// with no record may be there: that is the unconfirmed outcome, never a
		// plain failure a caller would retry into a second copy.
		return row, "", ErrKeepUnconfirmed
	}
	return row, "", ErrKeepRecord
}

// keepRemoveOwned removes final only while it is still the inode this call
// made, and answers whether nothing this call made is left there.
func keepRemoveOwned(final string, owned os.FileInfo) bool {
	now, err := os.Lstat(final)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !os.SameFile(now, owned)) {
		return true
	}
	return err == nil && os.Remove(final) == nil
}

// keepDestination validates the host's folder and the name, and answers the
// resolved directory. The directory is resolved through any links FIRST and the
// checks are made on the resolved spelling.
func keepDestination(source, home string, req KeepRequest) (dir, name string, err error) {
	if !filepath.IsAbs(req.Dir) {
		return "", "", fmt.Errorf("%w (not an absolute folder)", ErrKeepDest)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(req.Dir))
	if err != nil {
		return "", "", fmt.Errorf("%w (%v)", ErrKeepDest, err)
	}
	if info, err := os.Stat(resolved); err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("%w (not a folder)", ErrKeepDest)
	}
	if strings.TrimSpace(home) != "" && withinAny([]string{home}, resolved) {
		return "", "", fmt.Errorf("%w (that is codeaf's own folder)", ErrKeepDest)
	}
	name = req.Name
	if name == "" {
		name = filepath.Base(source)
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") || len(name) > 255 {
		return "", "", ErrKeepName
	}
	return resolved, name, nil
}

// keepStream copies exactly size bytes in bounded chunks, noticing a
// cancellation between them. Fewer or more bytes than the stat said is a
// changed file.
func keepStream(ctx context.Context, to io.Writer, from io.Reader, size int64) (int64, error) {
	buf := make([]byte, keepChunk)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := from.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > size {
				return total, ErrKeepChanged
			}
			if _, werr := to.Write(buf[:n]); werr != nil {
				return total, werr
			}
		}
		if errors.Is(err, io.EOF) {
			if total != size {
				return total, ErrKeepChanged
			}
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

// keepPublish gives the finished temporary file its final name WITHOUT ever
// replacing one. A hard link is atomic and fails if the name exists; where the
// filesystem cannot link, an exclusive create and copy stands in and removes
// what it made if it fails. It answers the inode now published at final, as the
// descriptors that made it saw it - never as a look at the name.
func keepPublish(tempPath, final string, tempInfo os.FileInfo) (os.FileInfo, error) {
	err := os.Link(tempPath, final)
	switch {
	case err == nil:
		if now, lerr := os.Lstat(final); lerr != nil || !os.SameFile(now, tempInfo) {
			return nil, fmt.Errorf("%w (the name was taken over as it was published)", ErrKeepDest)
		}
		return tempInfo, nil
	case errors.Is(err, os.ErrExist):
		return nil, &KeepExistsError{Path: final}
	case errors.Is(err, syscall.EPERM), errors.Is(err, syscall.ENOTSUP), errors.Is(err, syscall.EOPNOTSUPP), errors.Is(err, syscall.EXDEV), errors.Is(err, syscall.EMLINK):
		return keepExclusiveCopy(tempPath, final)
	}
	return nil, fmt.Errorf("%w (%v)", ErrKeepDest, err)
}

func keepExclusiveCopy(tempPath, final string) (os.FileInfo, error) {
	from, err := os.Open(tempPath)
	if err != nil {
		return nil, err
	}
	defer from.Close()
	to, err := os.OpenFile(final, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, &KeepExistsError{Path: final}
		}
		return nil, fmt.Errorf("%w (%v)", ErrKeepDest, err)
	}
	info, statErr := to.Stat()
	fail := func(err error) (os.FileInfo, error) {
		_ = to.Close()
		if statErr == nil {
			keepRemoveOwned(final, info)
		}
		return nil, err
	}
	if statErr != nil {
		return fail(statErr)
	}
	if _, err := io.Copy(to, from); err != nil {
		return fail(err)
	}
	if err := to.Sync(); err != nil {
		return fail(err)
	}
	if err := to.Close(); err != nil {
		return fail(err)
	}
	return info, nil
}
