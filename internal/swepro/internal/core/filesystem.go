// Application filesystem — port of src/core/filesystem.ts:10-244
// (swe-pro 3b25a1a). The methods keep the source's distinction between plain
// writes and write-with-parent-directory retry.
package core

import (
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// FileSystemError wraps operations implemented outside the basic os package.
type FileSystemError struct {
	Method string
	Cause  error
}

func (e *FileSystemError) Error() string {
	if e.Cause == nil {
		return "FileSystemError: " + e.Method
	}
	return "FileSystemError: " + e.Method + ": " + e.Cause.Error()
}

func (e *FileSystemError) Unwrap() error { return e.Cause }

// DirEntry is the portable directory-entry shape from AppFileSystem.
type DirEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// GlobOptions mirrors core/util/glob.ts.
type GlobOptions struct {
	Cwd      string
	Absolute bool
	Include  string // "file" (default) or "all"
	Dot      bool
	Symlink  bool
}

// AppFileSystem is the concrete application filesystem service.
type AppFileSystem struct{}

// NewFileSystem constructs the default OS-backed service.
func NewFileSystem() *AppFileSystem { return &AppFileSystem{} }

// ExistsSafe swallows all stat failures.
func (f *AppFileSystem) ExistsSafe(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ReadFileStringSafe maps only NotExist to an absent result.
func (f *AppFileSystem) ReadFileStringSafe(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

// IsDir returns false on every stat failure.
func (f *AppFileSystem) IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// IsFile returns false on every stat failure.
func (f *AppFileSystem) IsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// ReadDirectoryEntries preserves the operating system's readdir order.
func (f *AppFileSystem) ReadDirectoryEntries(path string) ([]DirEntry, error) {
	dir, err := os.Open(path)
	if err != nil {
		return nil, &FileSystemError{Method: "readDirectoryEntries", Cause: err}
	}
	defer dir.Close()
	entries, err := dir.Readdir(-1)
	if err != nil {
		return nil, &FileSystemError{Method: "readDirectoryEntries", Cause: err}
	}
	out := make([]DirEntry, 0, len(entries))
	for _, entry := range entries {
		entryType := "other"
		switch {
		case entry.IsDir():
			entryType = "directory"
		case entry.Mode()&os.ModeSymlink != 0:
			entryType = "symlink"
		case entry.Mode().IsRegular():
			entryType = "file"
		}
		out = append(out, DirEntry{Name: entry.Name(), Type: entryType})
	}
	return out, nil
}

// ReadJSON parses a file with JSON.parse's float64 number model.
func (f *AppFileSystem) ReadJSON(path string, dst any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	return decoder.Decode(dst)
}

// WriteJSON does not create parent directories.
func (f *AppFileSystem) WriteJSON(path string, data any, mode ...fs.FileMode) error {
	content, err := jscompat.StringifyIndent(data)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, content, 0o666); err != nil {
		return err
	}
	if len(mode) > 0 && mode[0] != 0 {
		return os.Chmod(path, mode[0])
	}
	return nil
}

// EnsureDir creates path recursively.
func (f *AppFileSystem) EnsureDir(path string) error {
	return os.MkdirAll(path, 0o777)
}

// WriteWithDirs retries a missing-parent write after creating the parent.
func (f *AppFileSystem) WriteWithDirs(path string, content []byte, mode ...fs.FileMode) error {
	err := os.WriteFile(path, content, 0o666)
	if errors.Is(err, fs.ErrNotExist) {
		if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o777); mkdirErr != nil {
			return mkdirErr
		}
		err = os.WriteFile(path, content, 0o666)
	}
	if err != nil {
		return err
	}
	if len(mode) > 0 && mode[0] != 0 {
		return os.Chmod(path, mode[0])
	}
	return nil
}

// WriteStringWithDirs is the string overload of WriteWithDirs.
func (f *AppFileSystem) WriteStringWithDirs(path, content string, mode ...fs.FileMode) error {
	return f.WriteWithDirs(path, []byte(content), mode...)
}

// Glob scans from options.Cwd using minimatch-style ** path segments.
func (f *AppFileSystem) Glob(pattern string, options ...GlobOptions) ([]string, error) {
	opt := GlobOptions{}
	if len(options) > 0 {
		opt = options[0]
	}
	cwd := opt.Cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, &FileSystemError{Method: "glob", Cause: err}
		}
	}
	out := []string{}
	err := walkGlob(cwd, opt.Symlink, func(path string, entry fs.DirEntry) error {
		if path == cwd {
			return nil
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			return err
		}
		slashRel := filepath.ToSlash(rel)
		if !opt.Dot && hasDotSegment(slashRel) && !patternMentionsDot(pattern) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !GlobMatch(pattern, slashRel) {
			return nil
		}
		if opt.Include != "all" && entry.IsDir() {
			return nil
		}
		if opt.Absolute {
			absolute, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			out = append(out, absolute)
		} else {
			out = append(out, filepath.FromSlash(slashRel))
		}
		return nil
	})
	if err != nil {
		return nil, &FileSystemError{Method: "glob", Cause: err}
	}
	return out, nil
}

func walkGlob(root string, followSymlinks bool, visit func(string, fs.DirEntry) error) error {
	seen := map[string]bool{}
	var walk func(string) error
	walk = func(path string) error {
		real := path
		if followSymlinks {
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				real = resolved
			}
			if seen[real] {
				return nil
			}
			seen[real] = true
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			child := filepath.Join(path, entry.Name())
			err := visit(child, entry)
			if errors.Is(err, fs.SkipDir) {
				continue
			}
			if err != nil {
				return err
			}
			isDir := entry.IsDir()
			if !isDir && followSymlinks && entry.Type()&os.ModeSymlink != 0 {
				if info, err := os.Stat(child); err == nil {
					isDir = info.IsDir()
				}
			}
			if isDir {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(root)
}

// GlobMatch mirrors minimatch(filepath, pattern, {dot:true}).
func GlobMatch(pattern, path string) bool {
	patternParts := splitSlash(pattern)
	pathParts := splitSlash(path)
	var match func(int, int) bool
	match = func(pi, si int) bool {
		if pi == len(patternParts) {
			return si == len(pathParts)
		}
		if patternParts[pi] == "**" {
			if match(pi+1, si) {
				return true
			}
			return si < len(pathParts) && match(pi, si+1)
		}
		if si >= len(pathParts) {
			return false
		}
		ok, err := filepath.Match(patternParts[pi], pathParts[si])
		return err == nil && ok && match(pi+1, si+1)
	}
	return match(0, 0)
}

func splitSlash(value string) []string {
	value = strings.ReplaceAll(value, "\\", "/")
	value = strings.TrimPrefix(value, "./")
	return strings.Split(value, "/")
}

func hasDotSegment(path string) bool {
	for _, part := range splitSlash(path) {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func patternMentionsDot(pattern string) bool {
	for _, part := range splitSlash(pattern) {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// FindUp finds target at start and each parent, nearest first.
func (f *AppFileSystem) FindUp(target, start string, stop ...string) ([]string, error) {
	return f.Up(UpOptions{Targets: []string{target}, Start: start, Stop: first(stop)})
}

// UpOptions configures Up.
type UpOptions struct {
	Targets []string
	Start   string
	Stop    string
}

// Up finds all target names at every ancestor.
func (f *AppFileSystem) Up(options UpOptions) ([]string, error) {
	result := []string{}
	current := options.Start
	for {
		for _, target := range options.Targets {
			search := filepath.Join(current, target)
			if _, err := os.Stat(search); err == nil {
				result = append(result, search)
			}
		}
		if options.Stop == current {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return result, nil
}

// GlobUp scans each ancestor and swallows per-directory glob errors.
func (f *AppFileSystem) GlobUp(pattern, start string, stop ...string) ([]string, error) {
	result := []string{}
	current := start
	stopAt := first(stop)
	for {
		matches, err := f.Glob(pattern, GlobOptions{Cwd: current, Absolute: true, Dot: true})
		if err == nil {
			result = append(result, matches...)
		}
		if stopAt == current {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return result, nil
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// MimeType mirrors mime-types.lookup(p) || application/octet-stream.
func MimeType(path string) string {
	extension := strings.ToLower(filepath.Ext(path))
	switch extension {
	case ".md", ".markdown":
		return "text/markdown"
	case ".ts":
		return "video/mp2t"
	case ".js", ".mjs":
		return "text/javascript"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "text/yaml"
	case ".wasm":
		return "application/wasm"
	case ".tsx":
		return "application/octet-stream"
	}
	if value := mime.TypeByExtension(extension); value != "" {
		return strings.TrimSpace(strings.Split(value, ";")[0])
	}
	return "application/octet-stream"
}

// NormalizePath canonicalizes Windows paths; it is a no-op on other systems.
func NormalizePath(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	resolved, _ := filepath.Abs(WindowsPath(path))
	if real, err := filepath.EvalSymlinks(resolved); err == nil {
		return real
	}
	return resolved
}

// NormalizePathPattern preserves a terminal wildcard during normalization.
func NormalizePathPattern(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	if path == "*" {
		return path
	}
	normalized := strings.ReplaceAll(path, "\\", "/")
	if !strings.HasSuffix(normalized, "/*") {
		return NormalizePath(path)
	}
	dir := strings.TrimSuffix(normalized, "/*")
	if len(dir) == 2 && dir[1] == ':' {
		dir += `\`
	}
	return filepath.Join(NormalizePath(dir), "*")
}

// Resolve returns the real absolute path or the normalized absolute path when
// the target does not exist.
func Resolve(path string) (string, error) {
	resolved, err := filepath.Abs(WindowsPath(path))
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(resolved)
	if err == nil {
		return NormalizePath(real), nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return NormalizePath(resolved), nil
	}
	return "", err
}

// WindowsPath translates common POSIX drive spellings on Windows.
func WindowsPath(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	return windowsPath(path)
}

func windowsPath(path string) string {
	slash := strings.ReplaceAll(path, "\\", "/")
	var rest string
	var drive byte
	switch {
	case len(slash) >= 3 && slash[0] == '/' && isASCIIAlpha(slash[1]) && slash[2] == ':':
		if len(slash) > 3 && slash[3] != '/' {
			return path
		}
		drive, rest = slash[1], slash[3:]
	case len(slash) >= 2 && slash[0] == '/' && isASCIIAlpha(slash[1]) && (len(slash) == 2 || slash[2] == '/'):
		drive, rest = slash[1], slash[2:]
	case strings.HasPrefix(slash, "/cygdrive/") && len(slash) >= 11 && isASCIIAlpha(slash[10]) &&
		(len(slash) == 11 || slash[11] == '/'):
		drive, rest = slash[10], slash[11:]
	case strings.HasPrefix(slash, "/mnt/") && len(slash) >= 6 && isASCIIAlpha(slash[5]) &&
		(len(slash) == 6 || slash[6] == '/'):
		drive, rest = slash[5], slash[6:]
	default:
		return path
	}
	if drive >= 'a' && drive <= 'z' {
		drive -= 'a' - 'A'
	}
	return string(drive) + ":/" + strings.TrimPrefix(rest, "/")
}

func isASCIIAlpha(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

// Overlaps reports whether either path is within the other.
func Overlaps(a, b string) bool {
	relA, _ := filepath.Rel(a, b)
	relB, _ := filepath.Rel(b, a)
	return relA == "" || !strings.HasPrefix(relA, "..") || relB == "" || !strings.HasPrefix(relB, "..")
}

// Contains reports whether child is under parent. The prefix-only `..` check
// deliberately matches the TS source.
func Contains(parent, child string) bool {
	relative, _ := filepath.Rel(parent, child)
	return !strings.HasPrefix(relative, "..")
}
