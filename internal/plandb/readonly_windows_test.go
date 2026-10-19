//go:build windows

package plandb

import (
	"path/filepath"
	"testing"
)

// R2 proves both read-only doors see the real writer's committed root and task
// at a native Windows path, without adopting or rewriting the plan themselves.
func TestWindowsPlanReadersNativePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dir with space é", "plandb.db")
	s, err := Open(path, "windows", "root", "héllo", "read the committed plan")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.AddMany([]TaskSpec{{ID: "work", ParentID: "root", Title: "do the work"}}); err != nil {
		t.Fatal(err)
	}
	tasks, err := ReadTasks(path)
	if err != nil || tasks.RootID != "root" || len(tasks.Tasks) != 2 || tasks.Tasks[1].Title != "do the work" {
		t.Fatalf("tasks = %+v, %v", tasks, err)
	}
	preview, err := ReadRootPreview(path)
	if err != nil || preview.Title != "héllo" || preview.Description != "read the committed plan" || preview.CreatedAt.IsZero() {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
}
