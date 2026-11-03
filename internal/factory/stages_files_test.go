package factory_test

import (
 "testing"

 "github.com/Agent-Field/codeaf/internal/factory"
)

func TestFileConditionsUseKnownFilesBeforeArea(t *testing.T) {
 for _, tc := range []struct { name string; item factory.Item; when string; want bool }{
  {"unknown auth", factory.Item{}, "touches auth", true},
  {"unknown ui", factory.Item{}, "has ui", true},
  {"area fallback", factory.Item{Triage: factory.Triage{Area:"AUTH"}}, "touches auth", true},
  {"known area misses", factory.Item{Triage: factory.Triage{Area:"docs"}}, "touches auth", false},
  {"forge files", factory.Item{Files: []factory.FileChange{{Path:"src/pages/a.tsx"}}}, "has ui", true},
  {"files override area", factory.Item{Triage: factory.Triage{Area:"auth"}, Change: &factory.Change{Files: []factory.ChangedFile{{Path:"docs/a.md"}}}}, "touches auth", false},
  {"measured change overrides forge", factory.Item{Files: []factory.FileChange{{Path:"docs/a.md"}}, Change: &factory.Change{Files: []factory.ChangedFile{{Path:"auth.go"}}}}, "touches auth", true},
  {"planned paths are not changes", factory.Item{PlannedFiles: []string{"docs/a.md"}}, "touches auth", true},
 } {
  t.Run(tc.name, func(t *testing.T) {
   if got := factory.Fits(factory.Stage{When:tc.when},tc.item); got != tc.want { t.Fatalf("Fits = %v, want %v",got,tc.want) }
  })
 }
}
