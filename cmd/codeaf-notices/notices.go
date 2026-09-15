package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	cpaceIdentifier = "internal/pair/cpace"
	cpaceModule     = "filippo.io/cpace"
	cpaceVersion    = "v0.0.0-20210101143347-24d601e2e469"
	ampIdentifier   = "github.com/amp-labs/connectors"
)

type moduleVersion struct {
	Path    string
	Version string
}

type licenceFile struct {
	Name string
	Text []byte
}

type component struct {
	Identifier  string
	Version     string
	Source      string
	Description string
	Licences    []licenceFile
}

func findRepositoryRoot(from string) (string, error) {
	dir, err := filepath.Abs(from)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("find the repository root above %s: no go.mod", from)
		}
		dir = parent
	}
}

func collectComponents(root string, environment []string) ([]component, error) {
	modules, err := listCompiledModules(root, environment)
	if err != nil {
		return nil, err
	}
	components := make([]component, 0, len(modules)+2)
	for _, module := range modules {
		dir, err := moduleDirectory(root, module, environment)
		if err != nil {
			return nil, err
		}
		licences, err := licenceFiles(dir)
		if err != nil {
			return nil, fmt.Errorf("read licences for %s: %w", module.Path, err)
		}
		components = append(components, component{
			Identifier: module.Path,
			Version:    module.Version,
			Licences:   licences,
		})
	}

	cpaceLicence, err := os.ReadFile(filepath.Join(root, "internal", "pair", "cpace", "LICENSE"))
	if err != nil {
		return nil, fmt.Errorf("read the vendored CPace licence: %w", err)
	}
	components = append(components, component{
		Identifier:  cpaceIdentifier,
		Version:     cpaceVersion,
		Source:      cpaceModule,
		Description: "`internal/pair/cpace` is a byte-for-byte copy of `filippo.io/cpace` living in this repository.",
		Licences:    []licenceFile{{Name: "LICENSE", Text: cpaceLicence}},
	})

	ampVersion, err := listModule(root, ampIdentifier, environment)
	if err != nil {
		return nil, err
	}
	ampDir, err := moduleDirectory(root, ampVersion, environment)
	if err != nil {
		return nil, err
	}
	ampLicences, err := licenceFiles(ampDir)
	if err != nil {
		return nil, fmt.Errorf("read licences for %s: %w", ampIdentifier, err)
	}
	// The catalog module is normally absent from the compiled set. Replacing an
	// existing row keeps its copied-source explanation if that ever changes.
	for i := range components {
		if components[i].Identifier == ampIdentifier {
			components = append(components[:i], components[i+1:]...)
			break
		}
	}
	components = append(components, component{
		Identifier:  ampIdentifier,
		Version:     ampVersion.Version,
		Source:      ampIdentifier,
		Description: "`internal/connect/ampcatalog/providers.json` is generated from this module's provider catalog and embedded in the binary.",
		Licences:    ampLicences,
	})

	sort.Slice(components, func(i, j int) bool {
		return components[i].Identifier < components[j].Identifier
	})
	return components, nil
}

func listCompiledModules(root string, environment []string) ([]moduleVersion, error) {
	const format = `{{if and (not .Standard) .Module}}{{if not .Module.Main}}{{.Module.Path}} {{.Module.Version}}{{end}}{{end}}`
	out, err := runGo(root, environment, "list", "-deps", "-f", format, "./cmd/codeaf")
	if err != nil {
		return nil, fmt.Errorf("list modules compiled into cmd/codeaf: %w", err)
	}
	byPath := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return nil, fmt.Errorf("understand module row %q", line)
		}
		if previous, ok := byPath[fields[0]]; ok && previous != fields[1] {
			return nil, fmt.Errorf("module %s has both %s and %s in the build", fields[0], previous, fields[1])
		}
		byPath[fields[0]] = fields[1]
	}
	modules := make([]moduleVersion, 0, len(byPath))
	for path, version := range byPath {
		modules = append(modules, moduleVersion{Path: path, Version: version})
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })
	return modules, nil
}

func listModule(root, path string, environment []string) (moduleVersion, error) {
	out, err := runGo(root, environment, "list", "-m", "-f", "{{.Path}} {{.Version}}", path)
	if err != nil {
		return moduleVersion{}, fmt.Errorf("resolve %s in the build list: %w", path, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 || fields[0] != path {
		return moduleVersion{}, fmt.Errorf("understand build-list row %q for %s", strings.TrimSpace(string(out)), path)
	}
	return moduleVersion{Path: fields[0], Version: fields[1]}, nil
}

func moduleDirectory(root string, module moduleVersion, environment []string) (string, error) {
	out, err := runGo(root, environment, "mod", "download", "-json", module.Path+"@"+module.Version)
	if err != nil {
		return "", fmt.Errorf("download %s@%s: %w", module.Path, module.Version, err)
	}
	var downloaded struct {
		Dir   string
		Error string
	}
	if err := json.Unmarshal(out, &downloaded); err != nil {
		return "", fmt.Errorf("decode the module directory for %s@%s: %w", module.Path, module.Version, err)
	}
	if downloaded.Error != "" {
		return "", fmt.Errorf("download %s@%s: %s", module.Path, module.Version, downloaded.Error)
	}
	if downloaded.Dir == "" {
		return "", fmt.Errorf("download %s@%s returned no source directory", module.Path, module.Version)
	}
	return downloaded.Dir, nil
}

func runGo(root string, environment []string, args ...string) ([]byte, error) {
	command := exec.Command("go", args...)
	command.Dir = root
	command.Env = environment
	out, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return out, nil
}

func generatorGoEnvironment() []string {
	return goEnvironment(map[string]string{"GOFLAGS": "-buildvcs=false"})
}

func goEnvironment(overrides map[string]string) []string {
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(os.Environ())+len(keys))
	for _, entry := range os.Environ() {
		key := entry
		if at := strings.IndexByte(entry, '='); at >= 0 {
			key = entry[:at]
		}
		if _, replaced := overrides[key]; !replaced {
			environment = append(environment, entry)
		}
	}
	for _, key := range keys {
		environment = append(environment, key+"="+overrides[key])
	}
	return environment
}

func licenceFiles(dir string) ([]licenceFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !licenceFilename(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	files := make([]licenceFile, 0, len(names))
	for _, name := range names {
		text, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		files = append(files, licenceFile{Name: name, Text: text})
	}
	return files, nil
}

func licenceFilename(name string) bool {
	upper := strings.ToUpper(name)
	foundPrefix := false
	for _, prefix := range []string{"LICENSE", "LICENCE", "COPYING", "NOTICE", "PATENTS"} {
		if strings.HasPrefix(upper, prefix) {
			foundPrefix = true
			break
		}
	}
	if !foundPrefix {
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case "", ".md", ".txt", ".rst":
		return true
	default:
		return false
	}
}

type licencePattern struct {
	Identifier string
	All        []string
	Without    []string
}

var licencePatterns = []licencePattern{
	{
		Identifier: "BSD-3-Clause with the Go PATENTS grant",
		All:        []string{"Redistribution and use in source and binary forms", "Neither the name", "Additional IP Rights Grant (Patents)"},
	},
	{Identifier: "MPL-2.0", All: []string{"Mozilla Public License Version 2.0"}},
	{Identifier: "Apache-2.0", All: []string{"Apache License", "Version 2.0"}},
	{Identifier: "Unlicense", All: []string{"This is free and unencumbered software released into the public domain"}},
	{Identifier: "ISC", All: []string{"Permission to use, copy, modify, and/or distribute this software for any purpose with or without fee"}},
	{Identifier: "BSD-3-Clause", All: []string{"Redistribution and use in source and binary forms", "Neither the name"}},
	{
		Identifier: "BSD-2-Clause",
		All:        []string{"Redistribution and use in source and binary forms", "Redistributions in binary form must reproduce"},
		Without:    []string{"Neither the name"},
	},
	{Identifier: "MIT", All: []string{"Permission is hereby granted, free of charge, to any person obtaining a copy"}},
}

// identifyLicence is a reading convenience, not a substitute for the terms.
// THE REPRODUCED TEXT IS THE AUTHORITY, so an unfamiliar text stays not
// determined rather than acquiring a plausible label by guesswork.
func identifyLicence(files []licenceFile) string {
	var combined strings.Builder
	for _, file := range files {
		combined.Write(file.Text)
		combined.WriteByte('\n')
	}
	text := combined.String()
	for _, pattern := range licencePatterns {
		matches := true
		for _, phrase := range pattern.All {
			if !strings.Contains(text, phrase) {
				matches = false
			}
		}
		for _, phrase := range pattern.Without {
			if strings.Contains(text, phrase) {
				matches = false
			}
		}
		if matches {
			return pattern.Identifier
		}
	}
	return "not determined"
}

func renderNotices(components []component) []byte {
	var out bytes.Buffer
	out.WriteString("# Third-party notices\n\n")
	out.WriteString("This file covers every third-party Go module compiled into the codeaf binary, plus the two copied components carried in this repository's own tree. It ships beside the binaries in every release. Regenerate it with `go run ./cmd/codeaf-notices generate`.\n")
	for _, component := range components {
		fmt.Fprintf(&out, "\n## %s\n\n", component.Identifier)
		// THE THREE FACTS ARE A LIST because consecutive plain lines are one
		// paragraph to every Markdown renderer, and a version reading as part
		// of the licence line is exactly the confusion a notice must not cause.
		fmt.Fprintf(&out, "- Version: %s\n", component.Version)
		fmt.Fprintf(&out, "- Licence: %s\n", identifyLicence(component.Licences))
		if component.Source != "" {
			fmt.Fprintf(&out, "- Source: `%s`\n", component.Source)
		}
		if component.Description != "" {
			fmt.Fprintf(&out, "\n%s\n", component.Description)
		}
		if len(component.Licences) == 0 {
			fmt.Fprintf(&out, "\nThe module `%s` ships no top-level licence file; see its upstream at `https://%s`.\n", component.Identifier, component.Identifier)
			continue
		}
		for _, licence := range component.Licences {
			fmt.Fprintf(&out, "\n### %s\n\n", licence.Name)
			fence := codeFence(licence.Text)
			fmt.Fprintf(&out, "%stext\n", fence)
			out.Write(licence.Text)
			if len(licence.Text) == 0 || licence.Text[len(licence.Text)-1] != '\n' {
				out.WriteByte('\n')
			}
			fmt.Fprintf(&out, "%s\n", fence)
		}
	}
	return out.Bytes()
}

func codeFence(text []byte) string {
	longest, run := 0, 0
	for _, char := range text {
		if char == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	if longest < 2 {
		longest = 2
	}
	return strings.Repeat("`", longest+1)
}
