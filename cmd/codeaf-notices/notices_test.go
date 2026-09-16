package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func committedNotices(t *testing.T) (string, map[string]string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), defaultNoticesPath))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	return text, noticeSections(text)
}

func noticeSections(text string) map[string]string {
	sections := make(map[string]string)
	identifier := ""
	var body strings.Builder
	keep := func() {
		if identifier != "" {
			sections[identifier] = body.String()
		}
	}
	for _, line := range strings.SplitAfter(text, "\n") {
		plain := strings.TrimSuffix(line, "\n")
		if strings.HasPrefix(plain, "## ") {
			keep()
			identifier = strings.TrimPrefix(plain, "## ")
			body.Reset()
			continue
		}
		if identifier != "" {
			body.WriteString(line)
		}
	}
	keep()
	return sections
}

func TestEveryCompiledModuleHasItsSection(t *testing.T) {
	root := repositoryRoot(t)
	modules, err := listCompiledModules(root, goEnvironment(map[string]string{
		"GOFLAGS": "-mod=readonly -buildvcs=false",
		"GOPROXY": "off",
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, sections := committedNotices(t)
	var missing []string
	for _, module := range modules {
		if _, ok := sections[module.Path]; !ok {
			missing = append(missing, module.Path)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("compiled modules have no third-party notice section: %s\nrun `go run ./cmd/codeaf-notices generate`", strings.Join(missing, ", "))
	}
}

func TestTheTwoCopiesCarriedInThisTreeHaveTheirSections(t *testing.T) {
	text, sections := committedNotices(t)
	for _, identifier := range []string{cpaceIdentifier, ampIdentifier} {
		if _, ok := sections[identifier]; !ok {
			t.Errorf("the copied component %s has no third-party notice section; run `go run ./cmd/codeaf-notices generate`", identifier)
		}
	}
	cpaceLicence, err := os.ReadFile(filepath.Join(repositoryRoot(t), "internal", "pair", "cpace", "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, string(cpaceLicence)) {
		t.Error("the notice does not reproduce internal/pair/cpace/LICENSE verbatim; run `go run ./cmd/codeaf-notices generate`")
	}
	if !strings.Contains(sections[ampIdentifier], "internal/connect/ampcatalog/providers.json") {
		t.Error("the amp-labs notice does not name internal/connect/ampcatalog/providers.json; run `go run ./cmd/codeaf-notices generate`")
	}
}

func TestNoNoticeSectionIsAHeadingWithNothingUnderIt(t *testing.T) {
	_, sections := committedNotices(t)
	identifiers := make([]string, 0, len(sections))
	for identifier := range sections {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	for _, identifier := range identifiers {
		if strings.TrimSpace(sections[identifier]) == "" {
			t.Errorf("third-party notice section %s has nothing under its heading; run `go run ./cmd/codeaf-notices generate`", identifier)
		}
	}
}
