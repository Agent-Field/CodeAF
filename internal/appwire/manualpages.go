package appwire

// manual.corpus.set: the desktop app's own manual pages, registered at start so
// the chat's manual tool can answer questions about the app as well as about
// codeaf. The engine keeps them in memory and replaces them whole on every set
// (internal/manual's apppages.go).

// ManualPage is one app page: a lowercase slug and its Markdown.
type ManualPage struct {
	Name     string `json:"name"`
	Markdown string `json:"markdown"`
}

// ManualCorpusParams is a whole set; an empty one removes every app page.
type ManualCorpusParams struct {
	Pages []ManualPage `json:"pages"`
}

// ManualCorpusResult is the page names the manual files the set under.
type ManualCorpusResult struct {
	Pages []string `json:"pages"`
}

// ManualPager is the engine's half of manual.corpus.set. A set that breaks a
// bound is refused bad_params, and nothing changed.
type ManualPager interface {
	SetManualPages(pages []ManualPage) ([]string, error)
}

// FeatureManualCorpus is hello's word for an engine that is a [ManualPager].
const FeatureManualCorpus = "manual.corpus.set"

func init() {
	methods["manual.corpus.set"] = typed(func(e Engine, p ManualCorpusParams) (*ManualCorpusResult, error) {
		pager, ok := e.(ManualPager)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve manual.corpus.set")
		}
		names, err := pager.SetManualPages(p.Pages)
		if err != nil {
			return nil, err
		}
		return &ManualCorpusResult{Pages: nonNil(names)}, nil
	})
}

func withManualCorpusFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(ManualPager); !ok {
		return hello
	}
	return withFeature(hello, FeatureManualCorpus)
}
