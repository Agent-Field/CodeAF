package agentfield

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
)

// ── WHAT AN AGENTFIELD EVENT BECOMES ────────────────────────────────────────
//
// The control plane dispatches every trigger's event to the intake reasoner as
// `{event, _meta}`: event is the source's NORMALIZED payload (what
// control-plane/internal/sources/<name> built after verifying the signature),
// and _meta names the trigger, the source, the event type and the delivery's
// idempotency key. A direct execute sends no _meta; its body is the input.
//
// [Translate] turns one of those into one of three things, and nothing else:
//
//   - an entry to file through [inbox.Add];
//   - a nudge: a `github` event asks the factory's own GitHub read to read
//     NOW. GITHUB STAYS NATIVE — an AgentField github event never files a
//     job, because github is the factory's own source, with its own identity,
//     labels, "Pick up issues" and every GitHub side effect;
//   - nothing, with the reason in a person's words (a Linear comment, a cron
//     tick that carries no work, a bot's Slack message).
//
// THE SOURCE OF EVERY ENTRY IS `agentfield:<source>`, with an underscore
// spelled as a hyphen because an inbox source name has none:
// agentfield:linear, agentfield:sentry, agentfield:slack,
// agentfield:generic-bearer, agentfield:generic-hmac, agentfield:cron, and
// agentfield:execute for a direct call. A routing rule names those. The
// external id is the source's own id for the work (a Linear identifier, a
// Sentry short id, a Slack message's channel and time), else the event's
// external_id, else the delivery's idempotency key, else the event's id.
// The trust is the source's own default capped by the control plane's
// ceiling: a person on Linear or Slack is a collaborator, an entry may say
// less, and NOTHING TRANSLATED IS EVER TRUSTED ABOVE THE CEILING.

// SourcePrefix begins the source name of every entry this package files.
const SourcePrefix = "agentfield:"

// ExecuteSource is the source a direct execute's entry is filed under.
const ExecuteSource = "execute"

// Meta is the dispatcher's `_meta`.
type Meta struct {
	TriggerID      string `json:"trigger_id,omitempty"`
	Source         string `json:"source,omitempty"`
	EventType      string `json:"event_type,omitempty"`
	EventID        string `json:"event_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	ReceivedAt     string `json:"received_at,omitempty"`
}

// Translation is what one event becomes. Exactly one of File, Nudge and
// Ignored is set.
type Translation struct {
	File *inbox.Entry
	// Nudge is the repository (owner/name) a github event named; the node
	// asks the GitHub read to read now when the factory watches it.
	Nudge string
	// Ignored says why the event files nothing.
	Ignored string
}

// SourceName is the inbox source an AgentField source's entries carry.
func SourceName(afSource string) string {
	s := strings.ToLower(strings.TrimSpace(afSource))
	if s == "" {
		s = ExecuteSource
	}
	return SourcePrefix + strings.ReplaceAll(s, "_", "-")
}

// Translate is the one door from a dispatched event to what the factory does
// with it. ceiling is the most an entry is trusted ([factory.Tier]); empty is
// collaborator.
func Translate(meta Meta, event json.RawMessage, ceiling factory.Tier) (Translation, error) {
	source := strings.ToLower(strings.TrimSpace(meta.Source))
	var (
		out Translation
		err error
	)
	switch source {
	case "github":
		return fromGitHub(event)
	case "linear":
		out, err = fromLinear(meta, event)
	case "sentry":
		out, err = fromSentry(meta, event)
	case "slack":
		out, err = fromSlack(meta, event)
	default:
		// generic_hmac, generic_bearer, cron, a direct execute and every other
		// source: the event already is an Entry, or carries one, or files
		// nothing.
		out, err = fromEntry(meta, event)
	}
	if err != nil || out.File == nil {
		return out, err
	}
	e := out.File
	e.Source = SourceName(source)
	if e.ExternalID == "" {
		e.ExternalID = firstOf(meta.IdempotencyKey, meta.EventID)
	}
	e.Trust = capTrust(e.Trust, ceiling)
	clean, err := inbox.Validate(*e)
	if err != nil {
		return Translation{}, err
	}
	out.File = &clean
	return out, nil
}

// trustRank orders the three tiers, lowest first.
var trustRank = map[factory.Tier]int{factory.TierStranger: 0, factory.TierCollab: 1, factory.TierOwner: 2}

// capTrust is t held to ceiling. An empty t is the ceiling itself; an empty
// ceiling is collaborator. A tier that is not one is a stranger.
func capTrust(t, ceiling factory.Tier) factory.Tier {
	if _, ok := trustRank[ceiling]; !ok {
		ceiling = factory.TierCollab
	}
	t = factory.Tier(strings.ToLower(strings.TrimSpace(string(t))))
	if t == "" {
		return ceiling
	}
	if _, ok := trustRank[t]; !ok {
		return factory.TierStranger
	}
	if trustRank[t] > trustRank[ceiling] {
		return ceiling
	}
	return t
}

// ── GitHub: a nudge, never a job ───────────────────────────────────────────

func fromGitHub(event json.RawMessage) (Translation, error) {
	var gh struct {
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	_ = json.Unmarshal(event, &gh)
	repo := strings.TrimSpace(gh.Repository.FullName)
	if repo == "" {
		return Translation{Ignored: "a GitHub event that names no repository"}, nil
	}
	return Translation{Nudge: repo}, nil
}

// ── Linear: an issue created or updated ────────────────────────────────────

// linearPriority maps Linear's priority number to the factory's scale:
// 1 Urgent, 2 High, 3 Medium, 4 Low; 0 is "No priority".
var linearPriority = map[int]string{1: "p0", 2: "p1", 3: "p2", 4: "p3"}

func fromLinear(meta Meta, event json.RawMessage) (Translation, error) {
	var ev struct {
		Action string          `json:"action"`
		Type   string          `json:"type"`
		URL    string          `json:"url"`
		Data   json.RawMessage `json:"data"`
		Actor  struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(event, &ev); err != nil {
		return Translation{}, errors.New("the Linear event is not the JSON Linear sends")
	}
	kind := strings.ToLower(ev.Type)
	action := strings.ToLower(ev.Action)
	if kind != "issue" || (action != "create" && action != "update") {
		return Translation{Ignored: fmt.Sprintf("a Linear %s, not an issue created or updated", eventName(meta.EventType, kind, action))}, nil
	}
	var issue struct {
		ID          string          `json:"id"`
		Identifier  string          `json:"identifier"`
		Title       string          `json:"title"`
		Description string          `json:"description"`
		URL         string          `json:"url"`
		Priority    json.RawMessage `json:"priority"`
		Labels      []struct {
			Name string `json:"name"`
		} `json:"labels"`
		Team struct {
			Key string `json:"key"`
		} `json:"team"`
		Creator struct {
			Name string `json:"name"`
		} `json:"creator"`
	}
	if err := json.Unmarshal(ev.Data, &issue); err != nil {
		return Translation{}, errors.New("the Linear issue in the event is not the JSON Linear sends")
	}
	e := inbox.Entry{
		ExternalID: firstOf(issue.Identifier, issue.ID),
		URL:        firstOf(issue.URL, ev.URL),
		Title:      oneLine(issue.Title, inbox.MaxTitle),
		Body:       issue.Description,
		Author:     firstOf(issue.Creator.Name, ev.Actor.Name, ev.Actor.Email),
		Trust:      factory.TierCollab,
	}
	if n, err := strconv.Atoi(strings.TrimSpace(string(issue.Priority))); err == nil {
		e.Priority = linearPriority[n]
	}
	for _, l := range issue.Labels {
		e.Labels = append(e.Labels, l.Name)
	}
	if issue.Team.Key != "" {
		e.Labels = append(e.Labels, "linear-team:"+issue.Team.Key)
	}
	if e.Title == "" {
		return Translation{Ignored: "a Linear issue with no title"}, nil
	}
	return Translation{File: &e}, nil
}

// ── Sentry: an issue ───────────────────────────────────────────────────────

// sentryPriority maps Sentry's level to the factory's scale.
var sentryPriority = map[string]string{"fatal": "p0", "error": "p1", "warning": "p2"}

func fromSentry(meta Meta, event json.RawMessage) (Translation, error) {
	var ev struct {
		Action   string `json:"action"`
		Resource string `json:"resource"`
		Data     struct {
			Issue *struct {
				ID        string `json:"id"`
				ShortID   string `json:"shortId"`
				Title     string `json:"title"`
				Culprit   string `json:"culprit"`
				Level     string `json:"level"`
				Permalink string `json:"permalink"`
				WebURL    string `json:"web_url"`
				Count     string `json:"count"`
				Metadata  struct {
					Type  string `json:"type"`
					Value string `json:"value"`
				} `json:"metadata"`
				Project struct {
					Slug string `json:"slug"`
				} `json:"project"`
			} `json:"issue"`
		} `json:"data"`
		Actor struct {
			Name string `json:"name"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(event, &ev); err != nil {
		return Translation{}, errors.New("the Sentry event is not the JSON Sentry sends")
	}
	resource, action := strings.ToLower(ev.Resource), strings.ToLower(ev.Action)
	issue := ev.Data.Issue
	if resource != "issue" || issue == nil || (action != "created" && action != "unresolved") {
		return Translation{Ignored: fmt.Sprintf("a Sentry %s, not an issue created or come back", eventName(meta.EventType, resource, action))}, nil
	}
	level := strings.ToLower(issue.Level)
	var body []string
	if issue.Metadata.Type != "" || issue.Metadata.Value != "" {
		body = append(body, strings.TrimSpace(issue.Metadata.Type+": "+issue.Metadata.Value))
	}
	if issue.Culprit != "" {
		body = append(body, "In: "+issue.Culprit)
	}
	if level != "" {
		body = append(body, "Level: "+level)
	}
	if issue.Count != "" {
		body = append(body, "Events: "+issue.Count)
	}
	e := inbox.Entry{
		ExternalID: firstOf(issue.ShortID, issue.ID),
		URL:        firstOf(issue.Permalink, issue.WebURL),
		Title:      oneLine(firstOf(issue.Title, issue.Metadata.Value), inbox.MaxTitle),
		Body:       strings.Join(body, "\n"),
		Author:     ev.Actor.Name,
		Priority:   sentryPriority[level],
		Trust:      factory.TierCollab,
	}
	if issue.Project.Slug != "" {
		e.Labels = append(e.Labels, "sentry-project:"+issue.Project.Slug)
	}
	if level != "" {
		e.Labels = append(e.Labels, "sentry-level:"+level)
	}
	if e.Title == "" {
		return Translation{Ignored: "a Sentry issue with no title"}, nil
	}
	return Translation{File: &e}, nil
}

// ── Slack: a message or a mention ──────────────────────────────────────────

// slackMention is `<@U123>` or `<@U123|name>`, which a title reads without.
var slackMention = regexp.MustCompile(`<@[A-Z0-9]+(\|[^>]*)?>`)

func fromSlack(meta Meta, event json.RawMessage) (Translation, error) {
	var env struct {
		Type   string `json:"type"`
		TeamID string `json:"team_id"`
		Event  struct {
			Type     string `json:"type"`
			Subtype  string `json:"subtype"`
			BotID    string `json:"bot_id"`
			User     string `json:"user"`
			Text     string `json:"text"`
			Channel  string `json:"channel"`
			TS       string `json:"ts"`
			ThreadTS string `json:"thread_ts"`
		} `json:"event"`
	}
	if err := json.Unmarshal(event, &env); err != nil {
		return Translation{}, errors.New("the Slack event is not the JSON Slack sends")
	}
	ev := env.Event
	switch {
	case env.Type != "event_callback":
		return Translation{Ignored: "a Slack " + firstOf(env.Type, "envelope") + ", not a message"}, nil
	case ev.Type != "message" && ev.Type != "app_mention":
		return Translation{Ignored: "a Slack " + firstOf(ev.Type, "event") + ", not a message"}, nil
	case ev.BotID != "" || ev.Subtype != "":
		// A bot's message (codeaf's own among them), an edit or a deletion is
		// not someone asking for work.
		return Translation{Ignored: "a Slack message from a bot, or an edit"}, nil
	}
	text := strings.TrimSpace(slackMention.ReplaceAllString(ev.Text, ""))
	if text == "" {
		return Translation{Ignored: "a Slack message with no words"}, nil
	}
	e := inbox.Entry{
		ExternalID: ev.Channel + ":" + ev.TS,
		Title:      oneLine(text, inbox.MaxTitle),
		Body:       text,
		Author:     ev.User,
		Trust:      factory.TierCollab,
	}
	if ev.Channel == "" || ev.TS == "" {
		e.ExternalID = ""
	}
	if ev.Channel != "" {
		e.Labels = append(e.Labels, "slack-channel:"+ev.Channel)
	}
	return Translation{File: &e}, nil
}

// ── Everything else: the event already is an Entry ─────────────────────────

func fromEntry(meta Meta, event json.RawMessage) (Translation, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(event, &obj); err != nil || obj == nil {
		return Translation{Ignored: noEntry(meta)}, nil
	}
	raw := event
	if inner, ok := obj["entry"]; ok {
		raw = inner
	} else if _, ok := obj["title"]; !ok {
		return Translation{Ignored: noEntry(meta)}, nil
	}
	var e inbox.Entry
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&e); err != nil {
		return Translation{}, errors.New("the event's entry is not an entry: title, body, external_id, repo, labels, priority and trust are its fields")
	}
	// THE SOURCE IS THE CONTROL PLANE'S, NEVER THE SENDER'S: a caller who can
	// post to one trigger may not file work as another system.
	e.Source = ""
	return Translation{File: &e}, nil
}

func noEntry(meta Meta) string {
	what := "event"
	if meta.Source != "" {
		what = meta.Source + " event"
		if meta.EventType != "" {
			what = meta.Source + " " + meta.EventType
		}
	}
	return "a " + what + " that carries no entry (a title, or an entry object)"
}

// ── small words ────────────────────────────────────────────────────────────

func eventName(eventType, kind, action string) string {
	if eventType != "" {
		return eventType
	}
	if action == "" {
		return firstOf(kind, "event")
	}
	return kind + "." + action
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// oneLine is s's first line, at most most characters.
func oneLine(s string, most int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > most {
		r := []rune(s)
		s = strings.TrimSpace(string(r[:most-1])) + "…"
	}
	return s
}
