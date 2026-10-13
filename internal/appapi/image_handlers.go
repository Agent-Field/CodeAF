package appapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const appImageLimit = 10 << 20

var imageContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/image", Response: BinaryResponse}, handle: (*Server).imageHandler},
}

func (s *Server) imageHandler(w http.ResponseWriter, r *http.Request) {
	record, err := s.imageAgent(r.PathValue("id"))
	if err != nil {
		agentFailure(w, err)
		return
	}
	data, contentType, err := readAppImage(record, r.URL.Query().Get("path"))
	if err != nil {
		agentFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) imageAgent(id string) (agentRecord, error) {
	a := s.options.Agents
	if a == nil {
		return agentRecord{}, errAgentMissing
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var record agentRecord
	err := a.disk(func(d *agentDisk) error {
		var ok bool
		record, ok = d.Records[id]
		if !ok {
			return errAgentMissing
		}
		return nil
	}, false)
	return record, err
}

func readAppImage(record agentRecord, path string) ([]byte, string, error) {
	file, err := openAppImage(record, path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, "", imageFileError(err)
	}
	if !info.Mode().IsRegular() {
		return nil, "", agentError(415, "not_allowed", "this path is not an image file")
	}
	if info.Size() > appImageLimit {
		return nil, "", imageTooLargeError()
	}
	contentType := appImageType(file.Name())
	if contentType == "" {
		return nil, "", agentError(415, "not_allowed", "use a PNG, JPEG, GIF or WebP image")
	}
	// Bound the read as well as the stat, because a writer may still be saving the image.
	data, err := io.ReadAll(io.LimitReader(file, appImageLimit+1))
	if err != nil {
		return nil, "", imageFileError(err)
	}
	if len(data) > appImageLimit {
		return nil, "", imageTooLargeError()
	}
	if http.DetectContentType(data) != contentType {
		return nil, "", agentError(415, "not_allowed", "the image contents must match its file extension")
	}
	return data, contentType, nil
}

func openAppImage(record agentRecord, path string) (*os.File, error) {
	if path == "" {
		return nil, agentError(422, "bad_input", "path must name an image")
	}
	if !filepath.IsAbs(path) {
		if record.Workspace == "" {
			return nil, agentError(403, "not_allowed", "this agent has no workspace for relative image paths")
		}
		path = filepath.Join(record.Workspace, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, imageFileError(err)
	}
	roots := appImageRoots(record)
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, _, ok := appImageRoot(roots, appImageMissingParent(err)); !ok {
				return nil, imageOutsideError()
			}
		}
		return nil, imageFileError(err)
	}
	dir, relative, ok := appImageRoot(roots, resolved)
	if !ok {
		return nil, imageOutsideError()
	}
	// A rooted open also refuses a symlink escape if the file changes after resolution.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, imageFileError(err)
	}
	defer root.Close()
	// Reject pipes and devices before opening, so an image read cannot wait for a writer.
	info, err := root.Stat(relative)
	if err != nil {
		return nil, imageFileError(err)
	}
	if !info.Mode().IsRegular() {
		return nil, agentError(415, "not_allowed", "this path is not an image file")
	}
	file, err := root.Open(relative)
	if err != nil {
		return nil, imageFileError(err)
	}
	return file, nil
}

func appImageRoots(record agentRecord) []string {
	paths := []string{record.Workspace}
	if record.SessionFile != "" {
		paths = append(paths, filepath.Dir(record.SessionFile))
	}
	var roots []string
	for _, path := range paths {
		if path == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			roots = append(roots, resolved)
		}
	}
	return roots
}

// Missing files still need a resolved boundary check, including /tmp aliases and symlink targets.
func appImageMissingParent(err error) string {
	for errors.Is(err, os.ErrNotExist) {
		var failure *os.PathError
		if !errors.As(err, &failure) {
			return ""
		}
		parent := filepath.Dir(failure.Path)
		if parent == failure.Path {
			return ""
		}
		var resolved string
		resolved, err = filepath.EvalSymlinks(parent)
		if err == nil {
			return resolved
		}
	}
	return ""
}

func appImageRoot(roots []string, path string) (string, string, bool) {
	for _, root := range roots {
		relative, err := filepath.Rel(root, path)
		if err == nil && filepath.IsLocal(relative) {
			return root, relative, true
		}
	}
	return "", "", false
}

func appImageType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return ""
}

func imageOutsideError() error {
	return agentError(403, "not_allowed", "images must be inside this agent's workspace or session directory")
}

func imageTooLargeError() error {
	return agentError(413, "not_allowed", fmt.Sprintf("images must be at most %d MiB", appImageLimit>>20))
}

func imageFileError(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return agentError(404, "not_found", "image not found")
	}
	return agentError(403, "not_allowed", "the image could not be read safely")
}
