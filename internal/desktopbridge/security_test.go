package desktopbridge

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

// Security acceptance for the desktop bridge (BE-SEC-02, and the response half
// of the provider-key rule). bridgeRoutes sits beside the dispatch: ServeHTTP
// in bridge.go, extra in routes.go, the seam table in routes.go, and the
// settings and places tables.
//
// Mutation: delete the bearer compare in ServeHTTP (the ConstantTimeCompare
// that answers 401). TestEveryBridgeRouteRefusesAStranger then fails because a
// path answers something other than 401. TestGuardAssertionsFailOnAnOpenHandler
// is that same checker pointed at a handler with the checks already deleted.
// Mutation: delete the Origin branch of guardRequest. The foreign-origin probe
// fails because the status is not 403.
// Mutation: delete the loopback Host check in guardRequest. The public-host
// probe fails because the status is not 421.
// Mutation: write the bridge token or a provider key into fail, or into any
// response header. The secret scan fails without printing the secret.
// Mutation: add a route (registerPlacesRoute, registerSettingsRoute, a seam
// pattern: literal, path ==, case "/…", parts[N] ==, or a case label in the
// route switches) and leave it out of bridgeRoutes.
// TestBridgeRoutesNameEveryDispatch fails. The stand-in is
// TestDispatchScanRejectsAnUnlistedPath.
// Mutation: stop filtering provider keys in terminalEnv, or return an empty
// environment. TestTheTerminalEnvironmentDropsProviderKeys fails.

const (
	unauthorizedSentence = "engine connection required"
	refusedOrigin        = "this connection is only for the codeaf app"

	// Canaries are values, not variable names. A response may name the rung
	// OPENROUTER_API_KEY. It must not repeat one of these.
	canaryRouter    = "sk-security-canary-openrouter-9f3c2a1b"
	canaryOpenAI    = "sk-security-canary-openai-9f3c2a1b"
	canaryAnthropic = "sk-security-canary-anthropic-9f3c2a1b"
	canaryDesktop   = "desktop-token-canary-9f3c2a1b"
)

// bridgeRoutes is every path ServeHTTP can dispatch. A hole such as {session}
// is one path segment the probe fills in. The dispatch scan compares holes by
// shape, so {session} covers the {id} a register call spells.
var bridgeRoutes = []string{
	"/api/engine/health",
	"/api/engine/roots",
	"/api/engine/models",
	"/api/engine/models/roles",
	"/api/engine/models/roles/{role}",
	"/api/engine/models/pinned",
	"/api/engine/settings/key",
	"/api/engine/settings/engine",
	"/api/engine/settings/permissions",
	"/api/engine/world",
	"/api/engine/events",
	"/api/engine/world/failures/seen",
	"/api/engine/favicon",
	"/api/engine/sessions",
	"/api/engine/history",
	"/api/engine/history/search",
	"/api/engine/history/archive",
	"/api/engine/history/group-offers",
	"/api/engine/history/{hist}",
	"/api/engine/history/{hist}/messages",
	"/api/engine/places",
	"/api/engine/places/status",
	"/api/engine/places/rail",
	"/api/engine/places/undo",
	"/api/engine/places/undo/{receipt}",
	"/api/engine/places/from-folder",
	"/api/engine/places/stale",
	"/api/engine/places/policy",
	"/api/engine/places/policy/{setting}",
	"/api/engine/places/proposals",
	"/api/engine/places/proposals/organize",
	"/api/engine/places/proposals/{offer}/accept",
	"/api/engine/places/proposals/{offer}/decline",
	"/api/engine/places/suggestions",
	"/api/engine/places/suggestions/snooze",
	"/api/engine/places/{place}",
	"/api/engine/places/{place}/effective-model",
	"/api/engine/places/{place}/delete-preview",
	"/api/engine/places/{place}/home",
	"/api/engine/places/{place}/impact",
	"/api/engine/places/{place}/parents",
	"/api/engine/places/{place}/archive",
	"/api/engine/places/{place}/restore",
	"/api/engine/places/{place}/delete",
	"/api/engine/places/{place}/merge",
	"/api/engine/places/{place}/sources",
	"/api/engine/places/{place}/sources/remove",
	"/api/engine/places/{place}/members",
	"/api/engine/places/{place}/members/remove",
	"/api/engine/places/{place}/pin",
	"/api/engine/places/{place}/unpin",
	"/api/engine/places/{place}/visit",
	"/api/engine/places/{place}/stale-snooze",
	"/api/engine/places/{id}/decisions",
	"/api/engine/places/{id}/decide-status",
	"/api/engine/places/{id}/decide",
	"/api/engine/places/{id}/knows",
	"/api/engine/places/{id}/knows/{line}",
	"/api/engine/places/{id}/knows/{line}/still-true",
	"/api/engine/decisions/{id}",
	"/api/engine/decisions/{id}/overturn",
	"/api/engine/councils",
	"/api/engine/councils/{id}/steer",
	"/api/engine/chats/{chat}/places",
	"/api/engine/workspaces/{workspace}",
	"/api/engine/workspaces/{workspace}/open-elsewhere",
	"/api/engine/workspaces/{workspace}/transfer",
	"/api/engine/workspaces/{workspace}/favicon",
	"/api/engine/sessions/{session}",
	"/api/engine/sessions/{session}/answer",
	"/api/engine/sessions/{session}/turn",
	"/api/engine/sessions/{session}/queue-edit",
	"/api/engine/sessions/{session}/queue-move",
	"/api/engine/sessions/{session}/queue-remove",
	"/api/engine/sessions/{session}/queue-send",
	"/api/engine/sessions/{session}/stop",
	"/api/engine/sessions/{session}/events",
	"/api/engine/sessions/{session}/tools/{call}",
	"/api/engine/sessions/{session}/tasks/{task}",
	"/api/engine/sessions/{session}/tasks/{task}/note",
	"/api/engine/sessions/{session}/tasks/{task}/amend",
	"/api/engine/sessions/{session}/tasks/{task}/pause",
	"/api/engine/sessions/{session}/tasks/{task}/resume",
	"/api/engine/sessions/{session}/tasks/{task}/cancel",
	"/api/engine/sessions/{session}/files",
	"/api/engine/sessions/{session}/files/stat",
	"/api/engine/sessions/{session}/files/list",
	"/api/engine/sessions/{session}/files/text",
	"/api/engine/sessions/{session}/files/find",
	"/api/engine/sessions/{session}/files/locate",
	"/api/engine/sessions/{session}/jobs",
	"/api/engine/sessions/{session}/jobs/{job}/stop",
	"/api/engine/sessions/{session}/jobs/{job}/log",
	"/api/engine/sessions/{session}/detach",
	"/api/engine/sessions/{session}/editors",
	"/api/engine/sessions/{session}/editors/open",
	"/api/engine/sessions/{session}/changes",
	"/api/engine/sessions/{session}/diff",
	"/api/engine/sessions/{session}/questions/hold",
	"/api/engine/sessions/{session}/terminals",
	"/api/engine/sessions/{session}/terminals/{term}",
	"/api/engine/sessions/{session}/terminals/{term}/stream",
	"/api/engine/sessions/{session}/terminals/{term}/output",
	"/api/engine/sessions/{session}/terminals/{term}/input",
	"/api/engine/sessions/{session}/terminals/{term}/resize",
	"/api/engine/sessions/{session}/terminals/{term}/close",
	"/api/engine/sessions/{session}/terminals/{term}/remove",
	"/api/engine/sessions/{session}/sources",
	"/api/engine/sessions/{session}/favicon",
	"/api/engine/sessions/{session}/using",
	"/api/engine/sessions/{session}/using/choice",
	"/api/engine/sessions/{session}/using/apply",
	"/api/engine/sessions/{id}/plan/{plan}/go",
	"/api/engine/sessions/{id}/plan/{plan}/edit",
	"/api/engine/sessions/{id}/plan/{plan}/cancel",
}

// securityRoutes is the BE-SEC-02 set: the routes that landed after the first
// token check and must refuse the same way. Each one is also in bridgeRoutes.
var securityRoutes = []string{
	"/api/engine/places",
	"/api/engine/world",
	"/api/engine/workspaces/{workspace}",
	"/api/engine/settings/key",
	"/api/engine/sessions/{session}/jobs",
	"/api/engine/sessions/{session}/files/list",
	"/api/engine/sessions/{session}/using",
	"/api/engine/sessions/{session}/detach",
	"/api/engine/roots",
}

func TestEveryBridgeRouteRefusesAStranger(t *testing.T) {
	if foreignOriginSentence != refusedOrigin {
		t.Fatalf("origin sentence is %q", foreignOriginSentence)
	}
	secrets := plantCanaries(t)
	secrets = append(secrets, testToken)
	var opened atomic.Int32
	b := New(testToken, func(string) (Connection, error) {
		opened.Add(1)
		return Connection{}, io.EOF
	})
	t.Cleanup(b.Close)
	seen := map[string]bool{}
	for _, path := range bridgeRoutes {
		if seen[path] {
			t.Fatalf("duplicate route %s", path)
		}
		seen[path] = true
		if err := guardRoute(b.Handler(), concreteRoute(path), secrets); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	for _, path := range securityRoutes {
		if !seen[path] {
			t.Fatalf("BE-SEC-02 path %s is not on the route list", path)
		}
	}
	if opened.Load() != 0 {
		t.Fatalf("a refused request opened an engine %d times", opened.Load())
	}
}

func TestGuardAssertionsFailOnAnOpenHandler(t *testing.T) {
	// The stand-in for deleting the token, origin and host checks: the handler
	// answers 200 and echoes both secrets.
	open := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo", testToken)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, canaryRouter)
	})
	if err := guardRoute(open, "/api/engine/health", []string{canaryRouter, testToken}); err == nil {
		t.Fatal("an open handler satisfied the route guard")
	}
}

func TestRootsAnswerOmitsSecrets(t *testing.T) {
	secrets := plantCanaries(t)
	secrets = append(secrets, testToken)
	b := New(testToken, func(string) (Connection, error) {
		t.Fatal("roots opened an engine")
		return Connection{}, io.EOF
	})
	t.Cleanup(b.Close)
	w := request(b, http.MethodGet, "/api/engine/roots", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"roots"`) {
		t.Fatalf("roots: %d %s", w.Code, w.Body.String())
	}
	if err := leaked(w, secrets); err != nil {
		t.Fatal(err)
	}
}

func TestTheTerminalEnvironmentDropsProviderKeys(t *testing.T) {
	// A plain owned variable must survive, so an empty environment cannot pass
	// by dropping the canaries along with everything else.
	t.Setenv("CODEAF_PLAIN_SETTING", "kept-plain")
	plantCanaries(t)
	joined := strings.Join(terminalEnv(), "\n")
	if !strings.Contains(joined, "CODEAF_PLAIN_SETTING=kept-plain") || !strings.Contains(joined, "TERM=xterm-256color") {
		t.Fatal("terminal environment dropped a non-secret or the terminal type")
	}
	for _, line := range terminalEnv() {
		name, value, _ := strings.Cut(line, "=")
		for _, secret := range []string{canaryRouter, canaryOpenAI, canaryAnthropic, canaryDesktop, testToken} {
			if secret != "" && strings.Contains(value, secret) {
				t.Fatalf("terminal environment kept %s", name)
			}
		}
		switch name {
		case "OPENROUTER_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "CODEAF_DESKTOP_TOKEN":
			t.Fatalf("terminal environment kept %s", name)
		}
	}
}

func plantCanaries(t *testing.T) []string {
	t.Helper()
	t.Setenv("OPENROUTER_API_KEY", canaryRouter)
	t.Setenv("OPENAI_API_KEY", canaryOpenAI)
	t.Setenv("ANTHROPIC_API_KEY", canaryAnthropic)
	t.Setenv("CODEAF_DESKTOP_TOKEN", canaryDesktop)
	return []string{canaryRouter, canaryOpenAI, canaryAnthropic, canaryDesktop}
}

func concreteRoute(path string) string {
	return strings.NewReplacer(
		"{session}", "s1",
		"{task}", "t1",
		"{job}", "1",
		"{term}", "term1",
		"{call}", "c1",
		"{place}", "p1",
		"{chat}", "ch1",
		"{offer}", "offer1",
		"{workspace}", "now",
		"{role}", "conversation",
		"{hist}", "h1",
		"{setting}", "organize",
		"{receipt}", "r1",
		"{id}", "id1",
		"{line}", "line1",
		"{plan}", "plan1",
	).Replace(path)
}

func guardRoute(h http.Handler, path string, secrets []string) error {
	probes := []struct {
		name, host, origin, token, sentence string
		status                              int
	}{
		{"no token", "127.0.0.1:1420", "", "", unauthorizedSentence, http.StatusUnauthorized},
		{"foreign origin", "127.0.0.1:1420", "https://evil.example", testToken, refusedOrigin, http.StatusForbidden},
		{"public host", "8.8.8.8:4000", "", testToken, refusedOrigin, http.StatusMisdirectedRequest},
	}
	for _, probe := range probes {
		w := ask(h, http.MethodGet, path, probe.host, probe.origin, probe.token)
		if w.Code != probe.status {
			return fmt.Errorf("%s: status %d, want %d", probe.name, w.Code, probe.status)
		}
		if !strings.Contains(w.Body.String(), probe.sentence) {
			return fmt.Errorf("%s: body omitted the refusal sentence", probe.name)
		}
		if probe.status != http.StatusUnauthorized {
			for key := range w.Header() {
				if strings.HasPrefix(http.CanonicalHeaderKey(key), "Access-Control") {
					return fmt.Errorf("%s: sent %s", probe.name, key)
				}
			}
		}
		if err := leaked(w, secrets); err != nil {
			return fmt.Errorf("%s: %w", probe.name, err)
		}
	}
	return nil
}

func ask(h http.Handler, method, path, host, origin, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func leaked(w *httptest.ResponseRecorder, secrets []string) error {
	var blob strings.Builder
	blob.WriteString(w.Body.String())
	for key, values := range w.Header() {
		blob.WriteByte('\n')
		blob.WriteString(key)
		for _, value := range values {
			blob.WriteByte('\n')
			blob.WriteString(value)
		}
	}
	text := blob.String()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(text, secret) {
			return fmt.Errorf("response leaked a secret (status %d)", w.Code)
		}
	}
	return nil
}

type routeMention struct {
	kind  string // path, segment, workspaces, workspaceSuffix
	value string
	from  string
}

func TestBridgeRoutesNameEveryDispatch(t *testing.T) {
	if err := uncovered(dispatchMentions(t), bridgeRoutes); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchScanRejectsAnUnlistedPath(t *testing.T) {
	missing := []routeMention{
		{kind: "path", value: "/not-a-route", from: "mutation"},
		{kind: "segment", value: "not-a-segment", from: "mutation"},
		{kind: "workspaceSuffix", value: "/not-a-suffix", from: "mutation"},
	}
	for _, mention := range missing {
		if err := uncovered([]routeMention{mention}, bridgeRoutes); err == nil {
			t.Fatalf("mutation accepted %#v", mention)
		}
	}
}

func uncovered(mentions []routeMention, routes []string) error {
	for _, mention := range mentions {
		if !mentionCovered(mention, routes) {
			return fmt.Errorf("%s mentions %s %q, which is not on bridgeRoutes", mention.from, mention.kind, mention.value)
		}
	}
	return nil
}

func mentionCovered(mention routeMention, routes []string) bool {
	switch mention.kind {
	case "path":
		want := mention.value
		if !strings.HasPrefix(want, "/api/engine") {
			want = "/api/engine" + want
		}
		for _, route := range routes {
			if sameShape(route, want) {
				return true
			}
		}
	case "segment":
		for _, route := range routes {
			for _, part := range strings.Split(route, "/") {
				if part == mention.value {
					return true
				}
			}
		}
	case "workspaces":
		for _, route := range routes {
			if sameShape(route, "/api/engine/workspaces/{workspace}") {
				return true
			}
		}
	case "workspaceSuffix":
		for _, route := range routes {
			if strings.Contains(route, "/workspaces/") && strings.HasSuffix(route, mention.value) {
				return true
			}
		}
	}
	return false
}

func sameShape(route, want string) bool {
	left := strings.Split(strings.Trim(route, "/"), "/")
	right := strings.Split(strings.Trim(want, "/"), "/")
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] == right[i] {
			continue
		}
		if routeHole(left[i]) && routeHole(right[i]) {
			continue
		}
		return false
	}
	return true
}

func routeHole(part string) bool {
	return strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}")
}

// caseLabelFiles are the switches whose quoted labels are URL segments.
// steer and queue are turn modes inside the turn handler, not paths.
var caseLabelFiles = map[string]bool{
	"bridge.go":          true,
	"places.go":          true,
	"queue.go":           true,
	"routes.go":          true,
	"terminal_routes.go": true,
}

var notAPathSegment = map[string]bool{
	"steer": true,
	"queue": true,
}

func dispatchMentions(t *testing.T) []routeMention {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var mentions []routeMention
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		source := stripGoComments(string(raw))
		from := filepath.Base(name)
		mentions = append(mentions, mentionsIn(from, source)...)
	}
	if len(mentions) == 0 {
		t.Fatal("dispatch scan found no routes")
	}
	return mentions
}

var (
	settingsRoutePattern = regexp.MustCompile(`registerSettingsRoute\(\s*http\.Method(?:Get|Post|Put|Delete)\s*,\s*"(/[^"]+)"`)
	placesRoutePattern   = regexp.MustCompile(`registerPlacesRoute\(\s*"(?:GET|POST|PUT|DELETE)\s+(/places/[^"]+)"`)
	seamPattern          = regexp.MustCompile(`pattern:\s*"(/[^"]+)"`)
	pathLiteralPattern   = regexp.MustCompile(`(?:path\s*==\s*|case\s+)"(/[a-zA-Z0-9{}_/-]+)"`)
	segmentPattern       = regexp.MustCompile(`(?:parts|rest)\[\d+\]\s*!?=\s*"([a-z][a-z0-9-]*)"`)
	caseLabelPattern     = regexp.MustCompile(`(?m)^[ \t]*case\s+((?:"[a-z][a-z0-9-]*"\s*,\s*)*"[a-z][a-z0-9-]*")\s*:`)
	quotedSegmentPattern = regexp.MustCompile(`"([a-z][a-z0-9-]*)"`)
	cutPrefixPattern     = regexp.MustCompile(`CutPrefix\(\s*\w+\s*,\s*"(/workspaces/)"`)
	cutSuffixPattern     = regexp.MustCompile(`CutSuffix\(\s*\w+\s*,\s*"(/[a-z0-9-]+)"`)
)

func mentionsIn(file, source string) []routeMention {
	var mentions []routeMention
	add := func(kind, value string) {
		mentions = append(mentions, routeMention{kind: kind, value: value, from: file})
	}
	for _, match := range settingsRoutePattern.FindAllStringSubmatch(source, -1) {
		add("path", match[1])
	}
	for _, match := range placesRoutePattern.FindAllStringSubmatch(source, -1) {
		add("path", match[1])
	}
	for _, match := range seamPattern.FindAllStringSubmatch(source, -1) {
		add("path", match[1])
	}
	for _, match := range pathLiteralPattern.FindAllStringSubmatch(source, -1) {
		add("path", match[1])
	}
	for _, match := range segmentPattern.FindAllStringSubmatch(source, -1) {
		add("segment", match[1])
	}
	if caseLabelFiles[file] {
		for _, match := range caseLabelPattern.FindAllStringSubmatch(source, -1) {
			for _, quoted := range quotedSegmentPattern.FindAllStringSubmatch(match[1], -1) {
				if notAPathSegment[quoted[1]] {
					continue
				}
				add("segment", quoted[1])
			}
		}
	}
	workspaces := cutPrefixPattern.MatchString(source)
	if workspaces {
		add("workspaces", "/workspaces/")
		for _, match := range cutSuffixPattern.FindAllStringSubmatch(source, -1) {
			add("workspaceSuffix", match[1])
		}
	}
	return mentions
}

func stripGoComments(source string) string {
	var b strings.Builder
	b.Grow(len(source))
	inBlock := false
	inString := byte(0)
	for i := 0; i < len(source); i++ {
		if inBlock {
			if source[i] == '\n' {
				b.WriteByte('\n')
			}
			if source[i] == '*' && i+1 < len(source) && source[i+1] == '/' {
				inBlock = false
				i++
			}
			continue
		}
		if inString != 0 {
			b.WriteByte(source[i])
			if source[i] == '\\' && i+1 < len(source) {
				i++
				b.WriteByte(source[i])
				continue
			}
			if source[i] == inString {
				inString = 0
			}
			continue
		}
		if source[i] == '/' && i+1 < len(source) && source[i+1] == '/' {
			for i < len(source) && source[i] != '\n' {
				i++
			}
			if i < len(source) {
				b.WriteByte('\n')
			}
			continue
		}
		if source[i] == '/' && i+1 < len(source) && source[i+1] == '*' {
			inBlock = true
			i++
			continue
		}
		if source[i] == '"' || source[i] == '\'' || source[i] == '`' {
			inString = source[i]
		}
		b.WriteByte(source[i])
	}
	return b.String()
}
