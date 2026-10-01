package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/opener"
	"github.com/Agent-Field/codeaf/internal/openrouterauth"
	"github.com/Agent-Field/codeaf/internal/trace"
)

type codexConnectFlow interface {
	URL() string
	Wait(context.Context) (codexauth.Tokens, error)
	Cancel()
}

type openRouterConnectFlow interface {
	URL() string
	Wait(context.Context) (string, error)
	Cancel()
}

var (
	connectCodexFlow = func(ctx context.Context) (codexConnectFlow, error) {
		return codexauth.Begin(ctx, codexauth.Options{})
	}
	connectOpenRouterFlow = func(ctx context.Context) (openRouterConnectFlow, error) {
		return openrouterauth.Begin(ctx, openrouterauth.Options{})
	}
	connectOpen         = opener.Start
	connectInput        = os.Stdin
	connectInputIsTTY   = stdinIsTerminal
	connectReadPassword = term.ReadPassword
)

func runConnect(args []string) error {
	flags := commandFlags("connect")
	noBrowser := flags.Bool("no-browser", false, "print the browser address without opening it")
	region := flags.String("region", "", "service region: intl or cn")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		return listConnections(config.ProfileDir())
	}
	if flags.NArg() != 1 {
		return wrongCall("codeaf connect takes one provider name")
	}
	service := strings.ToLower(strings.TrimSpace(flags.Arg(0)))
	switch service {
	case "codex":
		return connectCodex(context.Background(), config.ProfileDir(), *noBrowser)
	case modelsource.DefaultID:
		return connectOpenRouter(context.Background(), config.ProfileDir(), *noBrowser)
	default:
		return connectKeyService(context.Background(), config.ProfileDir(), service, strings.TrimSpace(*region))
	}
}

func connectCodex(ctx context.Context, profileDir string, noBrowser bool) error {
	flow, err := connectCodexFlow(ctx)
	if err != nil {
		return connectFailed("codex", err)
	}
	defer flow.Cancel()
	fmt.Fprintln(usageOut, flow.URL())
	if noBrowser {
		port := callbackPort(flow.URL())
		fmt.Fprintf(usageOut, "on a machine without a browser: ssh -L %s:localhost:%s <that machine> and open the link here\n", port, port)
	} else {
		if err := connectOpen(flow.URL()); err != nil {
			fmt.Fprintln(usageOut, opener.BrowserFailureWord)
		}
	}
	tokens, err := flow.Wait(ctx)
	if err != nil {
		return connectFailed("codex", err)
	}
	outcome, err := config.ConnectCodex(ctx, profileDir, tokens)
	if err != nil {
		return connectFailed("codex", err)
	}
	fmt.Fprintln(usageOut, config.CodexConnectionWord(tokens.Email, tokens.Plan, outcome))
	return nil
}

func callbackPort(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return "1455"
	}
	redirect, err := url.Parse(parsed.Query().Get("redirect_uri"))
	if err != nil || redirect.Port() == "" {
		return "1455"
	}
	return redirect.Port()
}

func connectOpenRouter(ctx context.Context, profileDir string, noBrowser bool) error {
	flow, err := connectOpenRouterFlow(ctx)
	if err != nil {
		return connectFailed(modelsource.DefaultID, err)
	}
	defer flow.Cancel()
	fmt.Fprintln(usageOut, flow.URL())
	if !noBrowser {
		if err := connectOpen(flow.URL()); err != nil {
			fmt.Fprintln(usageOut, opener.BrowserFailureWord)
		}
	}
	key, err := flow.Wait(ctx)
	if err != nil {
		return connectFailed(modelsource.DefaultID, err)
	}
	trace.Secret(key)
	if err := config.WriteAPIKey(profileDir, key); err != nil {
		return connectFailed(modelsource.DefaultID, err)
	}
	fmt.Fprintln(usageOut, modelsource.DefaultID+" connected")
	return nil
}

func connectFailed(service string, err error) error {
	reason := plainWords(err.Error())
	if _, tail, found := strings.Cut(reason, ": "); found {
		reason = tail
	}
	fmt.Fprintln(usageOut, service+" did not connect · "+reason)
	return exitStatus(1)
}

func listConnections(profileDir string) error {
	connected := config.ResolveSources(profileDir, config.APIKeyAt(profileDir), config.DefaultBaseURL)
	held := make(map[string]bool)
	for _, service := range connected.All() {
		if service.Source.ID == modelsource.DefaultID {
			held[service.Source.ID] = strings.TrimSpace(service.Key) != ""
		} else {
			held[service.Source.ID] = strings.TrimSpace(service.Key) != "" || service.Source.KeyOptional
		}
	}
	any := false
	line := func(name, method string, yes bool) {
		state := "not connected"
		if yes {
			state, any = "connected", true
		}
		fmt.Fprintf(usageOut, "%s · %s · %s\n", name, state, method)
	}
	line(modelsource.DefaultID, "browser or key", held[modelsource.DefaultID])
	for _, source := range modelsource.Vendored() {
		if modelsource.IsCustomID(source.ID) {
			continue
		}
		method := "key"
		if source.ID == "codex" {
			method = "browser"
		}
		line(source.Written, method, held[source.ID])
	}
	for _, row := range config.PersistedSources(profileDir) {
		if modelsource.IsCustomID(row.ID) {
			line(row.Written, "key", held[row.ID])
		}
	}
	if !any {
		fmt.Fprintln(usageOut, "no provider is connected")
	}
	return nil
}

func connectKeyService(ctx context.Context, profileDir, name, region string) error {
	source, row, found := connectionSource(profileDir, name)
	if !found || source.ID == "codex" || source.ID == modelsource.DefaultID {
		fmt.Fprintln(usageOut, name+" is not a provider this profile knows")
		return exitStatus(1)
	}
	if len(source.Regions) > 0 && region == "" {
		return wrongCall("codeaf connect " + name + " needs --region intl or --region cn")
	}
	if region != "" {
		valid := false
		for _, candidate := range source.Regions {
			if candidate.ID == region {
				valid = true
			}
		}
		if !valid {
			return wrongCall("--region must be intl or cn")
		}
		row.Region = region
	}
	if !source.KeyOptional {
		key, err := readConnectionKey()
		if err != nil {
			return err
		}
		row.Key, row.KeyEnv = strings.TrimSpace(key), ""
	}
	outcome, err := config.ConnectService(ctx, profileDir, row, source, nil)
	if err != nil {
		return err
	}
	written := strings.ToLower(strings.TrimSpace(row.Written))
	if written == "" {
		written = source.Written
	}
	fmt.Fprintln(usageOut, connectionOutcome(written, outcome))
	if outcome.Kind != modelsource.OutcomeConnected && outcome.Kind != modelsource.OutcomeAccountCannotPay {
		return exitStatus(1)
	}
	return nil
}

func connectionSource(profileDir, name string) (modelsource.Source, config.PersistedSource, bool) {
	for _, service := range config.ResolveSources(profileDir, "", config.DefaultBaseURL).All() {
		if strings.EqualFold(service.Source.ID, name) || strings.EqualFold(service.Source.Written, name) {
			for _, row := range config.PersistedSources(profileDir) {
				if strings.EqualFold(row.ID, service.Source.ID) {
					return service.Source, row, true
				}
			}
		}
	}
	for _, source := range modelsource.Vendored() {
		if strings.EqualFold(source.ID, name) || strings.EqualFold(source.Written, name) {
			if modelsource.IsCustomID(source.ID) {
				return modelsource.Source{}, config.PersistedSource{}, false
			}
			return source, config.PersistedSource{ID: source.ID, Written: source.Written}, true
		}
	}
	return modelsource.Source{}, config.PersistedSource{}, false
}

func readConnectionKey() (string, error) {
	if connectInput == nil {
		return "", errors.New("no key was provided on stdin")
	}
	if connectInputIsTTY(connectInput) {
		fmt.Fprint(usageErr, "key: ")
		raw, err := connectReadPassword(connectInput.Fd())
		fmt.Fprintln(usageErr)
		return strings.TrimSpace(string(raw)), err
	}
	raw, err := io.ReadAll(io.LimitReader(connectInput, 1<<20))
	return strings.TrimSpace(string(raw)), err
}

func connectionOutcome(service string, outcome modelsource.Outcome) string {
	// The panel and terminal are two doors onto one connection check. Config
	// owns the sentence so adding a field to an outcome cannot respell one alone.
	return config.ConnectionOutcomeWord(service, outcome)
}

func runDisconnect(args []string) error {
	flags := commandFlags("disconnect")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return wrongCall("codeaf disconnect needs one provider name")
	}
	profileDir := config.ProfileDir()
	name := strings.ToLower(strings.TrimSpace(flags.Arg(0)))
	if name == modelsource.DefaultID {
		if strings.TrimSpace(config.PersistedAPIKey(profileDir)) == "" {
			return disconnectedFailure(name)
		}
		if err := config.WriteAPIKey(profileDir, ""); err != nil {
			return err
		}
		fmt.Fprintln(usageOut, modelsource.DefaultID+" disconnected")
		return nil
	}
	_, row, found := connectionSource(profileDir, name)
	if !found || strings.TrimSpace(row.ID) == "" {
		return disconnectedFailure(name)
	}
	if err := config.DisconnectService(profileDir, row.ID); err != nil {
		return err
	}
	fmt.Fprintln(usageOut, strings.ToLower(strings.TrimSpace(row.Written))+" disconnected")
	return nil
}

func disconnectedFailure(service string) error {
	fmt.Fprintln(usageOut, service+" is not connected")
	return exitStatus(1)
}
