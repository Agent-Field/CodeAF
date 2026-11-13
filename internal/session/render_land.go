package session

import (
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

// renderLanding is how [Agent.landRender] ended.
type renderLanding int

const (
	// renderLanded: the file exists, complete, at the returned path.
	renderLanded renderLanding = iota
	// renderStopped: a stop was accepted before the bytes were committed, and
	// NOTHING was written — not a file, not an empty placeholder.
	renderStopped
	// renderNotSaved: the render was committed and saving it failed.
	renderNotSaved
)

// landRender is where a finished render becomes a file, and it is the only
// place that happens for a video or a clip.
//
// THE ORDER IS THE LAW. The commit gate ([job.commitRender]) comes FIRST, before
// the destination is even chosen — choosing one claims an empty file on disk —
// so a stop accepted before this point leaves no trace. After the gate a stop
// can no longer win ([job.requestKillRender] answers it with "already
// finished"), the bytes are written whole through [writeFileAtomic], and the
// caller records and announces a file that exists. There is no state in which a
// stop was acknowledged and a file then appears, and none in which "no file was
// saved" is said about one that was.
//
// write is a parameter only so a test can hold the write open and prove a stop
// arriving mid-write changes nothing.
func (a *Agent) landRender(rendering *job, asked, prompt, extension, directory string, data []byte, write func(string, []byte) error) (string, renderLanding, error) {
	if !rendering.commitRender() {
		return "", renderStopped, nil
	}
	path, err := a.mediaDestination(asked, prompt, extension, directory)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	if err == nil {
		err = write(path, data)
	}
	if err != nil {
		// The placeholder mediaDestination claimed is ours to clear, and only
		// while it is still empty and the name was chosen by us rather than the
		// caller: a path the caller asked for may name a file they care about.
		if path != "" && asked == "" {
			if info, statErr := os.Stat(path); statErr == nil && info.Size() == 0 {
				_ = os.Remove(path)
			}
		}
		return "", renderNotSaved, err
	}
	return path, renderLanded, nil
}

// writeFileAtomic writes data to a temporary file beside path and renames it
// into place, so a crash or a full disk leaves either the previous file or the
// whole new one at path and never a partial.
func writeFileAtomic(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".render-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	_, err = temp.Write(data)
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(name, 0o644)
	}
	if err == nil {
		err = fsreplace.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}
