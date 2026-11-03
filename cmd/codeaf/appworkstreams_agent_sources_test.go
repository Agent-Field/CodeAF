package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

func sessionWorkstreamSourceProposalForTest() session.WorkstreamSourceProposal {
	return session.WorkstreamSourceProposal{Kind: "link", URL: workstreamPtr("https://example.com/guide"), Why: "Read the guide."}
}

// WP-12 (g): the agent adds only a proposal, lists its exact stored fields and
// reads a text source; duplicate and invalid proposals add nothing else.
func TestWorkstreamAgentSourceProposalsAndReads(t *testing.T) {
	b, rec, w, door := workstreamAgentFixture(t)
	ctx := context.Background()
	path := workstreamSourcesFile(t)
	ask := session.WorkstreamSourceProposal{Kind: "file", Path: &path, Why: "Read recorded usage."}
	before := workstreamLimitSnapshot(t, b, w.ID)
	events := len(workstreamEvents(rec))
	proposal, err := door.ProposeSource(ctx, ask)
	if err != nil || proposal.ID == "" || proposal.Already {
		t.Fatalf("proposal: %+v %v", proposal, err)
	}
	again, err := door.ProposeSource(ctx, ask)
	if err != nil || !again.Already || again.ID != proposal.ID || len(workstreamEvents(rec)) != events+1 {
		t.Fatalf("duplicate: %+v %v", again, err)
	}
	listed, err := door.Read(ctx, session.WorkstreamReadAsk{What: "sources"})
	if err != nil {
		t.Fatal(err)
	}
	var list appwire.WorkstreamSourceListResult
	if err := json.Unmarshal(listed, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Sources) != 1 {
		t.Fatalf("list: %s", listed)
	}
	item := list.Sources[0]
	if item.ID != proposal.ID || item.Kind != "file" || item.Path == nil || *item.Path != path || !item.Proposed || item.ReadByJobs || item.AddedBy != "agent" || item.Why == nil || *item.Why != ask.Why {
		t.Fatalf("proposal row: %+v", item)
	}
	raw, err := door.Read(ctx, session.WorkstreamReadAsk{What: "source", SourceID: proposal.ID})
	if err != nil {
		t.Fatal(err)
	}
	var read struct {
		Source  appwire.WorkstreamSourceItem `json:"source"`
		Content string                       `json:"content"`
	}
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	if read.Content != "Recorded usage.\n" || read.Source.ID != proposal.ID {
		t.Fatalf("source read: %s", raw)
	}
	for _, bad := range []session.WorkstreamSourceProposal{{Kind: "file", Path: workstreamPtr("relative"), Why: "Reason"}, {Kind: "link", URL: workstreamPtr("file:///etc/passwd"), Why: "Reason"}, {Kind: "link", URL: workstreamPtr("https://example.com/other"), Why: ""}} {
		if _, err := door.ProposeSource(ctx, bad); err == nil {
			t.Fatal("invalid proposal succeeded")
		}
	}
	after := workstreamLimitSnapshot(t, b, w.ID)
	if before.Pages != after.Pages || before.Records != after.Records || before.Versions != after.Versions || !reflect.DeepEqual(before.Row.Members, after.Row.Members) || len(workstreamEvents(rec)) != events+1 {
		t.Fatal("source proposals changed unrelated state")
	}
	if _, err := door.Read(ctx, session.WorkstreamReadAsk{What: "source", SourceID: "missing"}); err == nil {
		t.Fatal("unknown source read succeeded")
	}
	other := workstreamCreateForTest(t, b, "Other source owner")
	foreign, err := b.WorkstreamSourceAdd(appwire.WorkstreamSourceAddParams{WorkstreamID: other.ID, Kind: "file", Path: &path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := door.Read(ctx, session.WorkstreamReadAsk{What: "source", SourceID: foreign.Source.ID}); err == nil {
		t.Fatal("foreign source read succeeded")
	}
}

// WP-12 (g): reads use only the stored file/folder path, bound content and
// entry names, and explain binary or missing files in one line.
func TestWorkstreamAgentSourceFileReadBounds(t *testing.T) {
	b, _, w, door := workstreamAgentFixture(t)
	ctx := context.Background()
	path := workstreamSourcesFile(t)
	file, err := b.WorkstreamSourceAdd(appwire.WorkstreamSourceAddParams{WorkstreamID: w.ID, Kind: "file", Path: &path})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		data string
		want string
	}{
		{strings.Repeat("é", session.WorkstreamSourceReadMax), "The file was cut."},
		{strings.Repeat("\"", session.WorkstreamSourceReadMax), "The file was cut."},
		{"binary\x00bytes", "The file is binary, not UTF-8 text."},
		{string([]byte{255, 254}), "The file is binary, not UTF-8 text."},
	} {
		if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		raw, err := door.Read(ctx, session.WorkstreamReadAsk{What: "source", SourceID: file.Source.ID})
		var answer struct {
			Content string `json:"content"`
		}
		if err != nil || len(raw) > session.WorkstreamSourceReadMax || !utf8.Valid(raw) {
			t.Fatalf("bounded source: %d %v", len(raw), err)
		}
		if err := json.Unmarshal(raw, &answer); err != nil || !strings.Contains(answer.Content, tc.want) {
			t.Fatalf("content: %q %v", answer.Content, err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	raw, err := door.Read(ctx, session.WorkstreamReadAsk{What: "source", SourceID: file.Source.ID})
	if err != nil || !strings.Contains(string(raw), "unreadable") {
		t.Fatalf("missing file: %s %v", raw, err)
	}
	folder := t.TempDir()
	for i := 0; i < 205; i++ {
		if err := os.WriteFile(filepath.Join(folder, fmt.Sprintf("entry-%03d", i)), []byte("child text must not be read"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"file", "repo"} {
		ask := appwire.WorkstreamSourceAddParams{WorkstreamID: w.ID, Kind: kind}
		if kind == "file" {
			ask.Path = &folder
		} else {
			ask.Repo = &folder
		}
		made, err := b.WorkstreamSourceAdd(ask)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := door.Read(ctx, session.WorkstreamReadAsk{What: "source", SourceID: made.Source.ID})
		var answer struct {
			Content string `json:"content"`
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &answer); err != nil || len(strings.Split(answer.Content, "\n")) != 200 || strings.Contains(answer.Content, "child text") {
			t.Fatalf("folder names: %s %v", raw, err)
		}
	}
}

// WP-12 Fix 1 Major 1 (g): final-component links never expose another file,
// whether present at admission or planted after an ordinary file was added.
func TestWorkstreamAgentMajor1ReadConfinementG(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprintf("replacement=%t", replacement), func(t *testing.T) {
			b, _, w, door := workstreamAgentFixture(t)
			path := workstreamSourcesFile(t)
			target := filepath.Join(t.TempDir(), "other.txt")
			const otherText = "Bytes from another readable file must never appear."
			if err := os.WriteFile(target, []byte(otherText), 0600); err != nil {
				t.Fatal(err)
			}
			plantLink := func() {
				t.Helper()
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if !replacement {
				plantLink()
			}
			item, err := b.WorkstreamSourceAdd(appwire.WorkstreamSourceAddParams{WorkstreamID: w.ID, Kind: "file", Path: &path})
			if err != nil {
				t.Fatal(err)
			}
			if replacement {
				plantLink()
			}
			raw, err := door.Read(context.Background(), session.WorkstreamReadAsk{What: "source", SourceID: item.Source.ID})
			if err != nil {
				t.Fatal(err)
			}
			var answer struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(raw, &answer); err != nil {
				t.Fatal(err)
			}
			if answer.Content != "The source is a link and was not read." || strings.Contains(string(raw), otherText) {
				t.Fatalf("link read: %s", raw)
			}
		})
	}
}

// WP-12 Fix 1 Major 1 (g): intermediate directory links remain usable, and
// folder reads return entry names without following file or directory links.
func TestWorkstreamAgentMajor1ReadConfinementGIntermediateAndEntries(t *testing.T) {
	b, _, w, door := workstreamAgentFixture(t)
	folder := t.TempDir()
	path := filepath.Join(folder, "text.txt")
	if err := os.WriteFile(path, []byte("Stored file text."), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "folder")
	if err := os.Symlink(folder, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for name, target := range map[string]string{"file-link": path, "folder-link": t.TempDir(), "missing-link": filepath.Join(folder, "missing")} {
		if err := os.Symlink(target, filepath.Join(folder, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		kind, path, want string
	}{
		{"file", filepath.Join(link, "text.txt"), "Stored file text."},
		{"file", folder, "file-link\nfolder-link\nmissing-link\ntext.txt"},
		{"repo", folder, "file-link\nfolder-link\nmissing-link\ntext.txt"},
	} {
		ask := appwire.WorkstreamSourceAddParams{WorkstreamID: w.ID, Kind: tc.kind}
		if tc.kind == "repo" {
			ask.Repo = &tc.path
		} else {
			ask.Path = &tc.path
		}
		item, err := b.WorkstreamSourceAdd(ask)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := door.Read(context.Background(), session.WorkstreamReadAsk{What: "source", SourceID: item.Source.ID})
		var answer struct {
			Content string `json:"content"`
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &answer); err != nil || answer.Content != tc.want {
			t.Fatalf("%s read: %s %v", tc.kind, raw, err)
		}
	}
}

// WP-12 (g): kept pages render markdown, records return text, and network,
// message and job sources stay pointers with no fetch or lookup.
func TestWorkstreamAgentKeptSourcesAndPointers(t *testing.T) {
	b, _, w, door := workstreamAgentFixture(t)
	ctx := context.Background()
	page, err := b.WorkstreamPageCreate(appwire.WorkstreamPageCreateParams{WorkstreamID: w.ID, Title: "Guide", Doc: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Kept page words."}]}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	record, err := b.WorkstreamRecordPropose(appwire.WorkstreamRecordProposeParams{WorkstreamID: w.ID, Kind: "question", Text: "Kept record text."})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		ask  appwire.WorkstreamSourceAddParams
		want string
	}{
		{appwire.WorkstreamSourceAddParams{Kind: "kept", Ref: &appwire.WorkstreamKeptRef{Kind: "page", ID: page.Page.ID}}, "Kept page words."},
		{appwire.WorkstreamSourceAddParams{Kind: "kept", Ref: &appwire.WorkstreamKeptRef{Kind: "record", ID: record.Record.ID}}, "Kept record text."},
		{appwire.WorkstreamSourceAddParams{Kind: "kept", Ref: &appwire.WorkstreamKeptRef{Kind: "message", ID: "unlooked-up", ChatID: workstreamPtr("chat")}}, "pointer"},
		{appwire.WorkstreamSourceAddParams{Kind: "kept", Ref: &appwire.WorkstreamKeptRef{Kind: "factory_job", ID: "7", Factory: workstreamPtr("f")}}, "pointer"},
		{appwire.WorkstreamSourceAddParams{Kind: "repo", Repo: workstreamPtr("owner/name")}, "pointer"},
		{appwire.WorkstreamSourceAddParams{Kind: "issue", Repo: workstreamPtr("owner/name"), Number: workstreamPtr(412)}, "pointer"},
		{appwire.WorkstreamSourceAddParams{Kind: "link", URL: workstreamPtr("https://example.com")}, "pointer"},
	}
	for _, tc := range cases {
		tc.ask.WorkstreamID = w.ID
		made, err := b.WorkstreamSourceAdd(tc.ask)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := door.Read(ctx, session.WorkstreamReadAsk{What: "source", SourceID: made.Source.ID})
		if err != nil || !strings.Contains(string(raw), tc.want) {
			t.Fatalf("kept read: %s %v", raw, err)
		}
	}
}

// WP-12 (g), (h): a done state committed after resolving the agent door is
// still enforced by the store, with an empty change receipt.
func TestWorkstreamSourceProposalDoneAfterResolution(t *testing.T) {
	_, _, _, door := workstreamAgentFixture(t)
	ctx := context.Background()
	s, w, err := door.(*workstreamAgentDoor).workstreamOwner(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.MarkDone(ctx, w.ID, ""); err != nil {
		t.Fatal(err)
	}
	_, _, change, err := s.ProposeSource(ctx, w.ID, workstream.SourceAdd{Kind: "link", URL: workstreamPtr("https://example.com")}, "Read it.")
	if workstreamAgentError(err) != session.ErrWorkstreamDone || len(change.Parts) != 0 {
		t.Fatalf("late done: %+v %v", change, err)
	}
}
