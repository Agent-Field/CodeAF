package delegate

// The host: everything a running program may ask of codeaf, and the
// environment its process starts in.

import (
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// The model API's two names in a program's environment: the OpenAI-style base
// URL codeaf serves this one run, and the token that opens it and nothing else.
// They are the ONLY road to a model a program has.
const (
	EnvModelAPI   = "CODEAF_MODEL_API"
	EnvModelToken = "CODEAF_MODEL_TOKEN"
)

// Host is what a running program asks codeaf for. Its body is handed one and
// reports through it: the records go to codeaf, and the models come from it.
type Host interface {
	// Workspace is the folder the program works in, absolute.
	Workspace() string
	// Ceilings are the limits codeaf set for this run. The program keeps them
	// itself so it can end cleanly, and codeaf enforces them whatever it does.
	Ceilings() Ceilings
	// Hello, Stage, Step and Terminal are the records (protocol.go). Hello
	// comes first and Terminal last, once.
	Hello(stages []string)
	Stage(stage StageRecord)
	Step(step StepRecord)
	Terminal(end Ending)
	// Models is this run's model API.
	Models() ModelAPI
}

// Ceilings are a run's limits. Zero is none.
type Ceilings struct {
	CostUSD float64
	Hours   float64
}

// Elapsed is the hours as a duration, zero for none.
func (c Ceilings) Elapsed() time.Duration {
	return time.Duration(c.Hours * float64(time.Hour))
}

// ModelAPI is the model API codeaf serves one run: an OpenAI-style base URL and
// the bearer token that opens it. A program in codeaf's own tree builds its
// route through internal/provider, the one package codeaf's funnel law lets
// spell a model route; a program outside it appends the route the way every
// OpenAI client does.
type ModelAPI struct {
	BaseURL string
	Token   string
}

// Ready answers whether there is an API to call.
func (m ModelAPI) Ready() bool {
	return strings.TrimSpace(m.BaseURL) != "" && strings.TrimSpace(m.Token) != ""
}

// Authorize puts the token on a request the program sends to the API.
func (m ModelAPI) Authorize(req *http.Request) { req.Header.Set("Authorization", "Bearer "+m.Token) }

// ModelAPIFromEnv reads the API from this process's environment; ok is false
// outside a run, which is how `codeaf <name>` tells a child of a host from a
// person at a shell.
func ModelAPIFromEnv() (ModelAPI, bool) {
	api := ModelAPI{BaseURL: strings.TrimSpace(env.Get(EnvModelAPI)), Token: strings.TrimSpace(env.Get(EnvModelToken))}
	return api, api.BaseURL != ""
}

// ChildEnv is the environment a program's process starts in: this process's,
// with every provider key and model redirection codeaf knows of taken out, and
// the model API's two names set.
//
// NO KEY REACHES A PROGRAM. Taking the keys out is not tidiness: a program
// hands its environment on to every command its model runs, so a key left
// here is a key any model-written shell line can print — senior-dev passed its
// whole environment to its shell tool before it was absorbed. And a
// redirection left here would let a program reach a model some other way than
// the API, which is the one road codeaf can meter, refuse at the ceiling and
// show a person.
func ChildEnv(api ModelAPI) []string {
	strip := []string{EnvModelAPI, EnvModelToken, envBaseURL, "OPENAI_API_KEY", modelsource.DefaultSource("").KeyEnv}
	for _, source := range modelsource.Vendored() {
		if source.KeyEnv != "" {
			strip = append(strip, source.KeyEnv)
		}
	}
	environ := env.EnvironWithout(strip...)
	if api.BaseURL != "" {
		environ = append(environ, EnvModelAPI+"="+api.BaseURL, EnvModelToken+"="+api.Token)
	}
	return environ
}

// envBaseURL is codeaf's own redirection of its default model service
// (internal/config). A program must not inherit it: its only address is the
// model API's.
const envBaseURL = "CODEAF_BASE_URL"
