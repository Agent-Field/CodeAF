package manual

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// THE CATALOG OWNS ITS SIZE. A count in prose goes stale when a contributor
// adds a source, even if every connection door already reads the catalog.
func TestManualProviderListsDoNotPinTheCatalogSize(t *testing.T) {
	paths := []string{"../../docs/GUIDE.md"}
	for _, folder := range []string{"chat", "pages"} {
		pages, err := filepath.Glob(folder + "/*.md")
		if err != nil {
			t.Fatal(err)
		}
		if len(pages) == 0 {
			t.Fatalf("no manual pages in %s", folder)
		}
		paths = append(paths, pages...)
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, paragraph := range strings.Split(string(body), "\n\n") {
			if pinsProviderCount(paragraph) {
				t.Errorf("%s pins the built-in provider count: %s", path, flatten(paragraph))
			}
		}
	}
}

var providerCount = regexp.MustCompile(`\b(?:[0-9]+|zero|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty|thirty|forty|fifty|sixty|seventy|eighty|ninety|hundred)(?:[- ](?:one|two|three|four|five|six|seven|eight|nine))?\s+(?:(?:built-in|supported|initial|setup|model|provider)\s+)*(?:providers|options)\b`)
var providerCountLead = regexp.MustCompile(`\b(?:all|same|shows?|includes?|supports?|offers?|lists?|holds?|contains?|has|there are)\s+(?:exactly\s+)?$`)
var providerOptionCatalog = regexp.MustCompile(`\b(?:provider (?:list|catalog|chooser|menu)|providers (?:group|list|catalog|menu)|flat list)\b`)

// Match catalog claims, including wrapped sentences and Markdown emphasis,
// while allowing quantities about connected providers or a router's endpoints.
func pinsProviderCount(paragraph string) bool {
	text := strings.ToLower(strings.Join(strings.Fields(strings.NewReplacer("*", "", "`", "", "_", "").Replace(paragraph)), " "))
	if !strings.Contains(text, "provider") {
		return false
	}
	for _, at := range providerCount.FindAllStringIndex(text, -1) {
		claim := text[at[0]:at[1]]
		// A provider can offer counted connection choices without counting the
		// catalog. Only provider options or a catalog list pin its size.
		if strings.HasSuffix(claim, "options") && !strings.Contains(claim, "provider options") && !providerOptionCatalog.MatchString(text) {
			continue
		}
		for _, catalogWord := range []string{"built-in", "supported", "initial", "setup"} {
			if strings.Contains(claim, catalogWord) {
				return true
			}
		}
		if providerCountLead.MatchString(text[:at[0]]) || strings.HasPrefix(strings.TrimSpace(text[at[1]:]), ":") {
			return true
		}
	}
	return false
}

func TestProviderCountGuardRecognizesCatalogClaims(t *testing.T) {
	for _, claim := range []string{
		"One flat provider list shows all nine options: OpenRouter, Ollama.",
		"The providers group includes all 10 setup options.",
		"That menu includes all ten initial provider options.",
		"Its provider list shows the same nine\noptions as initial setup.",
		"It holds the six built-in model providers plus every one connected.",
		"There are **11** supported providers.",
		"There are twenty-one provider options.",
		"12 providers: OpenRouter, Ollama.",
	} {
		if !pinsProviderCount(claim) {
			t.Errorf("count guard missed %q", claim)
		}
	}
	for _, claim := range []string{
		"Its flat list shows every built-in provider.",
		"With two or more providers, models are grouped by provider.",
		"Two providers may publish the same model.",
		"Probe each of the two providers your next message may reach.",
		"Choose between two connected providers.",
		"The provider returns 10 models.",
		"The provider offers two options: browser sign-in or an API key.",
		"For this provider, setup offers two options: sign in or paste a key.",
		"Two providers each offer two options: browser sign-in or an API key.",
		"The router tried nine endpoints behind the provider.",
		"Compare two options.",
	} {
		if pinsProviderCount(claim) {
			t.Errorf("count guard rejected %q", claim)
		}
	}
}
