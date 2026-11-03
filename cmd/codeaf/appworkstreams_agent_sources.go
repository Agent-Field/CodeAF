package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workstream"
	"github.com/Agent-Field/codeaf/internal/workstream/pagedoc"
)

var errWorkstreamSourceLink = errors.New("source is a link")

func (d *workstreamAgentDoor) ProposeSource(ctx context.Context, ask session.WorkstreamSourceProposal) (session.WorkstreamSourceProposed, error) {
	s, w, err := d.workstreamOwner(ctx, true)
	if err != nil {
		return session.WorkstreamSourceProposed{}, err
	}
	item, already, change, err := s.ProposeSource(ctx, w.ID, ask.SourceAdd(), ask.Why)
	if err != nil {
		return session.WorkstreamSourceProposed{}, workstreamAgentError(err)
	}
	d.backend.workstreamChanged(change)
	return session.WorkstreamSourceProposed{ID: item.ID, Already: already}, nil
}

func workstreamAgentReadSources(ctx context.Context, s *workstream.Store, id string) (json.RawMessage, error) {
	items, err := s.Sources(ctx, id)
	if err != nil {
		return nil, err
	}
	out := appwire.WorkstreamSourceListResult{Sources: []appwire.WorkstreamSourceItem{}}
	for _, item := range items {
		out.Sources = append(out.Sources, appWorkstreamSourceItem(item))
	}
	return json.Marshal(out)
}

func workstreamAgentReadSource(ctx context.Context, s *workstream.Store, id, sourceID string) (json.RawMessage, error) {
	item, err := s.GetSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if item.WorkstreamID != id {
		return nil, workstream.ErrNotFound
	}
	content, err := workstreamAgentSourceContent(ctx, s, item)
	if err != nil {
		return nil, err
	}
	return workstreamAgentSourceJSON(item, content)
}

func workstreamAgentSourceContent(ctx context.Context, s *workstream.Store, item workstream.SourceItem) (string, error) {
	switch item.Kind {
	case workstream.SourceFile:
		return workstreamAgentReadPath(item.Path, false), nil
	case workstream.SourceRepo:
		if filepath.IsAbs(item.Repo) {
			return workstreamAgentReadPath(item.Repo, true), nil
		}
	case workstream.SourceKept:
		return workstreamAgentReadKept(ctx, s, item)
	}
	return "This source is a pointer; its content is not read here.", nil
}

func workstreamAgentReadKept(ctx context.Context, s *workstream.Store, item workstream.SourceItem) (string, error) {
	switch item.Ref.Kind {
	case "page":
		page, err := workstreamAgentPage(ctx, s, item.WorkstreamID, item.Ref.ID, false)
		if err != nil {
			return "", err
		}
		if !page.DeletedAt.IsZero() {
			return "The kept page is deleted.", nil
		}
		return pagedoc.Markdown(page.Doc)
	case "record":
		records, err := s.Records(ctx, item.WorkstreamID, "", "", true)
		if err != nil {
			return "", err
		}
		for _, record := range records {
			if record.ID == item.Ref.ID {
				return record.Text, nil
			}
		}
		return "", workstream.ErrNotFound
	}
	return "This kept message or job is a pointer; its content is not read here.", nil
}

func workstreamAgentReadPath(path string, folderOnly bool) string {
	file, err := workstreamAgentOpenSourcePath(path)
	if errors.Is(err, errWorkstreamSourceLink) {
		return "The source is a link and was not read."
	}
	if err != nil {
		return "The source is unreadable."
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "The source is unreadable."
	}
	if folderOnly && !info.IsDir() {
		return "The repo folder is unreadable."
	}
	if info.IsDir() {
		return workstreamAgentReadFolder(file)
	}
	if !info.Mode().IsRegular() {
		return "The source is not a regular text file."
	}
	data, err := io.ReadAll(io.LimitReader(file, session.WorkstreamSourceReadMax+1))
	if err != nil {
		return "The file is unreadable."
	}
	cut := len(data) > session.WorkstreamSourceReadMax
	if cut {
		data = data[:session.WorkstreamSourceReadMax]
	}
	if cut {
		data = workstreamAgentUTF8Tail(data)
	}
	if !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return "The file is binary, not UTF-8 text."
	}
	if cut {
		return string(data) + "\nThe file was cut."
	}
	return string(data)
}

func workstreamAgentUTF8Tail(data []byte) []byte {
	end := len(data)
	for start := end - 1; start >= 0 && start >= end-utf8.UTFMax; start-- {
		if !utf8.RuneStart(data[start]) {
			continue
		}
		if !utf8.FullRune(data[start:]) {
			return data[:start]
		}
		break
	}
	return data
}

func workstreamAgentReadFolder(folder *os.File) string {
	names, err := folder.Readdirnames(200)
	if err != nil && err != io.EOF {
		return "The folder is unreadable."
	}
	sort.Strings(names)
	return strings.Join(names, "\n")
}

func workstreamAgentSourceJSON(item workstream.SourceItem, content string) (json.RawMessage, error) {
	out := struct {
		Source  appwire.WorkstreamSourceItem `json:"source"`
		Content string                       `json:"content"`
	}{Source: appWorkstreamSourceItem(item), Content: content}
	// Bound the encoded answer too: escapes and metadata count toward the read cap.
	for {
		raw, err := json.Marshal(out)
		if err != nil || len(raw) <= session.WorkstreamSourceReadMax {
			return raw, err
		}
		end := len(out.Content) / 2
		for end > 0 && !utf8.RuneStart(out.Content[end]) {
			end--
		}
		tail := "\nThe source read was cut."
		if item.Kind == workstream.SourceFile {
			tail = "\nThe file was cut."
		}
		out.Content = out.Content[:end] + tail
	}
}
