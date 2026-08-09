// Package instruction ports src/session/instruction.ts:1-239 from swe-pro
// commit 3b25a1a.
package instruction

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
)

type OrderedSet struct {
	keys []string
	set  map[string]struct{}
}

func NewOrderedSet() *OrderedSet {
	return &OrderedSet{set: make(map[string]struct{})}
}

func (set *OrderedSet) Add(value string) {
	if _, exists := set.set[value]; exists {
		return
	}
	set.set[value] = struct{}{}
	set.keys = append(set.keys, value)
}

func (set *OrderedSet) Has(value string) bool {
	_, exists := set.set[value]
	return exists
}

func (set *OrderedSet) Values() []string {
	return append([]string{}, set.keys...)
}

func Loaded(messages []msgmodel.WithParts) *OrderedSet {
	paths := NewOrderedSet()
	for _, message := range messages {
		for _, part := range message.Parts {
			tool, ok := part.(msgmodel.ToolPart)
			if !ok || tool.Tool != "read" {
				continue
			}
			completed, ok := tool.State.(msgmodel.ToolStateCompleted)
			if !ok || completed.Time.Compacted != nil && *completed.Time.Compacted != 0 {
				continue
			}
			raw, ok := completed.Metadata.Field("loaded")
			if !ok {
				continue
			}
			var loaded []json.RawMessage
			if json.Unmarshal(raw, &loaded) != nil || loaded == nil {
				continue
			}
			for _, item := range loaded {
				trimmed := strings.TrimSpace(string(item))
				if len(trimmed) == 0 || trimmed[0] != '"' {
					continue
				}
				var path string
				if json.Unmarshal(item, &path) == nil {
					paths.Add(path)
				}
			}
		}
	}
	return paths
}

type Config struct {
	Instructions []string
}

type Global struct {
	Config string
	Home   string
}

type Instance struct {
	Directory string
	Worktree  string
}

type Flags struct {
	DisableClaudeCodePrompt bool
	DisableProjectConfig    bool
}

type FileSystem interface {
	ExistsSafe(path string) bool
	ReadFileString(path string) (string, error)
	FindUp(target, start, stop string) ([]string, error)
	GlobUp(pattern, start, stop string) ([]string, error)
	Glob(pattern, cwd string) ([]string, error)
}

type HTTPClient interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

type Options struct {
	Config   Config
	Global   Global
	Instance Instance
	Flags    Flags
	FS       FileSystem
	HTTP     HTTPClient
}

type Service struct {
	options Options
	files   []string

	mu     sync.Mutex
	claims map[string]*OrderedSet
}

func New(options Options) *Service {
	if options.FS == nil {
		options.FS = OSFileSystem{}
	}
	if options.HTTP == nil {
		options.HTTP = &DefaultHTTPClient{}
	}
	files := []string{"AGENTS.md"}
	if !options.Flags.DisableClaudeCodePrompt {
		files = append(files, "CLAUDE.md")
	}
	files = append(files, "CONTEXT.md")
	return &Service{
		options: options, files: files, claims: make(map[string]*OrderedSet),
	}
}

func (service *Service) Clear(messageID string) {
	service.mu.Lock()
	delete(service.claims, messageID)
	service.mu.Unlock()
}

func (service *Service) SystemPaths() *OrderedSet {
	paths := NewOrderedSet()
	globalFiles := []string{filepath.Join(service.options.Global.Config, "AGENTS.md")}
	if !service.options.Flags.DisableClaudeCodePrompt {
		globalFiles = append(globalFiles, filepath.Join(
			service.options.Global.Home, ".claude", "CLAUDE.md",
		))
	}
	for _, file := range globalFiles {
		if service.options.FS.ExistsSafe(file) {
			paths.Add(resolve(file))
			break
		}
	}

	if !service.options.Flags.DisableProjectConfig {
		for _, file := range service.files {
			matches, _ := service.options.FS.FindUp(
				file, service.options.Instance.Directory, service.options.Instance.Worktree,
			)
			if len(matches) > 0 {
				for _, item := range matches {
					paths.Add(resolve(item))
				}
				break
			}
		}
	}

	for _, raw := range service.options.Config.Instructions {
		if isURL(raw) {
			continue
		}
		instruction := raw
		if strings.HasPrefix(raw, "~/") {
			instruction = filepath.Join(service.options.Global.Home, raw[2:])
		}
		var matches []string
		if filepath.IsAbs(instruction) {
			matches, _ = service.options.FS.Glob(
				filepath.Base(instruction), filepath.Dir(instruction),
			)
		} else if !service.options.Flags.DisableProjectConfig {
			matches, _ = service.options.FS.GlobUp(
				instruction, service.options.Instance.Directory,
				service.options.Instance.Worktree,
			)
		} else {
			matches, _ = service.options.FS.GlobUp(
				instruction, service.options.Global.Config,
				service.options.Global.Config,
			)
		}
		for _, item := range matches {
			paths.Add(resolve(item))
		}
	}
	return paths
}

func (service *Service) System(ctx context.Context) []string {
	paths := service.SystemPaths().Values()
	urls := []string{}
	for _, item := range service.options.Config.Instructions {
		if isURL(item) {
			urls = append(urls, item)
		}
	}
	files := parallelStrings(len(paths), 8, func(index int) string {
		content, err := service.options.FS.ReadFileString(paths[index])
		if err != nil {
			return ""
		}
		return content
	})
	remote := parallelStrings(len(urls), 4, func(index int) string {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		body, err := service.options.HTTP.Fetch(callCtx, urls[index])
		if err != nil {
			return ""
		}
		return strings.ToValidUTF8(string(body), "\uFFFD")
	})
	out := []string{}
	for index, path := range paths {
		if files[index] != "" {
			out = append(out, "Instructions from: "+path+"\n"+files[index])
		}
	}
	for index, url := range urls {
		if remote[index] != "" {
			out = append(out, "Instructions from: "+url+"\n"+remote[index])
		}
	}
	return out
}

func (service *Service) Find(dir string) *string {
	for _, file := range service.files {
		path := resolve(filepath.Join(dir, file))
		if service.options.FS.ExistsSafe(path) {
			return &path
		}
	}
	return nil
}

type Resolved struct {
	Filepath string `json:"filepath"`
	Content  string `json:"content"`
}

func (service *Service) Resolve(
	messages []msgmodel.WithParts, file string, messageID string,
) []Resolved {
	system := service.SystemPaths()
	already := Loaded(messages)
	results := []Resolved{}
	root := resolve(service.options.Instance.Directory)
	target := resolve(file)
	current := filepath.Dir(target)
	for strings.HasPrefix(current, root) && current != root {
		found := service.Find(current)
		if found == nil || *found == target || system.Has(*found) || already.Has(*found) {
			current = filepath.Dir(current)
			continue
		}

		service.mu.Lock()
		claimed := service.claims[messageID]
		if claimed == nil {
			claimed = NewOrderedSet()
			service.claims[messageID] = claimed
		}
		if claimed.Has(*found) {
			service.mu.Unlock()
			current = filepath.Dir(current)
			continue
		}
		claimed.Add(*found)
		service.mu.Unlock()

		content, err := service.options.FS.ReadFileString(*found)
		if err == nil && content != "" {
			results = append(results, Resolved{
				Filepath: *found,
				Content:  "Instructions from: " + *found + "\n" + content,
			})
		}
		current = filepath.Dir(current)
	}
	return results
}

func parallelStrings(count, limit int, work func(int) string) []string {
	out := make([]string, count)
	if count == 0 {
		return out
	}
	semaphore := make(chan struct{}, limit)
	var group sync.WaitGroup
	for index := range count {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			semaphore <- struct{}{}
			out[index] = work(index)
			<-semaphore
		}(index)
	}
	group.Wait()
	return out
}

func isURL(value string) bool {
	return strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "http://")
}

func resolve(path string) string {
	value, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(value)
}

type OSFileSystem struct{}

func (OSFileSystem) ExistsSafe(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (OSFileSystem) ReadFileString(path string) (string, error) {
	data, err := os.ReadFile(path)
	return strings.ToValidUTF8(string(data), "\uFFFD"), err
}

func (filesystem OSFileSystem) FindUp(
	target, start, stop string,
) ([]string, error) {
	result := []string{}
	current := start
	for {
		search := filepath.Join(current, target)
		if filesystem.ExistsSafe(search) {
			result = append(result, search)
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
	return result, nil
}

func (filesystem OSFileSystem) GlobUp(
	pattern, start, stop string,
) ([]string, error) {
	result := []string{}
	current := start
	for {
		matches, _ := filesystem.Glob(pattern, current)
		result = append(result, matches...)
		if stop == current {
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

func (OSFileSystem) Glob(pattern, cwd string) ([]string, error) {
	re, err := globRegexp(filepath.ToSlash(pattern))
	if err != nil {
		return nil, err
	}
	result := []string{}
	err = filepath.WalkDir(cwd, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(cwd, path)
		if err != nil {
			return err
		}
		if re.MatchString(filepath.ToSlash(relative)) {
			result = append(result, resolve(path))
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	return result, err
}

func globRegexp(pattern string) (*regexp.Regexp, error) {
	var out strings.Builder
	out.WriteString("^")
	for index := 0; index < len(pattern); {
		switch pattern[index] {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				index += 2
				if index < len(pattern) && pattern[index] == '/' {
					index++
					out.WriteString("(?:.*/)?")
				} else {
					out.WriteString(".*")
				}
			} else {
				index++
				out.WriteString("[^/]*")
			}
		case '?':
			index++
			out.WriteString("[^/]")
		default:
			out.WriteString(regexp.QuoteMeta(string(pattern[index])))
			index++
		}
	}
	out.WriteString("$")
	return regexp.Compile(out.String())
}

type DefaultHTTPClient struct {
	Client *http.Client
}

func (client *DefaultHTTPClient) Fetch(
	ctx context.Context, url string,
) ([]byte, error) {
	httpClient := client.Client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		response, err := httpClient.Do(request)
		if err == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			data, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			return data, readErr
		}
		transient := err != nil
		if response != nil {
			_ = response.Body.Close()
			err = errors.New(response.Status)
			transient = response.StatusCode == http.StatusRequestTimeout ||
				response.StatusCode == http.StatusTooManyRequests ||
				response.StatusCode >= 500
		}
		last = err
		if !transient {
			return nil, last
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return nil, context.Cause(ctx)
			case <-time.After(time.Duration(200*(1<<attempt)) * time.Millisecond):
			}
		}
	}
	return nil, last
}
