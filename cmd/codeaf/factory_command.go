package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
)

const factoryDefaultListen = "127.0.0.1:7420"

// factoryDiscovery lets another local client find the bound port, including
// when the host requested port zero. The token file is private like the config.
type factoryDiscovery struct {
	URL   string `json:"url"`
	PID   int    `json:"pid"`
	Token string `json:"token,omitempty"`
	App   bool   `json:"app,omitempty"`
}

func runFactory(args []string) error {
	verb := "serve"
	if len(args) > 0 {
		switch args[0] {
		case "serve", "steps", "convert", "token", "revoke", "revoke-sessions", "name":
			verb, args = args[0], args[1:]
		}
	}
	if verb == "steps" {
		flags := commandFlags("factory steps")
		if err := parseCommandFlags(flags, args); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return wrongCall("codeaf factory steps takes no arguments")
		}
		return printFactorySteps(os.Stdout)
	}
	if verb == "convert" {
		return runFactoryConvert(args, os.Stdout, os.Stderr)
	}
	if verb == "token" {
		return runFactoryToken(args)
	}
	if verb == "name" {
		return runFactoryName(args)
	}
	if verb == "revoke" {
		return runFactoryRevoke(args)
	}
	if verb == "revoke-sessions" {
		flags := commandFlags("factory revoke-sessions")
		if err := parseCommandFlags(flags, args); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return wrongCall("codeaf factory revoke-sessions takes no arguments")
		}
		return revokeFactorySessions(os.Stdout)
	}
	flags := commandFlags("factory")
	listen := flags.String("listen", "", "address to listen on")
	path := flags.String("config", home.Join("factory", "config.yaml"), "host configuration")
	model := flags.String("model", "", "model slug for the factory")
	newOperator := flags.Bool("new-operator-link", false, "print a fresh one-time operator recovery link")
	oneModel := flags.Bool("one-model", false, "every factory text call runs on the named model; the tier rows stand down")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return wrongCall("use codeaf factory serve, steps, convert, token, revoke, revoke-sessions or name")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	modelName := strings.TrimSpace(*model)
	if *oneModel && modelName == "" {
		settings, err := config.LoadKeyless()
		if err != nil {
			return err
		}
		modelName = v3TalkModel("", settings)
	}
	return serveFactory(ctx, *path, *listen, os.Stdout, modelName, *oneModel, *newOperator)
}

func printFactorySteps(out io.Writer) error {
	_, err := io.WriteString(out, pipeline.FormatSteps())
	return err
}

// serveFactory owns the HTTP service and triage: it runs no job. Its runner doors
// are the lease's, so a run pressed anywhere queues the job for whichever
// attached worker pulls it, and every other verb reaches that worker through
// its lease (factory_run.go).
func serveFactory(ctx context.Context, configPath, listen string, out io.Writer, model string, oneModel bool, recovery ...bool) error {
	st, err := store.Open(v3FactoryRoot())
	if err != nil {
		return err
	}
	release, err := factoryFileLock(filepath.Join(st.Root(), "serve.lock"), true)
	defer release()
	if err != nil {
		return err
	}
	cfg, err := factoryHostConfigAt(configPath, false)
	if err != nil {
		return err
	}
	if cfg.Repos != nil {
		if err := st.SetRepos(cfg.Repos); err != nil {
			return err
		}
	}
	if listen == "" {
		listen = cfg.Listen
	}
	secure, err := resolveFactoryTLS(cfg, listen, filepath.Dir(configPath))
	if err != nil {
		return fmt.Errorf("factory config %s: %w", configPath, err)
	}
	cfg.secure = &secure
	cfg.operatorLinks = true
	cfg.operatorRecovery = len(recovery) > 0 && recovery[0]
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("factory cannot listen on %s: %w", listen, err)
	}
	defer listener.Close()
	// The server reads GitHub through its own grant while each caller's
	// signed-in identity supplies Activity and job ownership.
	seam := factoryServerSeam(st, factoryTokensPath(configPath))
	service, err := startFactoryService(ctx, st, seam, listener, cfg.AccessToken, factoryTokensPath(configPath), cfg)
	if err != nil {
		return err
	}
	defer service.close()
	operatorLink := service.operatorLink
	// THE SERVER HAS NO CONVERSATION, so its launch supplies the model pin
	// on the same parent the desk's worker uses. Triage reads it for every request.
	setFactoryModelPin(model, oneModel)
	startFactoryTriage(ctx, st)
	startFactoryServerPoll(ctx, st, factoryTokensPath(configPath))
	server, done := service.server, service.done
	if _, err := fmt.Fprintln(out, service.public); err != nil {
		return err
	}
	if operatorLink.URL != "" {
		if _, err := fmt.Fprintln(out, "operator access link: "+operatorLink.URL); err != nil {
			return err
		}
	}
	for _, line := range service.notes {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return err
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// factoryService is one running factory HTTP service: the handler over a
// seam, its server on a listener, and the serve.json receipt naming it.
type factoryService struct {
	// url is the address this machine's own clients dial, which serve.json
	// names; public is the one a person is given. They differ when TLS is on.
	url          string
	public       string
	notes        []string
	operatorLink api.AccessLink
	server       *http.Server
	door         *http.Server
	handler      *serve.Handler
	done         chan error
	discovery    string
}

// startFactoryService serves seam on listener and writes serve.json, the
// receipt other codeaf processes on this machine discover it by. It is the one
// start both `codeaf factory` and a window serving in-process make, so the two
// can never answer differently. ctx bounds the long polls and change streams.
func startFactoryService(ctx context.Context, st *store.Store, seam factory.Seam, listener net.Listener, token, tokensPath string, hostConfigs ...factoryHostConfig) (*factoryService, error) {
	address := "http://" + listener.Addr().String()
	history, err := record.Open(st, record.Options{})
	if err != nil {
		return nil, err
	}
	// The service's watch, worker reports and record share the jobs' root, so
	// clients observe the same files the runner writes.
	var host factoryHostConfig
	if len(hostConfigs) > 0 {
		host = hostConfigs[0]
	}
	// A window and the explicit server both poll, while a service opened
	// only for local discovery does not promise a background read.
	servedBy := "factory"
	var poll time.Duration
	if token == "" {
		servedBy = "tui"
		poll = factoryPollEvery
	}
	if host.operatorLinks {
		poll = factoryPollEvery
	}
	mode := host.Mode
	if mode == "" {
		mode = api.FactoryTeam
		if servedBy == "tui" {
			mode = api.FactorySolo
		}
	}
	secure := factoryTLS{mode: factoryTLSOff}
	if host.secure != nil {
		secure = *host.secure
	}
	public := secure.publicURL(listener.Addr().String())
	jobWrites := factorygithub.JobWrites{Token: func(ctx context.Context) string { t, _ := factorygithub.TokenAt(ctx, home.Dir()); return t }}
	handlerConfig := serve.Config{
		GitHubToken: jobWrites.Token,

		ProfileDir:     home.Dir(),
		ChangeAssignee: jobWrites.Assign,
		// A read, not a write: a person's start checks the one item is still
		// open on GitHub, so the team server keeps it too.
		Recheck: factorygithub.Rechecker(jobWrites.Token),
		// A drop deletes the job's local branch and folder on this machine,
		// never anything on GitHub, so the team server does it too.
		Folders: newFactoryJobFolders(st), Mode: mode, OperatorLogin: host.OperatorLogin, TrustedProxies: secure.proxies, Name: host.Name, GitHubClientID: host.GitHubClientID, Team: host.Team, Address: public, AccessToken: token, TokensPath: tokensPath, Store: st, Root: st.Root(), Record: history,
		ServedBy: servedBy, PollingEvery: poll,
		PersonProfile: factoryPeople().Profile, PersonKnown: factoryPeople().Known, CodeOwners: factoryPeople().Owners,
		Pipeline: func(ctx context.Context, repo, dir string) (pipeline.File, error) {
			return factoryRules().Load(ctx, repo, dir, home.Join("factory.yaml"))
		},
	}
	if servedBy == "factory" && mode == api.FactoryTeam {
		// THE TEAM SERVER NEVER WRITES TO GITHUB, even with a host override.
		handlerConfig.ChangeAssignee = nil
	}
	handler := serve.New(seam, handlerConfig)
	var operatorLink api.AccessLink
	if host.operatorLinks {
		operatorLink, err = handler.NewOperatorLink(ctx, host.operatorRecovery)
		if err != nil {
			handler.Close()
			return nil, err
		}
	}
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 10 * time.Second,
		// Long polls and change streams must end with the service, otherwise
		// graceful shutdown waits on clients that are waiting on this process.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	// Every listener reports its end here; only the first is read, so the
	// channel holds one per listener and none of them blocks on the send.
	s := &factoryService{url: address, public: public, operatorLink: operatorLink, server: server, handler: handler, done: make(chan error, 2),
		discovery: filepath.Join(st.Root(), "serve.json")}
	served := listener
	if secure.on() {
		local, notes, err := s.serveTLS(ctx, secure, listener)
		if err != nil {
			return nil, err
		}
		served, s.notes = local, notes
	} else if !factoryLoopbackListen(listener.Addr().String()) && host.secure != nil {
		s.notes = append(s.notes, "warning: serving plain HTTP on "+listener.Addr().String()+
			"; access tokens and sign-in sessions cross the network unencrypted. Serve HTTPS with tls: auto and a domain, "+
			"or put a reverse proxy that serves HTTPS in front and list it in trusted_proxies (the manual: hosting a team factory over HTTPS)")
	}
	guard.Go("factory HTTP service", func() { s.done <- server.Serve(served) })
	body, err := json.Marshal(factoryDiscovery{URL: s.url, PID: os.Getpid(), Token: token})
	if err == nil {
		err = writeFactoryPrivate(s.discovery, append(body, '\n'))
	}
	if err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

// serveTLS serves HTTPS on the public listener, and answers the plain-HTTP
// local listener it opens in its place for serve.json.
//
// THIS MACHINE'S OWN CLIENTS KEEP PLAIN HTTP ON LOOPBACK. The certificate names
// the domain, never 127.0.0.1, so a window or `revoke-sessions` dialling the
// loopback address over HTTPS would be refused; a loopback socket never
// crosses a network, so it has nothing for TLS to protect.
func (s *factoryService) serveTLS(ctx context.Context, secure factoryTLS, listener net.Listener) (net.Listener, []string, error) {
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	config, door, err := secure.serverTLS(port)
	if err != nil {
		return nil, nil, err
	}
	local, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	s.url = "http://" + local.Addr().String()
	s.server.TLSConfig = config
	guard.Go("factory HTTPS service", func() { s.done <- s.server.ServeTLS(listener, "", "") })
	var notes []string
	if secure.mode == factoryTLSAuto {
		notes = append(notes, "the certificate for "+secure.domain+" is requested on the first HTTPS connection, renewed on its own, and kept in "+secure.cacheDir)
	}
	if secure.redirect != "" {
		plain, err := net.Listen("tcp", secure.redirect)
		switch {
		case err == nil:
			s.door = &http.Server{Handler: door, ReadHeaderTimeout: 10 * time.Second,
				BaseContext: func(net.Listener) context.Context { return ctx }}
			guard.Go("factory HTTP redirect", func() { _ = s.door.Serve(plain) })
		case secure.mode == factoryTLSAuto && port != "443":
			notes = append(notes, "warning: cannot listen on "+secure.redirect+" ("+err.Error()+
				"), and certificates need port 80 or HTTPS on port 443; no certificate can be issued until one of them is reachable")
		case secure.mode == factoryTLSAuto:
			notes = append(notes, "port 80 is not available, so certificates are proven on port 443 alone and plain HTTP is not redirected")
		}
	}
	return local, notes, nil
}

// close stops serving and takes the receipt down, so nobody discovers a
// service that is gone.
func (s *factoryService) close() {
	_ = os.Remove(s.discovery)
	_ = s.server.Close()
	if s.door != nil {
		_ = s.door.Close()
	}
	s.handler.Close()
}

// factoryFileLock coordinates service discovery and token writes across
// processes. A second service must not replace the first service's receipt.
func factoryFileLock(path string, nonBlocking bool) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return func() {}, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, err
	}
	if err := filelock.Lock(f, true, nonBlocking); err != nil {
		f.Close()
		if filelock.IsBusy(err) {
			return func() {}, errors.New("a factory service is already running for this codeaf home")
		}
		return func() {}, err
	}
	return func() { _ = filelock.Unlock(f); _ = f.Close() }, nil
}
