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
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/desktopbridge"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// This local surface door reuses the same persistent engine and wire as the
// terminal. Its credentials are process-local, never a provider key.
func runDesktopBridge(args []string) error {
	flags := commandFlags("desktop-bridge")
	address := flags.String("listen", "127.0.0.1:1423", "loopback address for the desktop transport")
	workspace := flags.String("workspace", "", "working directory shared with the terminal")
	placesFile := flags.String("places", "", "place graph file (default: the desktop folder of the codeaf state root)")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: codeaf desktop-bridge [--listen 127.0.0.1:1423] [--workspace path] [--places path]")
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("desktop transport must listen on a loopback address")
	}
	if strings.TrimSpace(*workspace) == "" {
		*workspace, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	resolved, err := engineWorkspace(*workspace)
	if err != nil {
		return err
	}
	token := env.Get("CODEAF_DESKTOP_TOKEN")
	if token == "" {
		token, err = desktopbridge.Token()
		if err != nil {
			return err
		}
	}
	if len(token) < 32 {
		return errors.New("desktop transport token must contain at least 32 characters")
	}
	profileDir := config.ProfileDir()
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	bridge := desktopbridge.New(token, func(file string) (desktopbridge.Connection, error) {
		command := exec.Command(binary, "engine", "--workspace", resolved)
		command.Stderr = os.Stderr
		input, err := command.StdinPipe()
		if err != nil {
			return desktopbridge.Connection{}, err
		}
		output, err := command.StdoutPipe()
		if err != nil {
			input.Close()
			return desktopbridge.Connection{}, err
		}
		if err := command.Start(); err != nil {
			input.Close()
			output.Close()
			return desktopbridge.Connection{}, err
		}
		pipe := &desktopPipe{Reader: output, Writer: input, command: command}
		client, err := remote.Dial(pipe, "local desktop", remote.Hello{Version: remote.Version, Workspace: resolved, Session: file, New: file == "", Model: conversationModel(profileDir), Surface: "desktop", Launch: &remote.LaunchShape{OneModel: true, Interactive: true}})
		if err != nil {
			pipe.Close()
			return desktopbridge.Connection{}, err
		}
		return desktopbridge.Connection{Agent: client.Agent(), Welcome: client.Welcome(), Follow: client.Follow(), Take: client.Take, FetchFile: client.FetchFile, StatPaths: client.StatPaths, ReadText: client.ReadText, FindFiles: client.FindFiles, DiffChanges: client.DiffChanges, DiffFile: client.DiffFile, DiffStart: client.DiffStart, Local: true, Close: func() { _ = client.Close() }}, nil
	})
	defer bridge.Close()
	bridge.UseModels(&desktopbridge.Models{ProfileDir: profileDir, Catalog: desktopCatalog(profileDir)})
	// The place graph lives at a path chosen here and handed in; nothing in the
	// bridge picks a default of its own.
	if strings.TrimSpace(*placesFile) == "" {
		*placesFile = home.Join("desktop", "places.json")
	}
	placeStore, err := placegraph.Open(placegraph.Options{Path: *placesFile})
	if err != nil {
		return fmt.Errorf("places: %w", err)
	}
	if recovery := placeStore.LastRecovery(); recovery != nil {
		fmt.Fprintf(os.Stderr, "places: %s file kept at %s (%s)\n", recovery.Kind, recovery.MovedTo, recovery.Reason)
	}
	bridge.UsePlaces(desktopbridge.NewPlaces(placeStore))
	bridge.UseHistory(&desktopbridge.History{Root: session.PlacesRoot()})
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"url": "http://" + listener.Addr().String(), "token": token, "model": desktopbridge.Model}); err != nil {
		return err
	}
	server := &http.Server{Handler: bridge, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	guard.Go("desktop-bridge-shutdown", func() {
		<-ctx.Done()
		deadline, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(deadline)
	})
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("desktop transport: %w", err)
	}
	return nil
}

type desktopPipe struct {
	io.Reader
	io.Writer
	command *exec.Cmd
	once    sync.Once
}

func (p *desktopPipe) Close() error {
	p.once.Do(func() {
		_ = p.Writer.(io.Closer).Close()
		_ = p.Reader.(io.Closer).Close()
		guard.Go("desktop-bridge-reap", func() { _ = p.command.Wait() })
	})
	return nil
}

// conversationModel is the model a new desktop conversation opens on: the
// person's choice for the Conversation role, or the default.
func conversationModel(profileDir string) string {
	model, _, _ := config.DesktopRoleChoice(profileDir, config.DesktopRoleConversation)
	return model
}

// desktopCatalog lists the provider's models through the engine's own catalog
// code, so the settings page offers exactly the models the engine can resolve.
func desktopCatalog(profileDir string) func(context.Context) ([]desktopbridge.CatalogModel, error) {
	return func(ctx context.Context) ([]desktopbridge.CatalogModel, error) {
		settings, err := config.LoadKeyless()
		if err != nil {
			return nil, err
		}
		listed, err := catalog.Refresh(ctx, catalog.Options{
			BaseURL: settings.BaseURL, APIKey: settings.APIKey, Dir: profileDir,
			HTTPClient: config.CatalogHTTPClient(settings.Sources.Default()),
		})
		if listed == nil {
			return nil, err
		}
		defer listed.Close()
		models := listed.ModelsNow()
		out := make([]desktopbridge.CatalogModel, 0, len(models))
		for _, model := range models {
			out = append(out, desktopbridge.CatalogModel{ID: model.ID, Name: model.Name, ContextLength: model.ContextLength, Efforts: model.Reasoning.Efforts})
		}
		return out, err
	}
}
