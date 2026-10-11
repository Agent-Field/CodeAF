package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/filelock"
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
		case "serve", "steps", "token":
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
	if verb == "token" {
		return runFactoryToken(args)
	}
	flags := commandFlags("factory")
	listen := flags.String("listen", "", "address to listen on")
	path := flags.String("config", home.Join("factory", "config.yaml"), "host configuration")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return wrongCall("use codeaf factory [serve], codeaf factory steps or codeaf factory token")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveFactory(ctx, *path, *listen, os.Stdout)
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

// serveFactory owns only the HTTP service. Worker execution belongs to a
// separate command, so this seam deliberately has no runner or mailbox.
func serveFactory(ctx context.Context, configPath, listen string, out io.Writer) error {
	st, err := store.Open(v3FactoryRoot())
	if err != nil {
		return err
	}
	release, err := factoryFileLock(filepath.Join(st.Root(), "serve.lock"), true)
	if err != nil {
		return err
	}
	defer release()
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
	address := "http://" + listener.Addr().String()
	seam := factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(st.CheckoutDir))
	server := &http.Server{Handler: serve.New(seam, serve.Config{Address: address, AccessToken: cfg.AccessToken}), ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer server.Close()
	discovery := filepath.Join(st.Root(), "serve.json")
	body, err := json.Marshal(factoryDiscovery{URL: address, PID: os.Getpid(), Token: cfg.AccessToken})
	if err != nil {
		return err
	}
	if err := writeFactoryPrivate(discovery, append(body, '\n')); err != nil {
		return err
	}
	defer os.Remove(discovery)
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

// factoryFileLock coordinates service discovery and token writes across
// processes. A second service must not replace the first service's receipt.
func factoryFileLock(path string, nonBlocking bool) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(f, true, nonBlocking); err != nil {
		f.Close()
		if filelock.IsBusy(err) {
			return nil, errors.New("a factory service is already running for this codeaf home")
		}
		return nil, err
	}
	return func() { _ = filelock.Unlock(f); _ = f.Close() }, nil
}
