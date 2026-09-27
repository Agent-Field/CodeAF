package tui3

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/config"
)

func TestAsyncUserCommandResponsesStayVisibleInCleanConversation(t *testing.T) {
	cases := []struct {
		name, want string
		msg        tea.Msg
	}{
		{"cache status", "cache holds", cacheNoteMsg{line: "the cache holds 12 MB"}},
		{"cache confirmation", "type /cache clean now", cacheNoteMsg{line: "type /cache clean now to go ahead"}},
		{"landing", "branch landed", landNoteMsg{line: "branch landed"}},
		{"compaction error", "compact failed", compactedMsg{err: errors.New("disk busy")}},
		{"export", "exported", exportedMsg{path: "/tmp/conversation.md"}},
		{"export refusal", "already there", exportedMsg{path: "/tmp/conversation.md", err: fs.ErrExist}},
		{"export error", "export failed", exportedMsg{err: errors.New("disk full")}},
		{"copy", "copied", copiedMsg{path: "/tmp/answer.md"}},
		{"copy refusal", filesThereWord, copiedMsg{path: "/tmp/answer.md", err: fs.ErrExist}},
		{"copy error", filesCopyFailedWord, copiedMsg{err: errors.New("disk full")}},
		{"picture error", filesOpenFailedWord, pictureOpenedMsg{path: "/tmp/image.png", err: errors.New("no viewer")}},
		{"model refresh error", ModelsFetchFailed, modelsFetchedMsg{err: errors.New("offline")}},
		{"model refresh", modelsNoteHead, modelsFetchedMsg{rows: pickerCatalog, at: time.Unix(100, 0)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.workMode = config.WorkFold
			drive(t, a, tc.msg)
			page := strings.Join(plainRows(a), "\n")
			if !strings.Contains(page, tc.want) {
				t.Fatalf("requested result hidden: %s", page)
			}
			for _, r := range rows(a) {
				if r.hit == hitWorkFold {
					t.Fatalf("direct answer acquired a work disclosure: %s", page)
				}
			}
		})
	}
}
