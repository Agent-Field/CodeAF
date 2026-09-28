package skills

// THE ONE OPENER FOR EVERY FILE A SKILL SCAN READS. A skill folder, a Claude
// Code settings file and a plugin's registry, manifest and catalog are all
// files somebody else put on this disk — the settings file and the skill
// folders of a project arrive with a cloned repository, and git commits a link
// as readily as a file. A link to a terminal made launch read the person's own
// keystrokes as a skill until ctrl+d; a pipe blocked in open itself; a link to
// /dev/zero was read until memory ran out. So every such read goes through
// [OpenRegular], and A FILE IT CANNOT OPEN SAFELY IS ABSENT: the scan carries on
// without it, exactly as it does for a file that is not there.

import (
	"errors"
	"io"
	"os"
)

var (
	// ErrNotRegular makes an unsafe file absent from discovery instead of
	// offering a skill whose content could block the conversation.
	ErrNotRegular = errors.New("not a regular file")
	// ErrTooLarge keeps plugin JSON from consuming an unbounded allocation.
	ErrTooLarge = errors.New("file exceeds read limit")
)

// Plugin metadata is all-or-nothing JSON, so a file above this bound is absent.
const maxPluginJSONBytes = 1 << 20

// OpenRegular opens only ordinary files, including links to ordinary files.
// Stat rejects a pipe before open can block; Fstat checks the actual descriptor
// after open because another process can replace the path between those calls.
func OpenRegular(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrNotRegular
	}
	f, err := openNonblocking(path)
	if err != nil {
		return nil, err
	}
	info, err = f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, ErrNotRegular
	}
	return f, nil
}

// ReadRegularHead reads only the bounded head of an ordinary file. A SKILL.md
// can have a long body, but its frontmatter is what discovery needs.
func ReadRegularHead(path string, limit int64) ([]byte, error) {
	f, err := OpenRegular(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit))
}

// ReadWholeRegular rejects a file beyond limit instead of returning a truncated
// JSON document. Stat avoids reading a known large file, and the extra byte
// catches one that grew while it was being read.
func ReadWholeRegular(path string, limit int64) ([]byte, error) {
	f, err := OpenRegular(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil {
		return nil, err
	} else if info.Size() > limit {
		return nil, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, err
	}
	var extra [1]byte
	if n, err := f.Read(extra[:]); n > 0 {
		return nil, ErrTooLarge
	} else if err != nil && err != io.EOF {
		return nil, err
	}
	return data, nil
}

// readPluginJSON keeps every plugin metadata reader under the same size law.
func readPluginJSON(path string) ([]byte, error) {
	return ReadWholeRegular(path, maxPluginJSONBytes)
}
