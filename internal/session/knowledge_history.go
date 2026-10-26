package session

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/knowledge"
)

type knowledgeHistory struct {
	root  string
	cache *knowledgeHistoryCache
}

type knowledgeHistoryCache struct {
	mu    sync.Mutex
	files map[string]knowledgeHistoryFile
}

type knowledgeHistoryFile struct {
	info   os.FileInfo
	record knowledgeHistoryRecord
}

type knowledgeHistoryRecord struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Cwd       string `json:"cwd"`
	Workspace string `json:"workspace"`
}

// KnowledgeHistory reads every saved conversation under the named places root,
// across projects and including archives. Task journals are never candidates.
func KnowledgeHistory(placesRoot string) knowledge.History {
	return knowledgeHistory{root: placesRoot, cache: &knowledgeHistoryCache{}}
}
func (h knowledgeHistory) Inventory(ctx context.Context) ([]knowledge.Journal, error) {
	var previous map[string]knowledgeHistoryFile
	current := make(map[string]knowledgeHistoryFile)
	if h.cache != nil {
		h.cache.mu.Lock()
		defer h.cache.mu.Unlock()
		previous = h.cache.files
		defer func() { h.cache.files = current }()
	}
	readRecord := func(path string, info os.FileInfo, header bool) (knowledgeHistoryRecord, error) {
		if cached, ok := previous[path]; ok && os.SameFile(cached.info, info) && cached.info.Size() == info.Size() && cached.info.ModTime() == info.ModTime() && cached.info.Mode() == info.Mode() {
			current[path] = cached
			return cached.record, nil
		}
		file, err := os.Open(path)
		if err != nil {
			return knowledgeHistoryRecord{}, err
		}
		defer file.Close()
		var raw []byte
		if header {
			raw, err = readKnowledgeLine(bufio.NewReader(file))
		} else {
			raw, err = io.ReadAll(file)
		}
		if err != nil && err != io.EOF {
			return knowledgeHistoryRecord{}, err
		}
		var record knowledgeHistoryRecord
		if json.Unmarshal(raw, &record) != nil {
			record = knowledgeHistoryRecord{}
		}
		current[path] = knowledgeHistoryFile{info: info, record: record}
		return record, nil
	}
	buckets, err := os.ReadDir(h.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	journals := []knowledge.Journal{}
	seen := map[string]bool{}
	add := func(path, session, project string) error {
		if seen[path] {
			return nil
		}
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		header, readErr := readRecord(path, info, true)
		if readErr != nil {
			seen[path] = true
			journals = append(journals, knowledge.Journal{ID: path, Session: session, Project: project, Size: info.Size(), Modified: info.ModTime(), Error: readErr.Error()})
			return nil
		}
		if header.Type == "session" {
			if header.ID != "" {
				session = header.ID
			}
			if header.Cwd != "" {
				project = header.Cwd
			}
		}
		seen[path] = true
		journals = append(journals, knowledge.Journal{ID: path, Session: session, Project: project, Size: info.Size(), Modified: info.ModTime()})
		return nil
	}
	for _, bucket := range buckets {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if !bucket.IsDir() {
			continue
		}
		dir := filepath.Join(h.root, bucket.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			path := filepath.Join(dir, entry.Name())
			if !entry.IsDir() {
				if strings.HasSuffix(entry.Name(), ".jsonl") {
					if err = add(path, strings.TrimSuffix(entry.Name(), ".jsonl"), ""); err != nil {
						return nil, err
					}
				}
				continue
			}
			place := Place{Dir: cell.Copied(path)}
			if _, err = os.Stat(filepath.Join(place.Dir, conversationDeletedFile)); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return nil, err
			}
			var meta knowledgeHistoryRecord
			metaPath := filepath.Join(place.Dir, placeMeta)
			metaInfo, metaErr := os.Stat(metaPath)
			if metaErr == nil {
				meta, metaErr = readRecord(metaPath, metaInfo, false)
				if meta.ID == "" {
					meta.Workspace = ""
				}
			}
			if metaErr != nil && !os.IsNotExist(metaErr) {
				journals = append(journals, knowledge.Journal{ID: place.Transcript(), Session: place.ID(), Error: metaErr.Error()})
				continue
			}
			if err = add(place.Transcript(), place.ID(), meta.Workspace); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(journals, func(i, j int) bool { return journals[i].ID < journals[j].ID })
	return journals, nil
}
func (h knowledgeHistory) Read(ctx context.Context, journal knowledge.Journal, cursor knowledge.JournalCursor) (knowledge.JournalBatch, error) {
	result := knowledge.JournalBatch{Cursor: cursor}
	file, err := os.Open(journal.ID)
	if err != nil {
		return result, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return result, err
	}
	if info.Size() < cursor.Offset {
		return result, errors.New("knowledge source journal was truncated")
	}
	prefixHash := func(length int64) (string, error) {
		bytes := make([]byte, min(length, 4096))
		_, err := file.ReadAt(bytes, 0)
		if err != nil && err != io.EOF {
			return "", err
		}
		sum := sha256.Sum256(bytes)
		return hex.EncodeToString(sum[:]), nil
	}
	if cursor.Offset > 0 && cursor.PrefixHash != "" {
		digest, err := prefixHash(cursor.Offset)
		if err != nil {
			return result, err
		}
		if digest != cursor.PrefixHash {
			return result, errors.New("knowledge source journal changed before its saved cursor")
		}
	}
	if cursor.Offset == 0 {
		// Preserve nearby previous-scope anchors without reading the whole chat
		// before its first batch. Metadata remains the fallback for older anchors.
		probe := bufio.NewReader(io.LimitReader(file, 16*1024*1024))
		first, readErr := readKnowledgeLine(probe)
		var header sessionHeader
		if readErr == nil && json.Unmarshal(first, &header) == nil && header.Type == "session" && header.Cwd == "" {
			for lines := 1; lines < 256; lines++ {
				if err = ctx.Err(); err != nil {
					return result, err
				}
				line, readErr := readKnowledgeLine(probe)
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					return result, readErr
				}
				var entry sessionEntry
				if json.Unmarshal(line, &entry) == nil && entry.Type == "knowledge_scope" {
					if entry.KnowledgePreviousProject != "" {
						result.Cursor.Scope = entry.KnowledgePreviousProject
					} else if entry.KnowledgeProject != "" {
						result.Cursor.Scope = entry.KnowledgeProject
					}
					break
				}
			}
		}
	}
	if _, err = file.Seek(cursor.Offset, io.SeekStart); err != nil {
		return result, err
	}
	reader := bufio.NewReaderSize(file, 64*1024)
	sessionID := journal.Session
	for lines := 0; lines < 256 && len(result.Sources) < 64; lines++ {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		line, readErr := readKnowledgeLine(reader)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return result, readErr
		}
		offset := result.Cursor.Offset
		result.Cursor.Offset += int64(len(line))
		var entry sessionEntry
		if err = json.Unmarshal(line, &entry); err != nil {
			return result, fmt.Errorf("knowledge journal offset %d: invalid record", offset)
		}
		switch entry.Type {
		case "session":
			var header sessionHeader
			if err = json.Unmarshal(line, &header); err != nil {
				return result, err
			}
			if header.Version > sessionFileVersion {
				return result, errors.New("knowledge source journal uses a newer format")
			}
			if header.ID != "" {
				sessionID = header.ID
			}
			if header.Cwd != "" {
				result.Cursor.Scope = header.Cwd
			}
		case "knowledge_scope":
			result.Cursor.Scope = entry.KnowledgeProject
		case "compaction":
			result.Cursor.Skip = entry.Window
		case "message":
			if result.Cursor.Skip > 0 {
				result.Cursor.Skip--
				continue
			}
			mark := entry.Presentation
			if mark == nil {
				mark = legacyPresentation(entry)
			}
			if entry.Note || strings.TrimSpace(entry.Content) == "" || (entry.Role != "user" && entry.Role != "assistant") {
				continue
			}
			if entry.Role == "assistant" && (len(entry.ToolCalls) > 0 || (mark != nil && mark.Audience != "human")) {
				continue
			}
			source := knowledge.Source{ID: fmt.Sprintf("%s:journal:%d", sessionID, offset), Session: sessionID, Project: result.Cursor.Scope, Role: entry.Role, TS: entry.Timestamp, Text: entry.Content}
			result.Sources = append(result.Sources, source)
		}
	}
	result.Cursor.PrefixHash, err = prefixHash(result.Cursor.Offset)
	return result, err
}
func readKnowledgeLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > 16*1024*1024 {
			return nil, errors.New("knowledge journal record exceeds 16 MiB")
		}
		line = append(line, part...)
		if err == bufio.ErrBufferFull {
			continue
		}
		return line, err
	}
}
