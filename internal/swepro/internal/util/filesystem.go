// Filesystem helpers — port of src/util/filesystem.ts:9-243
// (swe-pro 3b25a1a).
package util

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/core"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func Stat(path string) (os.FileInfo, bool) {
	info, err := os.Stat(path)
	return info, err == nil
}

func StatAsync(path string) (os.FileInfo, bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return info, err == nil, err
}

func Size(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func ReadText(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

func ReadJSON(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func ReadBytes(path string) ([]byte, error) { return os.ReadFile(path) }

func Write(path string, content []byte, mode ...fs.FileMode) error {
	permission := fs.FileMode(0o666)
	if len(mode) > 0 && mode[0] != 0 {
		permission = mode[0]
	}
	err := os.WriteFile(path, content, permission)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
			return err
		}
		return os.WriteFile(path, content, permission)
	}
	return err
}

func WriteText(path, content string, mode ...fs.FileMode) error {
	return Write(path, []byte(content), mode...)
}

func WriteJSON(path string, data any, mode ...fs.FileMode) error {
	content, err := jscompat.StringifyIndent(data)
	if err != nil {
		return err
	}
	return Write(path, content, mode...)
}

func WriteStream(path string, stream io.Reader, mode ...fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, stream)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(mode) > 0 && mode[0] != 0 {
		return os.Chmod(path, mode[0])
	}
	return nil
}

func FileMimeType(path string) string         { return core.MimeType(path) }
func NormalizePath(path string) string        { return core.NormalizePath(path) }
func NormalizePathPattern(path string) string { return core.NormalizePathPattern(path) }
func WindowsPath(path string) string          { return core.WindowsPath(path) }
func Overlaps(a, b string) bool               { return core.Overlaps(a, b) }
func Contains(parent, child string) bool      { return core.Contains(parent, child) }
func ResolvePath(path string) (string, error) { return core.Resolve(path) }

type FindUpOptions struct {
	RootFirst bool
}

func FindUp(targets []string, start string, stop string, options ...FindUpOptions) []string {
	dirs := []string{start}
	current := start
	for {
		if stop == current {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		dirs = append(dirs, parent)
		current = parent
	}
	if len(options) > 0 && options[0].RootFirst {
		for left, right := 0, len(dirs)-1; left < right; left, right = left+1, right-1 {
			dirs[left], dirs[right] = dirs[right], dirs[left]
		}
	}
	result := []string{}
	for _, dir := range dirs {
		for _, target := range targets {
			search := filepath.Join(dir, target)
			if Exists(search) {
				result = append(result, search)
			}
		}
	}
	return result
}

func Up(targets []string, start, stop string) []string {
	result := []string{}
	current := start
	for {
		for _, target := range targets {
			search := filepath.Join(current, target)
			if Exists(search) {
				result = append(result, search)
			}
		}
		if stop == current {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return result
}

func GlobUp(pattern, start, stop string) []string {
	filesystem := core.NewFileSystem()
	result := []string{}
	current := start
	for {
		matches, err := filesystem.Glob(pattern, core.GlobOptions{
			Cwd: current, Absolute: true, Dot: true,
		})
		if err == nil {
			result = append(result, matches...)
		}
		if stop == current {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return result
}
