package delegate

// The host: everything a running program may ask of codeaf, and the
// environment its process starts in.

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
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

// EnvRecords names the run's own record folder in a program's environment: the
// task's folder for a run the chat started, the run's folder under
// `~/.codeaf/v3/carried/<name>/` for a shell run. It is where a program that
// lands text leaves the files its answer points at, because the folder it
// works in is the person's and it promised to change nothing there.
const EnvRecords = "CODEAF_RECORDS"

// Recorder is a host that names the run's record folder ([EnvRecords]). A
// program asks for it by type, as it asks for a [Listener], so a test's host
// that names none is still a Host.
type Recorder interface {
	// Records is the run's record folder, absolute, or "" when codeaf named
	// none.
	Records() string
}

// RecordsEnv is the environment entry that names a run's record folder to its
// program, nil for none.
func RecordsEnv(folder string) []string {
	if strings.TrimSpace(folder) == "" {
		return nil
	}
	return []string{EnvRecords + "=" + folder}
}

// Records is the run's record folder codeaf named on the child's environment.
func (h *childHost) Records() string { return strings.TrimSpace(env.Get(EnvRecords)) }

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

// senior-dev's own unattended ceilings ([Delegate.Unattended]). They are
// named here rather than in its package because the manual's truth gate and
// the session's tests read them without linking its engine.
const (
	// An autonomous senior-dev run with no person watching must stop on its own.
	DefaultSeniorDevCostUSD = 10.0
	// An autonomous senior-dev run with no person watching must stop on its own.
	DefaultSeniorDevHours = 3.0
)

// SeniorDevCeilings is senior-dev's [Delegate.Unattended].
var SeniorDevCeilings = Ceilings{CostUSD: DefaultSeniorDevCostUSD, Hours: DefaultSeniorDevHours}

// IsZero says neither ceiling is set.
func (c Ceilings) IsZero() bool { return c.CostUSD == 0 && c.Hours == 0 }

// CappedBy is a conversation's remaining limits held to a program's unattended
// ceilings: an absent or unreadable limit becomes the program's, and a larger
// one is cut to it. A field the program leaves at zero is passed through
// untouched, so a program with no ceiling of its own runs on what is left.
func (c Ceilings) CappedBy(unattended Ceilings) Ceilings {
	if limit := unattended.CostUSD; limit > 0 && (c.CostUSD <= 0 || c.CostUSD > limit || math.IsNaN(c.CostUSD)) {
		c.CostUSD = limit
	}
	if limit := unattended.Hours; limit > 0 && (c.Hours <= 0 || c.Hours > limit || math.IsNaN(c.Hours)) {
		c.Hours = limit
	}
	return c
}

// FilledFrom is a shell line's ceilings with the program's unattended ones put
// where the line set none. A ceiling the person typed is kept as typed, larger
// or not: at a shell they are the one watching.
func (c Ceilings) FilledFrom(unattended Ceilings) Ceilings {
	if c.CostUSD <= 0 || math.IsNaN(c.CostUSD) || math.IsInf(c.CostUSD, 0) {
		c.CostUSD = unattended.CostUSD
	}
	if c.Hours <= 0 || math.IsNaN(c.Hours) || math.IsInf(c.Hours, 0) {
		c.Hours = unattended.Hours
	}
	return c
}

// SeniorDev is [Ceilings.CappedBy] senior-dev's ceilings.
func (c Ceilings) SeniorDev() Ceilings { return c.CappedBy(SeniorDevCeilings) }

// SeniorDevDefaults is [Ceilings.FilledFrom] senior-dev's ceilings.
func (c Ceilings) SeniorDevDefaults() Ceilings { return c.FilledFrom(SeniorDevCeilings) }

// Summary says the two ceilings as the person sees them at either start door,
// and says nothing of a ceiling that is not set: none at all is "", never
// `up to $0.00 and 0h` (the emptiness law).
func (c Ceilings) Summary() string {
	switch {
	case c.CostUSD > 0 && c.Hours > 0:
		return fmt.Sprintf("up to $%.2f and %s", c.CostUSD, c.TimeWord())
	case c.CostUSD > 0:
		return fmt.Sprintf("up to $%.2f", c.CostUSD)
	case c.Hours > 0:
		return "up to " + c.TimeWord()
	}
	return ""
}

// TimeWord spells the wall ceiling without padded zero units.
func (c Ceilings) TimeWord() string {
	wall := c.Elapsed()
	word := wall.String()
	if wall%time.Hour == 0 {
		word = fmt.Sprintf("%dh", int(wall/time.Hour))
	} else if wall%time.Minute == 0 {
		word = fmt.Sprintf("%dm", int(wall/time.Minute))
	}
	return word
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
// NO PROVIDER KEY IS INHERITED BY A PROGRAM. The child itself receives only the
// loopback model token; the model's shell strips that token as well. A
// redirection left here would let a program reach a model outside the API,
// the one road codeaf can meter and show a person.
func ChildEnv(api ModelAPI) []string {
	// Every *_API_KEY name, codeaf's own and the older spelling included, goes
	// by its suffix below, so it is not named here.
	strip := []string{EnvModelAPI, EnvModelToken, envBaseURL, modelsource.DefaultSource("").KeyEnv}
	for _, source := range modelsource.Vendored() {
		if source.KeyEnv != "" {
			strip = append(strip, source.KeyEnv)
		}
	}
	for _, source := range config.PersistedSources(config.ProfileDir()) {
		if source.KeyEnv != "" {
			strip = append(strip, source.KeyEnv)
		}
	}
	environ := env.EnvironWithout(strip...)
	// A provider may use a conventional key name before codeaf lists it, and
	// the older compatibility spelling may be present without its new name.
	kept := environ[:0]
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasSuffix(name, "_API_KEY") {
			kept = append(kept, entry)
		}
	}
	environ = kept
	if api.BaseURL != "" {
		environ = append(environ, EnvModelAPI+"="+api.BaseURL, EnvModelToken+"="+api.Token)
	}
	return environ
}

// envBaseURL is codeaf's own redirection of its default model service
// (internal/config). A program must not inherit it: its only address is the
// model API's.
const envBaseURL = "CODEAF_BASE_URL"
