package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// The addresses Slack answers on. They are variables rather than constants for
// the same reason Google's are: tests point them at a server they started, and
// nothing in the shipped program writes to them.
var (
	slackAuthURL  = "https://slack.com/oauth/v2_user/authorize"
	slackTokenURL = "https://slack.com/api/oauth.v2.user.access"
	slackAPIURL   = "https://slack.com/api"
	slackDoor     = "https://agentfield.ai/connect/slack/%d"
)

// slack is the plug for a person's Slack workspaces. It is deliberately a user
// connection and never a bot: every search, read and message is made as the
// person who signed in.
type slack struct{}

func init() { Register(slack{}) }

// slackScopes is the whole of what this plug asks a person for, in one place.
//
// THE ASK IS THE SMALLEST ONE THAT DOES THE WORK, grouped by what each set buys:
//
//   - search:read finds messages across the workspace.
//   - channels:read, groups:read, im:read and mpim:read list the conversations
//     the person can see.
//   - channels:history, groups:history, im:history and mpim:history open a
//     thread in any of those conversations.
//   - users:read and users:read.email put people's names on what was found.
//   - chat:write sends a message as the person who connected.
var slackScopes = []string{
	"search:read",
	"channels:read", "groups:read", "im:read", "mpim:read",
	"channels:history", "groups:history", "im:history", "mpim:history",
	"users:read", "users:read.email",
	"chat:write",
}

func (slack) Service() Service {
	return Service{
		ID:       "slack",
		Name:     "Slack",
		Category: categoryCommunication,
		Auth:     AuthBrowser,
		Blurb:    "Search, read and send Slack as you, in the workspaces you sign in to.",
		// The live exchange named the granted permissions as a comma-separated
		// list, so the store can prove that a connection still covers this ask.
		Scopes: append([]string(nil), slackScopes...),
	}
}

func (slack) Endpoint() oauth2.Endpoint {
	return oauth2.Endpoint{
		AuthURL:   slackAuthURL,
		TokenURL:  slackTokenURL,
		AuthStyle: oauth2.AuthStyleInParams,
	}
}

// Slack accepted the ordinary space-separated scope parameter in the live
// proof, so it needs no vendor-specific options here.
func (slack) AuthCodeOptions() []oauth2.AuthCodeOption { return nil }

// Door is the public address Slack knows for one fixed local listener.
func (slack) Door(port int) string { return fmt.Sprintf(slackDoor, port) }

// Transport turns Slack's success-status refusal on the exchange into the
// ordinary refusal shape the shared browser machinery already understands.
func (slack) Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return slackExchangeTransport{base: base}
}

// Account asks Slack whose connection this is and which workspace it belongs
// to. Empty pieces are dropped under the emptiness law.
func (slack) Account(ctx context.Context, client *http.Client) (string, error) {
	var answer struct {
		User string `json:"user"`
		Team string `json:"team"`
	}
	if err := slackCall(ctx, client, "auth.test", nil, &answer); err != nil {
		return "", err
	}
	var pieces []string
	if user := collapse(answer.User); user != "" {
		pieces = append(pieces, user)
	}
	if team := collapse(answer.Team); team != "" {
		pieces = append(pieces, team)
	}
	return strings.Join(pieces, " · "), nil
}

type slackExchangeTransport struct{ base http.RoundTripper }

func (s slackExchangeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := s.base.RoundTrip(request)
	if err != nil || request.URL.String() != slackTokenURL {
		return response, err
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	_ = response.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	var answer struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &answer) != nil || answer.OK {
		return response, nil
	}
	word := strings.TrimSpace(answer.Error)
	refusal, marshalErr := json.Marshal(map[string]string{
		"error":             word,
		"error_description": "Slack refused the sign-in: " + word,
	})
	if marshalErr != nil {
		return nil, marshalErr
	}
	response.StatusCode = http.StatusBadRequest
	response.Status = "400 Bad Request"
	response.Body = io.NopCloser(bytes.NewReader(refusal))
	response.ContentLength = int64(len(refusal))
	response.Header.Set("Content-Type", "application/json")
	response.Header.Del("Content-Length")
	return response, nil
}

// errSlackDead is what a dead Slack key reads as. A Slack user key never
// expires and has nothing to renew — no expires_in, no refresh key — so the
// three words below ARE its expiry, and the only next move is the browser trip
// again, said in the same sentence [Manager.Client]'s guard uses. Every helper
// hands it back bare, so the model reads one sentence for one situation.
var errSlackDead = errors.New("Slack has to be connected again")

// slackCall is the one door every Slack method goes through. Slack answers
// refusals with HTTP 200 and ok:false, so the status half is shared with every
// other account through [do], then the Slack half is checked here before any
// caller can mistake a refusal for an answer.
func slackCall(ctx context.Context, client *http.Client, method string, params url.Values, out any) error {
	if client == nil {
		return errors.New("no connected account for this request")
	}
	if params == nil {
		params = url.Values{}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(slackAPIURL, "/")+"/"+method, strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// One wrapper remembers a 429's retry window while leaving the account's
	// own transport — including its Authorization header — wholly intact.
	rate := &slackRateTransport{base: client.Transport}
	calling := *client
	calling.Transport = rate
	var raw json.RawMessage
	if err := do(&calling, request, &raw); err != nil {
		if rate.seconds != "" {
			return fmt.Errorf("Slack is rate-limiting this for another %s seconds", rate.seconds)
		}
		return err
	}
	var envelope struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("unreadable answer from Slack: %w", err)
	}
	if !envelope.OK {
		word := strings.TrimSpace(envelope.Error)
		switch word {
		case "invalid_auth", "token_revoked", "account_inactive":
			return errSlackDead
		}
		if word == "" {
			return fmt.Errorf("Slack refused %s", method)
		}
		return fmt.Errorf("Slack refused %s: %s", method, word)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("unreadable answer from Slack: %w", err)
	}
	return nil
}

type slackRateTransport struct {
	base    http.RoundTripper
	seconds string
}

func (s *slackRateTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	base := s.base
	if base == nil {
		base = http.DefaultTransport
	}
	response, err := base.RoundTrip(request)
	if err == nil && response.StatusCode == http.StatusTooManyRequests {
		s.seconds = strings.TrimSpace(response.Header.Get("Retry-After"))
	}
	return response, err
}

// SlackSearch answers a workspace query with the newest matching messages.
// Each block ends with the channel id and timestamp the thread reader takes.
func SlackSearch(ctx context.Context, client *http.Client, query string, max int) (string, error) {
	query = strings.TrimSpace(query)
	maximum := clampSlack(max, 10, 25)
	params := url.Values{
		"query":    {query},
		"count":    {strconv.Itoa(maximum)},
		"sort":     {"timestamp"},
		"sort_dir": {"desc"},
	}
	var answer struct {
		Messages struct {
			Matches []struct {
				Channel struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"channel"`
				User      string `json:"user"`
				Username  string `json:"username"`
				Text      string `json:"text"`
				TS        string `json:"ts"`
				Permalink string `json:"permalink"`
			} `json:"matches"`
		} `json:"messages"`
	}
	if err := slackCall(ctx, client, "search.messages", params, &answer); err != nil {
		if errors.Is(err, errSlackDead) {
			return "", err
		}
		return "", fmt.Errorf("search Slack: %w", err)
	}
	if len(answer.Messages.Matches) == 0 {
		return noMatches(query), nil
	}

	var builder strings.Builder
	for index, hit := range answer.Messages.Matches {
		if index > 0 {
			builder.WriteString("\n\n")
		}
		channel := collapse(hit.Channel.Name)
		if channel == "" {
			channel = strings.TrimSpace(hit.Channel.ID)
		}
		who := collapse(hit.Username)
		if who == "" {
			who = strings.TrimSpace(hit.User)
		}
		heading := []string{"#" + channel}
		if who != "" {
			heading = append(heading, who)
		}
		if when := slackWhen(hit.TS); when != "" {
			heading = append(heading, when)
		}
		builder.WriteString(strings.Join(heading, " · "))
		if text := collapse(hit.Text); text != "" {
			builder.WriteString("\n")
			builder.WriteString(clip(text, 400))
		}
		if permalink := strings.TrimSpace(hit.Permalink); permalink != "" {
			builder.WriteString("\n")
			builder.WriteString(permalink)
		}
		builder.WriteString("\nchannel ")
		builder.WriteString(strings.TrimSpace(hit.Channel.ID))
		builder.WriteString(" · ts ")
		builder.WriteString(strings.TrimSpace(hit.TS))
	}
	return bound(matchLine(len(answer.Messages.Matches), query) + "\n\n" + builder.String()), nil
}

// SlackReadThread reads one thread in one bounded call. Slack limits an
// outside-Marketplace application to one of these calls a minute and fifteen
// messages, so THERE IS NO PAGING LOOP HERE.
func SlackReadThread(ctx context.Context, client *http.Client, channel, ts string) (string, error) {
	params := url.Values{
		"channel":   {strings.TrimSpace(channel)},
		"ts":        {strings.TrimSpace(ts)},
		"limit":     {"15"},
		"inclusive": {"true"},
	}
	var answer struct {
		Messages []struct {
			User     string `json:"user"`
			Username string `json:"username"`
			Text     string `json:"text"`
			TS       string `json:"ts"`
			Profile  struct {
				DisplayName string `json:"display_name"`
				RealName    string `json:"real_name"`
			} `json:"user_profile"`
		} `json:"messages"`
	}
	if err := slackCall(ctx, client, "conversations.replies", params, &answer); err != nil {
		if errors.Is(err, errSlackDead) {
			return "", err
		}
		return "", fmt.Errorf("read Slack thread: %w", err)
	}
	if len(answer.Messages) == 0 {
		return "No messages in that thread.", nil
	}

	names := map[string]string{}
	lookups := 0
	var builder strings.Builder
	for index, message := range answer.Messages {
		if index > 0 {
			builder.WriteString("\n\n")
		}
		who := collapse(message.Profile.DisplayName)
		if who == "" {
			who = collapse(message.Profile.RealName)
		}
		if who == "" {
			who = collapse(message.Username)
		}
		user := strings.TrimSpace(message.User)
		if who == "" && user != "" {
			if held, seen := names[user]; seen {
				who = held
			} else if lookups < 10 {
				lookups++
				who = slackUserName(ctx, client, user)
				if who == "" {
					who = user
				}
				names[user] = who
			} else {
				who = user
			}
		}
		line := who
		if when := slackWhen(message.TS); when != "" {
			if line != "" {
				line += " · "
			}
			line += when
		}
		builder.WriteString(line)
		if text := collapse(message.Text); text != "" {
			if line != "" {
				builder.WriteString("\n")
			}
			builder.WriteString(text)
		}
	}
	return bound(builder.String()), nil
}

func slackUserName(ctx context.Context, client *http.Client, id string) string {
	var answer struct {
		User struct {
			Name     string `json:"name"`
			RealName string `json:"real_name"`
			Profile  struct {
				DisplayName string `json:"display_name"`
				RealName    string `json:"real_name"`
			} `json:"profile"`
		} `json:"user"`
	}
	if slackCall(ctx, client, "users.info", url.Values{"user": {id}}, &answer) != nil {
		return ""
	}
	for _, name := range []string{
		answer.User.Profile.DisplayName,
		answer.User.Profile.RealName,
		answer.User.RealName,
		answer.User.Name,
	} {
		if name = collapse(name); name != "" {
			return name
		}
	}
	return ""
}

// SlackListChannels lists the public and private channels the person can see.
// Filtering happens locally because Slack's listing method has no name filter.
func SlackListChannels(ctx context.Context, client *http.Client, filter string, max int) (string, error) {
	maximum := clampSlack(max, 50, 200)
	params := url.Values{
		"types":            {"public_channel,private_channel"},
		"exclude_archived": {"true"},
		"limit":            {strconv.Itoa(maximum)},
	}
	var answer struct {
		Channels []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			NumMembers int    `json:"num_members"`
			Purpose    struct {
				Value string `json:"value"`
			} `json:"purpose"`
		} `json:"channels"`
	}
	if err := slackCall(ctx, client, "conversations.list", params, &answer); err != nil {
		if errors.Is(err, errSlackDead) {
			return "", err
		}
		return "", fmt.Errorf("list Slack channels: %w", err)
	}
	want := strings.ToLower(strings.TrimSpace(filter))
	var lines []string
	for _, channel := range answer.Channels {
		name := collapse(channel.Name)
		if want != "" && !strings.Contains(strings.ToLower(name), want) {
			continue
		}
		fields := []string{"#" + name, strings.TrimSpace(channel.ID)}
		if channel.NumMembers > 0 {
			noun := "members"
			if channel.NumMembers == 1 {
				noun = "member"
			}
			fields = append(fields, fmt.Sprintf("%d %s", channel.NumMembers, noun))
		}
		if purpose := collapse(channel.Purpose.Value); purpose != "" {
			fields = append(fields, purpose)
		}
		lines = append(lines, strings.Join(fields, " · "))
	}
	if len(lines) == 0 {
		if trimmed := strings.TrimSpace(filter); trimmed != "" {
			return fmt.Sprintf("No channels match %q.", trimmed), nil
		}
		return "No channels.", nil
	}
	return bound(strings.Join(lines, "\n")), nil
}

// SlackSend posts one message, optionally as a reply in a thread.
func SlackSend(ctx context.Context, client *http.Client, channel, text, threadTS string) (string, error) {
	channel = strings.TrimSpace(channel)
	text = strings.TrimSpace(text)
	if channel == "" {
		return "", fmt.Errorf("send Slack: no channel named")
	}
	if text == "" {
		return "", fmt.Errorf("send Slack: nothing to say")
	}
	params := url.Values{"channel": {channel}, "text": {text}}
	if threadTS = strings.TrimSpace(threadTS); threadTS != "" {
		params.Set("thread_ts", threadTS)
	}
	var answer struct {
		TS string `json:"ts"`
	}
	if err := slackCall(ctx, client, "chat.postMessage", params, &answer); err != nil {
		if errors.Is(err, errSlackDead) {
			return "", err
		}
		return "", fmt.Errorf("send Slack: %w", err)
	}
	line := "Sent to " + channel
	if ts := strings.TrimSpace(answer.TS); ts != "" {
		line += " (ts " + ts + ")"
	}
	return line, nil
}

func clampSlack(given, fallback, maximum int) int {
	switch {
	case given <= 0:
		return fallback
	case given > maximum:
		return maximum
	}
	return given
}

func slackWhen(ts string) string {
	seconds, _, _ := strings.Cut(strings.TrimSpace(ts), ".")
	unix, err := strconv.ParseInt(seconds, 10, 64)
	if err != nil || unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).Local().Format("2006-01-02 15:04")
}
