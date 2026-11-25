package intake

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
)

func (s Service) inbox(app, channel string) (string, error) {
	base := filepath.Join(s.Root, "inbox", app, channel)
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	return base, nil
}

func (s Service) secret(app, id string) string {
	if !safeApp(app) || !safeName(id) {
		return ""
	}
	dir, err := os.OpenRoot(filepath.Join(s.Root, "secrets"))
	if err != nil {
		return ""
	}
	defer dir.Close()
	path := filepath.Join(app, id)
	info, err := dir.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 4096 {
		return ""
	}
	raw, err := readFile(dir, path, info, 4096)
	key := strings.TrimSpace(string(raw))
	if err != nil || len(key) < 32 || strings.ContainsAny(key, " \t\r\n") {
		return ""
	}
	return key
}

// FolderPoller remembers successful reads while open. The writer's durable
// dedupe receipt is what makes a restart or concurrent post safe.
type FolderPoller struct {
	Service Service
	seen    map[string]seenFile
}

type seenFile struct {
	info    fs.FileInfo
	version int
}

func (p *FolderPoller) Poll(ctx context.Context) error {
	root, err := os.OpenRoot(filepath.Join(p.Service.Root, "inbox"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	apps, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	next := map[string]seenFile{}
	for _, entry := range apps {
		if !entry.IsDir() || !safeApp(entry.Name()) {
			continue
		}
		app := entry.Name()
		def, loadErr := p.Service.Load(app)
		if loadErr != nil {
			err = errors.Join(err, fmt.Errorf("intake folder %s: %w", app, loadErr))
			continue
		}
		for id, channel := range def.Manifest.Channels {
			if !safeName(id) || !enabled(channel) || (channel.Kind != "file-drop" && channel.Kind != "email-in") {
				continue
			}
			path := filepath.Join(app, id)
			info, statErr := root.Lstat(path)
			if statErr != nil || !info.IsDir() {
				continue
			}
			entries, readErr := fs.ReadDir(root.FS(), path)
			if readErr != nil {
				err = errors.Join(err, readErr)
				continue
			}
			for _, file := range entries {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if !fileName(file.Name()) || strings.HasPrefix(file.Name(), ".") || !file.Type().IsRegular() {
					continue
				}
				if channel.Kind == "email-in" && !strings.EqualFold(filepath.Ext(file.Name()), ".eml") {
					continue
				}
				name := filepath.Join(path, file.Name())
				info, readErr := file.Info()
				if readErr != nil {
					err = errors.Join(err, readErr)
					continue
				}
				if sameFile(info, p.seen[name].info) && p.seen[name].version == def.Version {
					next[name] = p.seen[name]
					continue
				}
				raw, readErr := readFile(root, name, info, data.IntakeBytesMax)
				if readErr == nil {
					var receipt data.IntakeReceipt
					receipt, readErr = p.Service.deliver(def, channel, Post{App: app, Channel: id, Name: file.Name(), Content: raw})
					if !receipt.Replayed {
						for _, problem := range receipt.Problems {
							err = errors.Join(err, fmt.Errorf("intake file %s, CSV line %d: %s", name, problem.Line, problem.Message))
						}
					}
				}
				if readErr != nil {
					err = errors.Join(err, fmt.Errorf("intake file %s: %w", name, readErr))
					continue
				}
				next[name] = seenFile{info: info, version: def.Version}
			}
		}
	}
	p.seen = next
	return err
}

func sameFile(a, b fs.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime() == b.ModTime()
}

func readFile(root *os.Root, path string, expected fs.FileInfo, limit int64) ([]byte, error) {
	if !expected.Mode().IsRegular() || expected.Size() > limit {
		return nil, BadInput("the file is too large or is not a regular file")
	}
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !sameFile(info, expected) {
		return nil, BadInput("the file changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	info, err = f.Stat()
	if err != nil || int64(len(raw)) > limit || !sameFile(info, expected) || int64(len(raw)) != info.Size() {
		return nil, BadInput("the file changed while reading or is too large")
	}
	return raw, nil
}
