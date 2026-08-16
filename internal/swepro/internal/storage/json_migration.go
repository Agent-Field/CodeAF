// JSON-to-database migration — port of src/storage/json-migration.ts:17-435
// (swe-pro 3b25a1a). The database and clock are narrow interfaces so the
// filesystem decision shell can be exercised without a concrete ORM.
package storage

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ProjectTable      = "project"
	SessionTable      = "session"
	MessageTable      = "message"
	PartTable         = "part"
	TodoTable         = "todo"
	PermissionTable   = "permission"
	SessionShareTable = "session_share"
)

// Database is the subset of drizzle used by json-migration.ts.
type Database interface {
	Run(statement string) error
	Insert(table string, values []*Object) error
}

// Progress is emitted after each migrated source-file batch.
type Progress struct {
	Current int    `json:"current"`
	Total   int    `json:"total"`
	Label   string `json:"label"`
}

// MigrationOptions configures RunJSONMigration.
type MigrationOptions struct {
	Progress func(Progress)
	Now      func() time.Time
}

// MigrationStats mirrors the object returned by JsonMigration.run.
type MigrationStats struct {
	Projects    int      `json:"projects"`
	Sessions    int      `json:"sessions"`
	Messages    int      `json:"messages"`
	Parts       int      `json:"parts"`
	Todos       int      `json:"todos"`
	Permissions int      `json:"permissions"`
	Shares      int      `json:"shares"`
	Errors      []string `json:"errors"`
}

type orphanStats struct {
	sessions    int
	todos       int
	permissions int
	shares      int
}

// RunJSONMigration migrates dataDir/storage into db.
func RunJSONMigration(db Database, dataDir string, options *MigrationOptions) (MigrationStats, error) {
	stats := MigrationStats{Errors: []string{}}
	storageDir := filepath.Join(dataDir, "storage")
	if info, err := os.Stat(storageDir); err != nil || !info.IsDir() {
		return stats, nil
	}

	if err := db.Run("PRAGMA journal_mode = WAL"); err != nil {
		return stats, err
	}
	for _, pragma := range []string{
		"PRAGMA synchronous = OFF",
		"PRAGMA cache_size = 10000",
		"PRAGMA temp_store = MEMORY",
	} {
		if err := db.Run(pragma); err != nil {
			return stats, err
		}
	}

	nowFn := time.Now
	if options != nil && options.Now != nil {
		nowFn = options.Now
	}
	now := float64(nowFn().UnixMilli())
	progress := func(Progress) {}
	if options != nil && options.Progress != nil {
		progress = options.Progress
	}
	orphans := orphanStats{}

	projectFiles := scanFiles(storageDir, "project", "*.json")
	sessionFiles := scanFiles(storageDir, "session", "*", "*.json")
	messageFiles := scanFiles(storageDir, "message", "*", "*.json")
	partFiles := scanFiles(storageDir, "part", "*", "*.json")
	todoFiles := scanFiles(storageDir, "todo", "*.json")
	permFiles := scanFiles(storageDir, "permission", "*.json")
	shareFiles := scanFiles(storageDir, "session_share", "*.json")
	total := len(projectFiles) + len(sessionFiles) + len(messageFiles) + len(partFiles) +
		len(todoFiles) + len(permFiles) + len(shareFiles)
	if total < 1 {
		total = 1
	}
	current := 0
	step := func(label string, count int) {
		current = min(total, current+count)
		progress(Progress{Current: current, Total: total, Label: label})
	}
	progress(Progress{Current: current, Total: total, Label: "starting"})

	if err := db.Run("BEGIN TRANSACTION"); err != nil {
		return stats, err
	}

	projectIDs := map[string]bool{}
	for start := 0; start < len(projectFiles); start += 1000 {
		end := min(start+1000, len(projectFiles))
		batch := readBatch(projectFiles, start, end, &stats)
		values := []*Object{}
		for j, data := range batch {
			if !jsTruthy(data) {
				continue
			}
			id := strings.TrimSuffix(filepath.Base(projectFiles[start+j]), ".json")
			projectIDs[id] = true
			row := NewObject()
			row.Set("id", id)
			row.Set("worktree", nullish(objectGet(data, "worktree"), "/"))
			if value, ok := objectGetOK(data, "vcs"); ok {
				row.Set("vcs", value)
			}
			if value, ok := nullishPresent(objectGetOK(data, "name")); ok {
				row.Set("name", value)
			}
			if value, ok := pathGetOK(data, "icon", "url"); ok {
				row.Set("icon_url", value)
			}
			if value, ok := pathGetOK(data, "icon", "override"); ok {
				row.Set("icon_url_override", value)
			}
			if value, ok := pathGetOK(data, "icon", "color"); ok {
				row.Set("icon_color", value)
			}
			row.Set("time_created", nullish(pathGet(data, "time", "created"), now))
			row.Set("time_updated", nullish(pathGet(data, "time", "updated"), now))
			if value, ok := pathGetOK(data, "time", "initialized"); ok {
				row.Set("time_initialized", value)
			}
			row.Set("sandboxes", nullish(objectGet(data, "sandboxes"), []any{}))
			if value, ok := objectGetOK(data, "commands"); ok {
				row.Set("commands", value)
			}
			values = append(values, row)
		}
		stats.Projects += insertBatch(db, ProjectTable, values, "project", &stats)
		step("projects", end-start)
	}

	sessionProjects := make([]string, len(sessionFiles))
	for i, file := range sessionFiles {
		sessionProjects[i] = filepath.Base(filepath.Dir(file))
	}
	sessionIDs := map[string]bool{}
	for start := 0; start < len(sessionFiles); start += 1000 {
		end := min(start+1000, len(sessionFiles))
		batch := readBatch(sessionFiles, start, end, &stats)
		values := []*Object{}
		for j, data := range batch {
			if !jsTruthy(data) {
				continue
			}
			id := strings.TrimSuffix(filepath.Base(sessionFiles[start+j]), ".json")
			projectID := sessionProjects[start+j]
			if !projectIDs[projectID] {
				orphans.sessions++
				continue
			}
			sessionIDs[id] = true
			row := NewObject()
			row.Set("id", id)
			row.Set("project_id", projectID)
			row.Set("parent_id", nullish(objectGet(data, "parentID"), nil))
			row.Set("slug", nullish(objectGet(data, "slug"), ""))
			row.Set("directory", nullish(objectGet(data, "directory"), ""))
			row.Set("path", nullish(objectGet(data, "path"), nil))
			row.Set("title", nullish(objectGet(data, "title"), ""))
			row.Set("version", nullish(objectGet(data, "version"), ""))
			row.Set("share_url", nullish(pathGet(data, "share", "url"), nil))
			row.Set("summary_additions", nullish(pathGet(data, "summary", "additions"), nil))
			row.Set("summary_deletions", nullish(pathGet(data, "summary", "deletions"), nil))
			row.Set("summary_files", nullish(pathGet(data, "summary", "files"), nil))
			row.Set("summary_diffs", nullish(pathGet(data, "summary", "diffs"), nil))
			row.Set("revert", nullish(objectGet(data, "revert"), nil))
			row.Set("permission", nullish(objectGet(data, "permission"), nil))
			row.Set("time_created", nullish(pathGet(data, "time", "created"), now))
			row.Set("time_updated", nullish(pathGet(data, "time", "updated"), now))
			row.Set("time_compacting", nullish(pathGet(data, "time", "compacting"), nil))
			row.Set("time_archived", nullish(pathGet(data, "time", "archived"), nil))
			values = append(values, row)
		}
		stats.Sessions += insertBatch(db, SessionTable, values, "session", &stats)
		step("sessions", end-start)
	}

	allMessageFiles := []string{}
	allMessageSessions := []string{}
	messageSessions := map[string]string{}
	for _, file := range messageFiles {
		sessionID := filepath.Base(filepath.Dir(file))
		if !sessionIDs[sessionID] {
			continue
		}
		allMessageFiles = append(allMessageFiles, file)
		allMessageSessions = append(allMessageSessions, sessionID)
	}
	for start := 0; start < len(allMessageFiles); start += 1000 {
		end := min(start+1000, len(allMessageFiles))
		batch := readBatch(allMessageFiles, start, end, &stats)
		values := []*Object{}
		for j, data := range batch {
			if !jsTruthy(data) {
				continue
			}
			file := allMessageFiles[start+j]
			id := strings.TrimSuffix(filepath.Base(file), ".json")
			sessionID := allMessageSessions[start+j]
			messageSessions[id] = sessionID
			if rest, ok := data.(*Object); ok {
				rest.Delete("id")
				rest.Delete("sessionID")
			}
			row := NewObject()
			row.Set("id", id)
			row.Set("session_id", sessionID)
			row.Set("time_created", nullish(pathGet(data, "time", "created"), now))
			row.Set("time_updated", nullish(pathGet(data, "time", "updated"), now))
			row.Set("data", data)
			values = append(values, row)
		}
		stats.Messages += insertBatch(db, MessageTable, values, "message", &stats)
		step("messages", end-start)
	}

	for start := 0; start < len(partFiles); start += 1000 {
		end := min(start+1000, len(partFiles))
		batch := readBatch(partFiles, start, end, &stats)
		values := []*Object{}
		for j, data := range batch {
			if !jsTruthy(data) {
				continue
			}
			file := partFiles[start+j]
			id := strings.TrimSuffix(filepath.Base(file), ".json")
			messageID := filepath.Base(filepath.Dir(file))
			sessionID := messageSessions[messageID]
			if sessionID == "" {
				stats.Errors = append(stats.Errors, "part missing message session: "+file)
				continue
			}
			if !sessionIDs[sessionID] {
				continue
			}
			if rest, ok := data.(*Object); ok {
				rest.Delete("id")
				rest.Delete("messageID")
				rest.Delete("sessionID")
			}
			row := NewObject()
			row.Set("id", id)
			row.Set("message_id", messageID)
			row.Set("session_id", sessionID)
			row.Set("time_created", nullish(pathGet(data, "time", "created"), now))
			row.Set("time_updated", nullish(pathGet(data, "time", "updated"), now))
			row.Set("data", data)
			values = append(values, row)
		}
		stats.Parts += insertBatch(db, PartTable, values, "part", &stats)
		step("parts", end-start)
	}

	todoSessions := make([]string, len(todoFiles))
	for i, file := range todoFiles {
		todoSessions[i] = strings.TrimSuffix(filepath.Base(file), ".json")
	}
	for start := 0; start < len(todoFiles); start += 1000 {
		end := min(start+1000, len(todoFiles))
		batch := readBatch(todoFiles, start, end, &stats)
		values := []*Object{}
		for j, data := range batch {
			if !jsTruthy(data) {
				continue
			}
			sessionID := todoSessions[start+j]
			if !sessionIDs[sessionID] {
				orphans.todos++
				continue
			}
			todos, ok := data.([]any)
			if !ok {
				stats.Errors = append(stats.Errors, "todo not an array: "+todoFiles[start+j])
				continue
			}
			for position, todo := range todos {
				content := objectGet(todo, "content")
				status := objectGet(todo, "status")
				priority := objectGet(todo, "priority")
				if !jsTruthy(content) || !jsTruthy(status) || !jsTruthy(priority) {
					continue
				}
				row := NewObject()
				row.Set("session_id", sessionID)
				row.Set("content", content)
				row.Set("status", status)
				row.Set("priority", priority)
				row.Set("position", float64(position))
				row.Set("time_created", now)
				row.Set("time_updated", now)
				values = append(values, row)
			}
		}
		stats.Todos += insertBatch(db, TodoTable, values, "todo", &stats)
		step("todos", end-start)
	}

	permProjects := make([]string, len(permFiles))
	for i, file := range permFiles {
		permProjects[i] = strings.TrimSuffix(filepath.Base(file), ".json")
	}
	for start := 0; start < len(permFiles); start += 1000 {
		end := min(start+1000, len(permFiles))
		batch := readBatch(permFiles, start, end, &stats)
		values := []*Object{}
		for j, data := range batch {
			if !jsTruthy(data) {
				continue
			}
			projectID := permProjects[start+j]
			if !projectIDs[projectID] {
				orphans.permissions++
				continue
			}
			row := NewObject()
			row.Set("project_id", projectID)
			row.Set("data", data)
			values = append(values, row)
		}
		stats.Permissions += insertBatch(db, PermissionTable, values, "permission", &stats)
		step("permissions", end-start)
	}

	shareSessions := make([]string, len(shareFiles))
	for i, file := range shareFiles {
		shareSessions[i] = strings.TrimSuffix(filepath.Base(file), ".json")
	}
	for start := 0; start < len(shareFiles); start += 1000 {
		end := min(start+1000, len(shareFiles))
		batch := readBatch(shareFiles, start, end, &stats)
		values := []*Object{}
		for j, data := range batch {
			if !jsTruthy(data) {
				continue
			}
			sessionID := shareSessions[start+j]
			if !sessionIDs[sessionID] {
				orphans.shares++
				continue
			}
			id := objectGet(data, "id")
			secret := objectGet(data, "secret")
			url := objectGet(data, "url")
			if !jsTruthy(id) || !jsTruthy(secret) || !jsTruthy(url) {
				stats.Errors = append(stats.Errors, "session_share missing id/secret/url: "+shareFiles[start+j])
				continue
			}
			row := NewObject()
			row.Set("session_id", sessionID)
			row.Set("id", id)
			row.Set("secret", secret)
			row.Set("url", url)
			values = append(values, row)
		}
		stats.Shares += insertBatch(db, SessionShareTable, values, "session_share", &stats)
		step("shares", end-start)
	}

	if err := db.Run("COMMIT"); err != nil {
		return stats, err
	}
	progress(Progress{Current: total, Total: total, Label: "complete"})
	return stats, nil
}

func scanFiles(root string, parts ...string) []string {
	pattern := filepath.Join(append([]string{root}, parts...)...)
	files, err := filepath.Glob(pattern)
	if err != nil {
		return []string{}
	}
	return files
}

func readBatch(files []string, start, end int, stats *MigrationStats) []any {
	items := make([]any, end-start)
	for i := start; i < end; i++ {
		value, err := readOrderedFile(files[i])
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("failed to read %s: %v", files[i], err))
			continue
		}
		items[i-start] = value
	}
	return items
}

func insertBatch(db Database, table string, values []*Object, label string, stats *MigrationStats) int {
	if len(values) == 0 {
		return 0
	}
	if err := db.Insert(table, values); err != nil {
		stats.Errors = append(stats.Errors, fmt.Sprintf("failed to migrate %s batch: %v", label, err))
		return 0
	}
	return len(values)
}

func objectGet(v any, key string) any {
	value, _ := objectGetOK(v, key)
	return value
}

func objectGetOK(v any, key string) (any, bool) {
	o, ok := v.(*Object)
	if !ok {
		return nil, false
	}
	return o.Get(key)
}

func pathGet(v any, keys ...string) any {
	value, _ := pathGetOK(v, keys...)
	return value
}

func pathGetOK(v any, keys ...string) (any, bool) {
	current := v
	var ok bool
	for _, key := range keys {
		current, ok = objectGetOK(current, key)
		if !ok || current == nil {
			return nil, false
		}
	}
	return current, true
}

func nullish(value, fallback any) any {
	if value == nil {
		return fallback
	}
	return value
}

func nullishPresent(value any, present bool) (any, bool) {
	if !present || value == nil {
		return nil, false
	}
	return value, true
}

func jsTruthy(v any) bool {
	switch value := v.(type) {
	case nil:
		return false
	case bool:
		return value
	case string:
		return value != ""
	case float64:
		return value != 0 && !math.IsNaN(value)
	default:
		return true
	}
}
