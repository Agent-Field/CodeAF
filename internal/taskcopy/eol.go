package taskcopy

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/cell"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// checkoutEOL records clean text files' actual line endings (K3/K4). Git status
// normalizes them, so a clean LF file can otherwise become CRLF when another
// computer checks it out. Mixed endings need their raw overlay, because one
// style cannot reconstruct their per-line choices. Binary files stay untouched.
func checkoutEOL(tree string) (map[string]string, []string, error) {
	out, err := git(tree, "ls-files", "--eol", "-z")
	if err != nil {
		return nil, nil, err
	}
	styles := map[string]string{}
	var mixed []string
	for _, entry := range strings.Split(out, "\x00") {
		metadata, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(metadata)
		if !ok || len(fields) < 2 || cell.IsTaskDropping(path) {
			continue
		}
		switch fields[1] {
		case "w/lf":
			styles[path] = "lf"
		case "w/crlf":
			styles[path] = "crlf"
		case "w/mixed":
			mixed = append(mixed, path)
		}
	}
	return styles, mixed, nil
}

// applyEOL corrects only the checkout's representation, before raw edits are
// overlaid. The record cannot redirect writes outside the task copy (K3/K4).
func applyEOL(dest string, styles map[string]string) error {
	if len(styles) == 0 {
		return nil
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()
	for rel, style := range styles {
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return fmt.Errorf("carried line endings for %q leave the task copy", rel)
		}
		if style != "lf" && style != "crlf" {
			return fmt.Errorf("unknown carried line endings for %q", rel)
		}
		file, err := root.Open(filepath.FromSlash(rel))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return err
		}
		if !info.Mode().IsRegular() {
			file.Close()
			continue
		}
		raw, err := io.ReadAll(file)
		closeErr := file.Close()
		if err = errors.Join(err, closeErr); err != nil {
			return err
		}
		converted := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
		if style == "crlf" {
			converted = bytes.ReplaceAll(converted, []byte("\n"), []byte("\r\n"))
		}
		if bytes.Equal(raw, converted) {
			continue
		}
		// The copy is being rebuilt before it can open. Its original mode is put
		// back by applyModes; a read-only checkout may need write permission first.
		if err := root.Chmod(filepath.FromSlash(rel), info.Mode().Perm()|0200); err != nil {
			return err
		}
		output, err := root.OpenFile(filepath.FromSlash(rel), os.O_WRONLY|os.O_TRUNC, 0)
		if err != nil {
			return err
		}
		_, err = output.Write(converted)
		if err = errors.Join(err, output.Close()); err != nil {
			return err
		}
	}
	return nil
}

// portableModes keeps the Unix bits a moved copy recorded, even when NTFS
// cannot represent them; a new Windows path gets 0644 (K3/K4).
func portableModes(tree, dest string) map[string]fs.FileMode {
	modes := modesOfTree(tree)
	for path := range modes {
		if portableNativeMetadataPath(path) {
			delete(modes, path)
		}
	}
	if runtime.GOOS != "windows" {
		return modes
	}
	prior, _ := readRecord(dest)
	for path := range modes {
		if mode, ok := prior.Modes[path]; ok {
			modes[path] = mode
		} else {
			modes[path] = 0644
		}
	}
	return modes
}
