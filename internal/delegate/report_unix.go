//go:build linux || darwin

package delegate

import (
	"context"
	"encoding/json"
	"errors"

	"golang.org/x/sys/unix"
)

// reportOpenFlags open a file inside the record folder: never through a link,
// never waiting on a pipe's writer, never taking a terminal as the process's.
const reportOpenFlags = unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_NOCTTY | unix.O_CLOEXEC

// reportFolder is the record folder, opened once. Every later step (the
// record, the report, naming either again) is relative to this one
// descriptor, so a folder swapped for another after it was opened is never
// read from, and the record that authorises a read and the file it reads are
// always in the same folder.
type reportFolder struct {
	fd   int
	dir  string
	held unix.Stat_t
	// read is the report as its descriptor said it was when its bytes had been
	// read, which [reportFolder.still] holds the name to afterwards.
	read unix.Stat_t
}

// openReportFolder opens dir as a folder, and refuses a link there IN THE SAME
// CALL (O_NOFOLLOW on the last component), so there is no moment between
// looking at the path and opening it. Ancestors of dir are the store's own
// layout and may legitimately sit behind a link; they are not judged.
func openReportFolder(dir string) (*reportFolder, error) {
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	switch {
	case err == nil:
	case errors.Is(err, unix.ENOENT):
		return nil, ErrReportMissing
	case errors.Is(err, unix.ELOOP), errors.Is(err, unix.ENOTDIR):
		return nil, ErrReportUnsafe
	default:
		return nil, ErrReportUnreadable
	}
	folder := &reportFolder{fd: fd, dir: dir}
	if unix.Fstat(fd, &folder.held) != nil {
		unix.Close(fd)
		return nil, ErrReportUnreadable
	}
	if folder.held.Mode&unix.S_IFMT != unix.S_IFDIR {
		unix.Close(fd)
		return nil, ErrReportUnsafe
	}
	return folder, nil
}

func (f *reportFolder) close() { unix.Close(f.fd) }

// record reads the folder's program record the way every reader of it is
// bounded ([ProgramRecordCap]), through the folder's descriptor. A record that
// is not there, is over the bound or does not parse is no record.
func (f *reportFolder) record(ctx context.Context) (ProgramRecord, error) {
	data, _, err := f.readRegular(ctx, ProgramFile, -1, ProgramRecordCap)
	switch {
	case errors.Is(err, ErrReportMissing), errors.Is(err, ErrReportTooLarge):
		return ProgramRecord{}, ErrReportNoRecord
	case err != nil:
		return ProgramRecord{}, err
	}
	var record ProgramRecord
	if json.Unmarshal(data, &record) != nil || record.Name == "" {
		return ProgramRecord{}, ErrReportNoRecord
	}
	return record, nil
}

// read reads the report name, which must hold exactly want bytes (its
// receipt's count): a file of another size is not the run's report and is not
// read at all.
func (f *reportFolder) readReport(ctx context.Context, name string, want int64) ([]byte, error) {
	data, read, err := f.readRegular(ctx, name, want, ReportMax)
	f.read = read
	return data, err
}

// readRegular reads the ordinary file name in the folder, whole, within limit
// bytes; want, when it is not negative, is the only size accepted.
//
// THE NAME IS LOOKED AT BEFORE IT IS OPENED, AND THE OPENED FILE MUST BE WHAT
// WAS LOOKED AT. A link, a pipe, a device or a folder is refused without being
// opened (a device's open can act); the open itself cannot follow a link or
// wait on a pipe; and the descriptor must name the same inode, one name only.
// After the read the descriptor must still say the same size, modification
// and change times (a rewrite in place moves the change time even when it
// puts the same size and mtime back), and the name must still lead to it.
func (f *reportFolder) readRegular(ctx context.Context, name string, want, limit int64) ([]byte, unix.Stat_t, error) {
	var named unix.Stat_t
	if err := unix.Fstatat(f.fd, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, unix.Stat_t{}, ErrReportMissing
		}
		return nil, unix.Stat_t{}, ErrReportUnreadable
	}
	if named.Mode&unix.S_IFMT != unix.S_IFREG || named.Nlink != 1 {
		return nil, unix.Stat_t{}, ErrReportUnsafe
	}
	if named.Size > limit {
		return nil, unix.Stat_t{}, ErrReportTooLarge
	}
	if want >= 0 && named.Size != want {
		return nil, unix.Stat_t{}, ErrReportNotReceipted
	}
	fd, err := unix.Openat(f.fd, name, reportOpenFlags, 0)
	switch {
	case err == nil:
	case errors.Is(err, unix.ENOENT):
		return nil, unix.Stat_t{}, ErrReportChanged
	case errors.Is(err, unix.ELOOP), errors.Is(err, unix.ENXIO), errors.Is(err, unix.EISDIR), errors.Is(err, unix.ENOTDIR):
		return nil, unix.Stat_t{}, ErrReportUnsafe
	default:
		return nil, unix.Stat_t{}, ErrReportUnreadable
	}
	defer unix.Close(fd)
	var before unix.Stat_t
	if unix.Fstat(fd, &before) != nil {
		return nil, unix.Stat_t{}, ErrReportUnreadable
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Nlink != 1 {
		return nil, unix.Stat_t{}, ErrReportUnsafe
	}
	if !reportSameStat(named, before) {
		return nil, unix.Stat_t{}, ErrReportChanged
	}
	// ONE BYTE OF ROOM BEYOND THE SIZE THE DESCRIPTOR SAID: a file that grew
	// since is seen as the extra byte and refused, not cut.
	buf := make([]byte, before.Size+1)
	filled := 0
	for filled < len(buf) {
		if err := ctx.Err(); err != nil {
			return nil, unix.Stat_t{}, err
		}
		n, err := unix.Read(fd, buf[filled:min(filled+reportChunk, len(buf))])
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return nil, unix.Stat_t{}, ErrReportUnreadable
		}
		if n == 0 {
			break
		}
		filled += n
	}
	var after unix.Stat_t
	if unix.Fstat(fd, &after) != nil {
		return nil, unix.Stat_t{}, ErrReportUnreadable
	}
	if !reportSameStat(before, after) || int64(filled) != after.Size {
		return nil, unix.Stat_t{}, ErrReportChanged
	}
	var again unix.Stat_t
	if err := unix.Fstatat(f.fd, name, &again, unix.AT_SYMLINK_NOFOLLOW); err != nil || again.Dev != after.Dev || again.Ino != after.Ino {
		return nil, unix.Stat_t{}, ErrReportChanged
	}
	return buf[:filled], after, nil
}

// still answers whether, after everything the caller did with the bytes, the
// path still names the folder this descriptor holds and the name still leads
// to the very file that was read in the very state it was read in: a folder
// renamed away or replaced, a name renamed over, or a rewrite in place (which
// moves the change time even when it puts the size and mtime back) is a read
// to do again.
func (f *reportFolder) still(name string) error {
	var named unix.Stat_t
	if err := unix.Lstat(f.dir, &named); err != nil || named.Mode&unix.S_IFMT != unix.S_IFDIR ||
		named.Dev != f.held.Dev || named.Ino != f.held.Ino {
		return ErrReportChanged
	}
	var file unix.Stat_t
	if err := unix.Fstatat(f.fd, name, &file, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
		file.Mode&unix.S_IFMT != unix.S_IFREG || !reportSameStat(file, f.read) {
		return ErrReportChanged
	}
	return nil
}

// reportSameStat answers whether two looks at a file saw the same file in the
// same state: the inode, the size, and the modification and change times.
func reportSameStat(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}
