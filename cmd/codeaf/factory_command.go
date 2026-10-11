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
	Token string `json:"token"`
}

func runFactory(args []string) error {
	verb := "serve"
	if len(args) > 0 {
		switch args[0] {
		case "serve", "steps", "convert", "token", "revoke-sessions":
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
	oneModel := flags.Bool("one-model", false, "every factory text call runs on the named model; the tier rows stand down")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return wrongCall("use codeaf factory [serve], steps, convert, token or revoke-sessions")
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
	return serveFactory(ctx, *path, *listen, os.Stdout, modelName, *oneModel)
}

func printFactorySteps(out io.Writer) error {
	r := factory.DefaultRecipe()
	for _, kind := range []factory.Kind{factory.KindIssue, factory.KindPR, factory.KindCI} {
		if _, err := fmt.Fprintf(out, "## %s pipeline\n", kind); err != nil {
			return err
		}
		for _, step := range r.For(kind) {
			prompt := step.Ask
			if step.Kind == factory.StageGate {
				prompt = "a person approves before the job continues"
			}
			if _, err := fmt.Fprintf(out, "%s: %s\n", step.Name, prompt); err != nil {
				return err
			}
		}
	}
	return nil
}

// serveFactory owns the HTTP service and triage: it runs no job. Its runner doors
// are the lease's, so a run pressed anywhere queues the job for whichever
// attached worker pulls it, and every other verb reaches that worker through
// its lease (factory_run.go).
func serveFactory(ctx context.Context, configPath, listen string, out io.Writer, model string, oneModel bool) error {
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
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("factory cannot listen on %s: %w", listen, err)
	}
	defer listener.Close()
	// The service records every approval under a forge handle, so it must know
	// who the token belongs to: without the connection doors an approval from
	// the browser is refused ("approval requires a forge handle").
	seam := factorygithub.Connect(factory.LocalSeam(st, time.Now(), factoryRunnerDoors(st), factoryPipelineOption(), factory.WithRepoDirs(st.CheckoutDir)), factoryProfile(""))
	service, err := startFactoryService(ctx, st, seam, listener, cfg.AccessToken)
	if err != nil {
		return err
	}
	defer service.close()
	// THE SERVER HAS NO CONVERSATION, so its launch supplies the model pin
	// on the same parent the desk's worker uses. Triage reads it for every request.
	setFactoryModelPin(model, oneModel)
	startFactoryTriage(ctx, st)
	address, server, done := service.url, service.server, service.done
	if _, err := fmt.Fprintln(out, address); err != nil {
		return err
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
	url       string
	server    *http.Server
	handler   *serve.Handler
	done      chan error
	discovery string
}

// startFactoryService serves seam on listener and writes serve.json, the
// receipt other codeaf processes on this machine discover it by. It is the one
// start both `codeaf factory` and a window serving in-process make, so the two
// can never answer differently. ctx bounds the long polls and change streams.
func startFactoryService(ctx context.Context, st *store.Store, seam factory.Seam, listener net.Listener, token string) (*factoryService, error) {
	address := "http://" + listener.Addr().String()
	history, err := record.Open(st, record.Options{})
	if err != nil {
		return nil, err
	}
	// The service's watch, worker reports and record share the jobs' root, so
	// clients observe the same files the runner writes.
	handler := serve.New(seam, serve.Config{Address: address, AccessToken: token, Store: st, Root: st.Root(), Record: history})
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 10 * time.Second,
		// Long polls and change streams must end with the service, otherwise
		// graceful shutdown waits on clients that are waiting on this process.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	s := &factoryService{url: address, server: server, handler: handler, done: make(chan error, 1),
		discovery: filepath.Join(st.Root(), "serve.json")}
	guard.Go("factory HTTP service", func() { s.done <- server.Serve(listener) })
	body, err := json.Marshal(factoryDiscovery{URL: address, PID: os.Getpid(), Token: token})
	if err == nil {
		err = writeFactoryPrivate(s.discovery, append(body, '\n'))
	}
	if err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

// close stops serving and takes the receipt down, so nobody discovers a
// service that is gone.
func (s *factoryService) close() {
	_ = os.Remove(s.discovery)
	_ = s.server.Close()
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
