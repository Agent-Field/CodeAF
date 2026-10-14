package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/apps"
	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/processgroup"
)

type appServeOptions struct {
	home      string
	listen    string
	parentPID int
}

func runApp(args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			fmt.Fprintln(usageOut, "codeaf app serve [--home dir] [--listen 127.0.0.1:0] [--parent-pid pid]")
			return exitHelped
		}
		return errors.New("usage: codeaf app serve [--home dir] [--listen 127.0.0.1:0] [--parent-pid pid]")
	}
	if done, err := serveWorkspace(args[1:]); done || err != nil {
		return err
	}
	flags := commandFlags("app serve")
	options := appServeOptions{}
	flags.StringVar(&options.home, "home", "", "shared codeaf home (default: CODEAF_HOME or ~/.codeaf)")
	flags.StringVar(&options.listen, "listen", "127.0.0.1:0", "numeric loopback address; port 0 chooses a free port")
	flags.IntVar(&options.parentPID, "parent-pid", 0, "stop when this parent process exits (0 disables the check)")
	if err := parseCommandFlags(flags, args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || options.parentPID < 0 {
		return errors.New("app serve accepts only flags and a nonnegative parent pid")
	}
	root, err := appEngineHome(options.home)
	if err != nil {
		return err
	}
	options.home = root
	if err := os.Setenv(home.EnvVar, root); err != nil {
		return err
	}
	if err := os.Setenv(config.AppModeEnv, "1"); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveApp(ctx, options, os.Stdin, os.Stdout, os.Stderr)
}

// The app and terminal resolve the same state root so agents, history and
// settings remain visible through either door. An explicit home still wins.
func appEngineHome(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return filepath.Abs(explicit)
	}
	return filepath.Abs(home.Dir())
}

func serveApp(parent context.Context, options appServeOptions, input io.ReadCloser, output, errorsOut io.Writer) error {
	credentials := config.NewAppCredentials()
	restoreCredentials := config.UseAppCredentials(credentials)
	defer restoreCredentials()
	parentAlive := appParentCheck(options.parentPID, os.Getppid, processgroup.ProcessAlive)
	if options.parentPID != 0 && !parentAlive(options.parentPID) {
		return errors.New("app parent process is no longer running")
	}
	listener, err := appapi.Listen(options.listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	token, err := appapi.Token()
	if err != nil {
		return fmt.Errorf("create app token: %w", err)
	}
	epoch, err := appapi.Token()
	if err != nil {
		return fmt.Errorf("create app epoch: %w", err)
	}
	logsDir := filepath.Join(options.home, "logs")
	if err := os.MkdirAll(logsDir, 0700); err != nil {
		return fmt.Errorf("create app log directory: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(logsDir, "app-serve.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("open app log: %w", err)
	}
	defer logFile.Close()
	logOutput := io.MultiWriter(errorsOut, logFile)
	previousLog := log.Writer()
	log.SetOutput(logOutput)
	defer log.SetOutput(previousLog)
	logger := log.New(logOutput, "app: ", log.LstdFlags)

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	// stdin belongs to this launch. Closing it on every exit releases the
	// reader even when HTTP shutdown or parent death ended the launch first.
	defer input.Close()
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	agents := appapi.NewAgentRegistry(options.home, appAgentOpener(binary, logOutput))
	defer agents.Close()
	api := appapi.New(token, appapi.Options{Version: buildinfo.String(), Epoch: epoch, Shutdown: cancel, Credentials: credentials, ProfileDir: config.ProfilePath(config.ProfileDir(), ""), Context: ctx, Agents: agents, StandingTick: appStandingTick})
	defer api.Close()
	apps.Mount(ctx, api, apps.Wire(ctx, options.home))
	factories, err := newAppFactoryHost(ctx, options.home, "http://"+listener.Addr().String(), token, credentials)
	if err != nil {
		return err
	}
	defer factories.close()
	if err := api.BindFactories(&appapi.FactoryRegistry{Home: options.home, URL: factories.address, Token: token, Credentials: credentials, Solo: factories.mountSolo, Power: factories.power, SetupCheckouts: factories.floor.Checkouts, SetupCheckout: func(ctx context.Context, repo, path string, clone bool) (string, error) {
		return serve.SetupCheckout(ctx, factories.floor, factories.profile, repo, path, clone)
	}}); err != nil {
		return err
	}
	server := &http.Server{
		Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout: 60 * time.Second, ErrorLog: logger,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	shutdownDone := make(chan struct{})
	guard.Go("app/shutdown", func() {
		defer close(shutdownDone)
		<-ctx.Done()
		deadline, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if err := server.Shutdown(deadline); err != nil {
			_ = server.Close()
		}
	})
	guard.Go("app/stdin", func() {
		// Parse only the first line. Buffered bytes after it still belong to
		// the lifetime pipe; no handoff bytes reach logs or the hello.
		reader := bufio.NewReader(input)
		if err := config.ReadAppHandoff(reader, credentials); err != nil {
			if ctx.Err() == nil {
				logger.Print("key handoff ended or was invalid")
			}
			cancel()
			return
		}
		_, _ = io.Copy(io.Discard, reader)
		cancel()
	})
	if options.parentPID != 0 {
		guard.Go("app/parent", func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			watchAppParent(ctx, options.parentPID, parentAlive, ticker.C, cancel)
		})
	}
	hello := appapi.Hello{
		URL: "http://" + listener.Addr().String(), Token: token, PID: os.Getpid(),
		CodeafVersion: buildinfo.String(), Contract: appapi.Contract, Home: options.home, Epoch: epoch,
	}
	if err := json.NewEncoder(output).Encode(hello); err != nil {
		cancel()
		<-shutdownDone
		return fmt.Errorf("write app hello: %w", err)
	}
	logger.Print("listening on ", listener.Addr())
	err = server.Serve(listener)
	cancel()
	<-shutdownDone
	logger.Print("stopped")
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("app transport: %w", err)
	}
	return nil
}

// A dead launch parent can remain a zombie until its launcher reaps it.
// Reparenting tells the child it died even while a signal probe still finds
// that pid. An explicitly named process outside this parent chain uses the
// signal probe alone.
func appParentCheck(pid int, parent func() int, exists func(int) bool) func(int) bool {
	launchParent := parent() == pid
	return func(pid int) bool {
		return (!launchParent || parent() == pid) && exists(pid)
	}
}

// watchAppParent takes its clock and liveness check as inputs so the death
// rule is tested without real sleeps or a PID that another process could own.
func watchAppParent(ctx context.Context, pid int, alive func(int) bool, ticks <-chan time.Time, stop func()) {
	for {
		if !alive(pid) {
			stop()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}
