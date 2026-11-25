package schema

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func (c *checker) contentEntry(file string) (string, ContentManifest) {
	dir, ext, ok := fileInGroup(file)
	if ok {
		for name, entry := range c.kit.Registries.Content {
			if entry.Directory == dir && entry.Extension == ext && (entry.Filename == "" || FileID(file) == entry.Filename) {
				return name, entry
			}
		}
	}
	return "", ContentManifest{}
}

func (c *checker) checkContent() {
	for _, file := range sortedKeys(c.def.Files) {
		name, entry := c.contentEntry(file)
		if name == "" {
			continue
		}
		raw := c.def.Files[file]
		var value any = string(raw)
		switch entry.Encoding {
		case "json":
			if !c.schemaCheck(file, raw, schemas().entries["content/"+name]) {
				continue
			}
			value = object(raw)
		case "csv":
			r := csv.NewReader(bytes.NewReader(raw))
			for {
				_, err := r.Read()
				if err == io.EOF {
					break
				}
				if err != nil {
					c.add(file, "", CodeContent, "Invalid sheet: %v", err)
					break
				}
			}
		}
		for _, rule := range entry.Rules {
			for _, picked := range ruleValues(value, rule.Path) {
				c.checkContentRule(file, value, rule, picked)
			}
		}
	}
}

func (c *checker) checkContentRule(file string, root any, rule ManifestRule, picked selectedValue) {
	parent, _ := root.(map[string]any)
	switch rule.Check {
	case "record-reference":
		c.checkContentRecord(file, picked.path, asString(picked.value))
	case "record-read":
		c.checkRecordAccess(file, picked.path, asString(picked.value), "r")
	case "unique-ids":
		c.checkUniqueIDs(file, picked.path, picked.value, CodeContent)
	case "timeline-order":
		c.checkTimeline(file, picked.path, picked.value)
	case "edge-endpoints":
		ids := map[string]bool{}
		for _, node := range objectRows(parent[rule.Options]) {
			ids[asString(node["id"])] = true
		}
		for i, link := range objectRows(picked.value) {
			if !ids[asString(link["from"])] || !ids[asString(link["to"])] {
				c.add(file, fmt.Sprintf("%s/%d", picked.path, i), CodeContent, "Both ends of a link must name nodes in the graph.")
			}
		}
	case "frame":
		if parent["frame"] == "board" {
			return
		}
		size, _ := parent["size"].([]any)
		if len(size) != 2 {
			return
		}
		for i, box := range objectRows(picked.value) {
			x, y := numeric(box["x"]), numeric(box["y"])
			if x < 0 || y < 0 || x+numeric(box["w"]) > numeric(size[0]) || y+numeric(box["h"]) > numeric(size[1]) {
				c.add(file, fmt.Sprintf("%s/%d", picked.path, i), CodeContent, "The box lies outside its frame.")
			}
		}
	case "document-parts":
		head, ids, ok := documentParts(asString(picked.value))
		if !ok {
			c.add(file, "", CodeContent, "Markdown part markers must match its part-ids front matter in order.")
			return
		}
		for _, ref := range head.Links {
			c.checkContentRecord(file, "/links", ref)
		}
		if len(ids) == 0 {
			c.add(file, "", CodeContent, "Add at least one Markdown part.")
		}
	case "sheets":
		entries, _ := picked.value.(map[string]any)
		for _, name := range sortedKeys(entries) {
			c.checkContentBinding(file, picked.path+"/"+name, "sheets/"+name+".csv", "sheet", "")
		}
	}
}

func (c *checker) checkContentBinding(file, at, ref, expected, partID string) {
	key, entry := c.contentEntry(ref)
	raw, found := c.def.Files[ref]
	if !found || key == "" || expected != "" && key != expected {
		c.add(file, at, CodeContentBinding, "%q is not an available %s content file.", ref, expected)
		return
	}
	if partID == "" {
		return
	}
	switch entry.PartModel {
	case "marker":
		_, ids, ok := documentParts(string(raw))
		if ok && contains(ids, partID) {
			return
		}
	case "file":
		if FileID(ref) == partID {
			return
		}
	default:
		for _, rule := range entry.Rules {
			if rule.Check != "unique-ids" {
				continue
			}
			for _, selected := range ruleValues(object(raw), rule.Path+"/*/id") {
				if asString(selected.value) == partID {
					return
				}
			}
		}
	}
	c.add(file, at, CodeContentBinding, "%s has no content part named %q.", ref, partID)
}

func (c *checker) checkContentRecord(file, at, ref string) {
	typ, id, ok := strings.Cut(ref, "/")
	if !ok || id == "" {
		c.add(file, at, CodeContent, "A record reference must be written as type/id.")
		return
	}
	if c.checkRecordAccess(file, at, typ, "r") {
		if rows, sampled := c.sampleIDs[typ]; sampled && !rows[id] {
			c.add(file, at, CodeSampleRefMissing, "No sample %s record has id %q.", typ, id)
		}
	}
}

func objectRows(rows any) []map[string]any {
	items, _ := rows.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if row, ok := item.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out
}

func (c *checker) checkUniqueIDs(file, at string, rows any, code string) {
	seen := map[string]bool{}
	for i, row := range objectRows(rows) {
		id := asString(row["id"])
		if seen[id] {
			c.add(file, fmt.Sprintf("%s/%d/id", at, i), code, "Two entries use the id %q.", id)
		}
		seen[id] = true
	}
}

func contentDate(s string) (time.Time, error) {
	if len(s) == len(time.DateOnly) {
		return time.Parse(time.DateOnly, s)
	}
	return time.Parse(time.RFC3339, s)
}

func (c *checker) checkTimeline(file, at string, raw any) {
	dependencies := map[string][]string{}
	for i, item := range objectRows(raw) {
		id := asString(item["id"])
		start, startErr := contentDate(asString(item["start"]))
		end, endErr := contentDate(asString(item["end"]))
		if startErr != nil || endErr != nil || start.After(end) {
			c.add(file, fmt.Sprintf("%s/%d", at, i), CodeContent, "Timeline items need valid dates with start at or before end.")
		}
		dependencies[id] = stringList(item["after"])
	}
	active, visited := map[string]bool{}, map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if active[id] {
			c.add(file, at, CodeContent, "Timeline dependencies cycle through %q.", id)
			return
		}
		if visited[id] {
			return
		}
		active[id] = true
		for _, predecessor := range dependencies[id] {
			if _, ok := dependencies[predecessor]; !ok {
				c.add(file, at, CodeContent, "%q depends on missing timeline item %q.", id, predecessor)
				continue
			}
			visit(predecessor)
		}
		delete(active, id)
		visited[id] = true
	}
	for _, id := range sortedKeys(dependencies) {
		visit(id)
	}
}

type docFrontMatter struct {
	PartIDs []string `yaml:"part-ids"`
	Links   []string `yaml:"links"`
}

var docMarker = regexp.MustCompile(`^<!--\s*part:([a-zA-Z0-9_-]+)\s*-->$`)

func documentParts(raw string) (docFrontMatter, []string, bool) {
	var head docFrontMatter
	rest, ok := strings.CutPrefix(strings.ReplaceAll(raw, "\r\n", "\n"), "---\n")
	if !ok {
		return head, nil, false
	}
	header, body, ok := strings.Cut(rest, "\n---\n")
	if !ok || yaml.Unmarshal([]byte(header), &head) != nil {
		return head, nil, false
	}
	var ids []string
	for _, line := range strings.Split(body, "\n") {
		if marker := docMarker.FindStringSubmatch(line); marker != nil {
			if contains(ids, marker[1]) {
				return head, nil, false
			}
			ids = append(ids, marker[1])
		}
	}
	if len(head.PartIDs) != len(ids) {
		return head, nil, false
	}
	for i, id := range ids {
		if head.PartIDs[i] != id {
			return head, nil, false
		}
	}
	return head, ids, true
}
