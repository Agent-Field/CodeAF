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
	"path/filepath"
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
	"github.com/Agent-Field/codeaf/internal/workspacestore"
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
	// The place graph lives at a path chosen here and handed in; nothing in the
	// bridge picks a default of its own. It is made absolute ONCE, and that one
	// string is both the file the bridge's store opens and the file every
	// engine this bridge reaches reads — carried in the hello, because the
	// engine is often a session host that was running before this bridge was,
	// and a variable set on the child below would never reach it.
	graphPath, err := desktopPlacesPath(*placesFile)
	if err != nil {
		return fmt.Errorf("places: %w", err)
	}
	placeDoor, err := session.PlaceGraphDoorFor(graphPath)
	if err != nil {
		return fmt.Errorf("places: %w", err)
	}
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
		client, err := remote.Dial(pipe, "local desktop", desktopHello(resolved, file, conversationModel(profileDir), placeDoor.Path))
		if err != nil {
			pipe.Close()
			return desktopbridge.Connection{}, err
		}
		return desktopbridge.Connection{Agent: client.Agent(), Welcome: client.Welcome(), Follow: client.Follow(), Take: client.Take, FetchFile: client.FetchFile, StatPaths: client.StatPaths, ReadText: client.ReadText, FindFiles: client.FindFiles, DiffChanges: client.DiffChanges, DiffFile: client.DiffFile, DiffStart: client.DiffStart, Local: true, Close: func() { _ = client.Close() }}, nil
	})
	defer bridge.Close()
	bridge.UseModels(&desktopbridge.Models{ProfileDir: profileDir, Catalog: desktopCatalog(profileDir)})
	placeStore, err := placegraph.Open(placegraph.Options{Path: placeDoor.Path})
	if err != nil {
		return fmt.Errorf("places: %w", err)
	}
	if recovery := placeStore.LastRecovery(); recovery != nil {
		fmt.Fprintf(os.Stderr, "places: %s file kept at %s (%s)\n", recovery.Kind, recovery.MovedTo, recovery.Reason)
	}
	places := desktopbridge.NewPlaces(placeStore)
	if err := places.UseDoor(placeDoor); err != nil {
		return fmt.Errorf("places: %w", err)
	}
	bridge.UsePlaces(places)
	bridge.UseHistory(&desktopbridge.History{Root: session.PlacesRoot()})
	// Each window place's tab set lives beside the place graph, in a folder the
	// bridge is handed rather than one it picks: so a --places file in a test or
	// a throwaway home carries its tab sets with it.
	workspaces, err := workspacestore.Open(workspacestore.Options{Dir: desktopWorkspacesDir(placeDoor.Path)})
	if err != nil {
		return fmt.Errorf("tab sets: %w", err)
	}
	bridge.UseWorkspaces(workspaces)
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

// desktopPlacesPath is the place graph file this bridge opens and every engine
// it reaches reads: the --places flag, or the desktop folder of the state root,
// made absolute once so both processes name one file.
func desktopPlacesPath(flag string) (string, error) {
	path := strings.TrimSpace(flag)
	if path == "" {
		path = home.Join("desktop", "places.json")
	}
	return filepath.Abs(path)
}

// desktopWorkspacesDir is where the tab sets of each window place are kept:
// a "workspaces" folder next to the place graph (PLACES-ARCHITECTURE §3.4).
func desktopWorkspacesDir(graphPath string) string {
	return filepath.Join(filepath.Dir(graphPath), "workspaces")
}

// desktopHello is the hello a desktop conversation is opened with. The place
// graph rides its launch shape (remote.LaunchShape.PlaceGraph) so a session
// host that was already running reads the same file this bridge does.
func desktopHello(workspace, file, model, graph string) remote.Hello {
	return remote.Hello{
		Version: remote.Version, Workspace: workspace, Session: file, New: file == "", Model: model,
		Surface: "desktop",
		Launch:  &remote.LaunchShape{OneModel: true, Interactive: true, PlaceGraph: graph},
	}
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
